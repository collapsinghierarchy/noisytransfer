package hub

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	roomTTL          = 10 * time.Minute // keep state when both sides are offline
	gcInterval       = 1 * time.Minute
	maxMailboxQueued = 10000
)

type connWrap struct {
	ws  *websocket.Conn
	wmu sync.Mutex // serialize *all* writes (WriteMessage/WriteJSON/WriteControl)
}

type deliverEnvelope struct {
	Type    string          `json:"type"` // "deliver"
	Seq     uint64          `json:"seq"`
	From    string          `json:"from"`
	Payload json.RawMessage `json:"payload"`
}

type queued struct {
	seq     uint64
	from    string
	payload json.RawMessage
}

type mailbox struct {
	nextSeq       uint64
	deliveredUpTo uint64
	queue         []queued // kept sorted by seq ascending
}

type room struct {
	conns        map[string]*connWrap // side -> conn
	sids         map[string]string    // side -> sessionID (optional)
	mboxes       map[string]*mailbox  // side -> mailbox
	lastActivity time.Time
}

type byConnKey struct {
	appID string
	side  string
}

type Hub struct {
	mu     sync.Mutex
	rooms  map[string]*room
	byConn map[*websocket.Conn]byConnKey
}

func NewHub() *Hub {
	h := &Hub{
		rooms:  make(map[string]*room),
		byConn: make(map[*websocket.Conn]byConnKey),
	}
	go h.gcLoop()
	return h
}

func (h *Hub) gcLoop() {
	ticker := time.NewTicker(gcInterval)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		now := time.Now()
		for appID, r := range h.rooms {
			// delete only if no connections AND TTL expired
			if len(r.conns) == 0 && now.Sub(r.lastActivity) > roomTTL {
				delete(h.rooms, appID)
			}
		}
		h.mu.Unlock()
	}
}

func (h *Hub) touch(r *room) {
	r.lastActivity = time.Now()
}

// Register connection for (appID, side). Enforces one active conn per side.
func (h *Hub) Register(appID, side, sid string, conn *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if side != "A" && side != "B" {
		return errors.New("invalid side (want A or B)")
	}

	r := h.rooms[appID]
	if r == nil {
		r = &room{
			conns:        make(map[string]*connWrap, 2),
			sids:         make(map[string]string, 2),
			mboxes:       map[string]*mailbox{"A": {}, "B": {}},
			lastActivity: time.Now(),
		}
		h.rooms[appID] = r
	}

	// Evict existing side (if any)
	if old := r.conns[side]; old != nil {
		// send close frame under write lock, then close
		old.wmu.Lock()
		_ = old.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "replaced"))
		old.wmu.Unlock()
		_ = old.ws.Close()
		delete(h.byConn, old.ws)
	}
	wrap := &connWrap{ws: conn}
	r.conns[side] = wrap
	r.sids[side] = sid
	h.byConn[conn] = byConnKey{appID: appID, side: side}

	h.touch(r) // mark activity
	// Opportunistically push pending (uses current deliveredUpTo)
	h.pushAllLocked(appID, side)

	return nil
}

func (h *Hub) Unregister(appID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key, ok := h.byConn[conn]
	if !ok {
		return
	}
	delete(h.byConn, conn)

	if r := h.rooms[key.appID]; r != nil {
		if w := r.conns[key.side]; w != nil && w.ws == conn {
			delete(r.conns, key.side)
		}
	}
}

func (h *Hub) RoomSize(appID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[appID]; r != nil {
		return len(r.conns)
	}
	return 0
}

// Broadcast signaling to "the other" side(s).
func (h *Hub) Broadcast(appID string, sender *websocket.Conn, msg []byte) {
	h.mu.Lock()
	key := h.byConn[sender]
	r := h.rooms[appID]
	var targets []*connWrap
	if r != nil {
		for side, c := range r.conns {
			if c != nil && c.ws != sender && (key.side == "" || side != key.side) {
				targets = append(targets, c)
			}
		}
	}
	h.mu.Unlock()

	// write outside hub lock, under each conn's write mutex
	for _, c := range targets {
		c.wmu.Lock()
		_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.ws.WriteMessage(websocket.TextMessage, msg)
		c.wmu.Unlock()
		if err != nil {
			_ = c.ws.Close()
			h.Unregister(appID, c.ws)
		}
	}
}

func (h *Hub) BroadcastEvent(appID string, evt any) {
	data, _ := json.Marshal(evt)
	h.mu.Lock()
	r := h.rooms[appID]
	var conns []*connWrap
	if r != nil {
		for _, c := range r.conns {
			if c != nil {
				conns = append(conns, c)
			}
		}
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.wmu.Lock()
		_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.ws.WriteMessage(websocket.TextMessage, data)
		c.wmu.Unlock()
		if err != nil {
			_ = c.ws.Close()
			h.Unregister(appID, c.ws)
		}
	}
}

