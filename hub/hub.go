// hub.go
package hub

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Hub manages rooms of WebSocket connections and per-connection message queues.
type Hub struct {
	mu        sync.Mutex
	rooms     map[string]map[*websocket.Conn]struct{}
	msgQueues map[*websocket.Conn]chan []byte
	queueSize int
}

// NewHub creates a Hub using the given buffer size for each connection's queue.
func NewHub(queueSize int) *Hub {
	if queueSize <= 0 {
		queueSize = 16 // default fallback
	}
	return &Hub{
		rooms:     make(map[string]map[*websocket.Conn]struct{}),
		msgQueues: make(map[*websocket.Conn]chan []byte),
		queueSize: queueSize,
	}
}

// Register adds a connection to the given appID room and starts its writePump.
func (h *Hub) Register(appID string, c *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[appID] == nil {
		h.rooms[appID] = make(map[*websocket.Conn]struct{})
	}
	if len(h.rooms[appID]) >= 2 {
		return errors.New("room full (max 2 peers)")
	}
	// Add conn
	h.rooms[appID][c] = struct{}{}

	// Create buffered queue
	q := make(chan []byte, h.queueSize)
	h.msgQueues[c] = q
	go h.writePump(c, q)

	// Optional: set read deadline
	c.SetReadDeadline(time.Now().Add(10 * time.Minute))
	return nil
}

// Unregister removes a connection and closes its queue.
func (h *Hub) Unregister(appID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conns := h.rooms[appID]; conns != nil {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.rooms, appID)
		}
	}
	if q, ok := h.msgQueues[c]; ok {
		close(q)
		delete(h.msgQueues, c)
	}
}

// Broadcast sends msg to all peers in room via non-blocking enqueue.
func (h *Hub) Broadcast(appID string, sender *websocket.Conn, msg []byte) {
	h.mu.Lock()
	conns := h.rooms[appID]
	queues := h.msgQueues
	h.mu.Unlock()

	for c := range conns {
		if c == sender {
			continue
		}
		select {
		case queues[c] <- msg:
			// enqueued
		default:
			// drop if full
		}
	}
}

// writePump drains the channel q and writes messages to the socket.
func (h *Hub) writePump(c *websocket.Conn, q chan []byte) {
	for msg := range q {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			_ = c.Close()
			return
		}
	}
}
