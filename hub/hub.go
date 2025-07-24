package hub

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub manages rooms of WebSocket connections.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*websocket.Conn]struct{}
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*websocket.Conn]struct{})}
}

// Register adds a connection under appID, enforcing max 2 peers.
func (h *Hub) Register(appID string, conn *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[appID] == nil {
		h.rooms[appID] = make(map[*websocket.Conn]struct{})
	}
	if len(h.rooms[appID]) >= 2 {
		return errors.New("room full (max 2 peers)")
	}

	h.rooms[appID][conn] = struct{}{}
	return nil
}

// Unregister removes a connection from the room.
func (h *Hub) Unregister(appID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conns := h.rooms[appID]; conns != nil {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.rooms, appID)
		}
	}
}

// Broadcast sends msg to the other peer immediately.
func (h *Hub) Broadcast(appID string, sender *websocket.Conn, msg []byte) {
	h.mu.Lock()
	conns := h.rooms[appID]
	h.mu.Unlock()

	for conn := range conns {
		if conn == sender {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			conn.Close()
			h.Unregister(appID, conn)
		}
	}
}

// RoomSize returns how many peers are in appID.
func (h *Hub) RoomSize(appID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms[appID])
}

// BroadcastEvent marshals 'evt' as JSON and sends it to all peers.
func (h *Hub) BroadcastEvent(appID string, evt interface{}) {
	h.mu.Lock()
	conns := h.rooms[appID]
	h.mu.Unlock()

	data, _ := json.Marshal(evt)
	for c := range conns {
		_ = c.WriteMessage(websocket.TextMessage, data)
	}
}