// Hello updates delivered watermark and pushes anything pending.
func (h *Hub) Hello(appID, side, _sid string, deliveredUpTo uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r := h.rooms[appID]
	if r == nil {
		return
	}
	mb := r.mboxes[side]
	if mb == nil {
		mb = &mailbox{}
		r.mboxes[side] = mb
	}
	if deliveredUpTo > mb.deliveredUpTo {
		mb.deliveredUpTo = deliveredUpTo
		// drop <= deliveredUpTo
		trimQueue(mb)
	}
	h.pushAllLocked(appID, side)
	h.touch(r)
}

// Enqueue adds a message for 'to' and attempts delivery.
func (h *Hub) Enqueue(appID, from, to string, payload json.RawMessage) error {
	if to != "A" && to != "B" {
		return errors.New("invalid 'to' (want A or B)")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	r := h.rooms[appID]
	if r == nil {
		r = &room{
			conns:        make(map[string]*connWrap, 2),
			sids:         make(map[string]string, 2),
			mboxes:       map[string]*mailbox{"A": {}, "B": {}},
			lastActivity: time.Now(),
		}
		h.rooms[appID] = r
	}
	mb := r.mboxes[to]
	if mb == nil {
		mb = &mailbox{}
		r.mboxes[to] = mb
	}

	seq := mb.nextSeq + 1
	mb.nextSeq = seq
	mb.queue = append(mb.queue, queued{seq: seq, from: from, payload: payload})
	if len(mb.queue) > maxMailboxQueued {
		// Drop oldest and signal pressure by forcing a close of the recipient (optional),
		// or return an error. Here we drop and keep going.
		return errors.New("backlog limit")
	}

	// best-effort push to online recipient
	h.pushAllLocked(appID, to)
	h.touch(r)

	return nil
}

// AckUpTo advances watermark and drops <= upTo for side.
func (h *Hub) AckUpTo(appID, side string, upTo uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r := h.rooms[appID]; r != nil {
		if mb := r.mboxes[side]; mb != nil {
			if upTo > mb.deliveredUpTo {
				mb.deliveredUpTo = upTo
				trimQueue(mb)
			}
			h.touch(r)
		}
	}
}

func trimQueue(mb *mailbox) {
	// mb.queue is sorted; drop from front while seq <= deliveredUpTo
	i := 0
	for i < len(mb.queue) && mb.queue[i].seq <= mb.deliveredUpTo {
		i++
	}
	if i > 0 {
		mb.queue = append([]queued{}, mb.queue[i:]...)
	}
}

// pushAllLocked snapshots deliverable frames (> deliveredUpTo) and sends them
// outside the hub lock. Safe because delivery is idempotent and ordered per seq.
func (h *Hub) pushAllLocked(appID, side string) {
	r := h.rooms[appID]
	if r == nil {
		return
	}
	wrap := r.conns[side]
	if wrap == nil {
		return
	}
	mb := r.mboxes[side]
	if mb == nil || len(mb.queue) == 0 {
		return
	}

	// Snapshot frames to send and the connection we plan to use.
	// Queue is append-only with strictly increasing seq; no need to sort.
	upTo := mb.deliveredUpTo
	frames := make([]deliverEnvelope, 0, len(mb.queue))
	for _, q := range mb.queue {
		if q.seq > upTo {
			frames = append(frames, deliverEnvelope{
				Type: "deliver", Seq: q.seq, From: q.from, Payload: q.payload,
			})
		}
	}
	// Exit early if nothing to do.
	if len(frames) == 0 {
		return
	}

	// Keep a stable pointer for “replaced conn” detection after unlock.
	cur := wrap
	h.mu.Unlock()
	// --- I/O outside hub lock ---
	for _, env := range frames {
		// If the side got replaced, stop sending the rest.
		h.mu.Lock()
		r2 := h.rooms[appID]
		same := (r2 != nil && r2.conns[side] == cur)
		h.mu.Unlock()
		if !same {
			break
		}

		cur.wmu.Lock()
		_ = cur.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := cur.ws.WriteJSON(env)
		cur.wmu.Unlock()
		if err != nil {
			_ = cur.ws.Close()
			go h.Unregister(appID, cur.ws)
			break
		}
	}
	// Re-acquire to bump activity
	h.mu.Lock()
	if r := h.rooms[appID]; r != nil {
		h.touch(r)
	}
	// caller expects us to hold h.mu on exit; keep that contract
}

func (h *Hub) WritePingConn(appID string, conn *websocket.Conn, deadline time.Duration) error {
	h.mu.Lock()
	key, ok := h.byConn[conn]
	if !ok {
		h.mu.Unlock()
		return errors.New("connection not registered")
	}
	r := h.rooms[appID]
	if r == nil {
		h.mu.Unlock()
		return errors.New("room not found")
	}
	wrap := r.conns[key.side]
	// only ping the same physical connection; if replaced, stop
	if wrap == nil || wrap.ws != conn {
		h.mu.Unlock()
		return errors.New("connection replaced")
	}
	// hold the wrap pointer; release hub lock before doing IO
	h.mu.Unlock()

	wrap.wmu.Lock()
	defer wrap.wmu.Unlock()
	_ = wrap.ws.SetWriteDeadline(time.Now().Add(deadline))
	return wrap.ws.WriteMessage(websocket.PingMessage, nil)
}
