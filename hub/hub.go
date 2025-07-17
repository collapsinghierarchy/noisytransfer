package hub

import (
	"errors"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub groups live WebSockets by appID (room). Room size hard‑capped at 2.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[*websocket.Conn]struct{}
}

func NewHub() *Hub { return &Hub{rooms: make(map[string]map[*websocket.Conn]struct{})} }

func (h *Hub) Register(appID string, c *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[appID] == nil {
		h.rooms[appID] = make(map[*websocket.Conn]struct{})
	}
	if len(h.rooms[appID]) >= 2 {
		return errors.New("room full (max 2 peers)")
	}
	h.rooms[appID][c] = struct{}{}
	return nil
}

func (h *Hub) Unregister(appID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns := h.rooms[appID]; conns != nil {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.rooms, appID)
		}
	}
}

func (h *Hub) Broadcast(appID string, sender *websocket.Conn, msg []byte) {
	h.mu.Lock()
	conns := h.rooms[appID]
	h.mu.Unlock()
	for c := range conns {
		if c == sender {
			continue
		}
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("broadcast write error: %v", err)
		}
	}
}
