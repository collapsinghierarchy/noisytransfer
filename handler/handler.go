package handler

import (
	"net/http"

	"github.com/collapsinghierarchy/noisytransfer/hub"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// NewWSHandler returns an http.Handler serving a single /ws endpoint.
func NewWSHandler(h *hub.Hub, allowedOrigins []string) http.Handler {
	// build origin lookup
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = struct{}{}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			_, ok := origins[r.Header.Get("Origin")]
			return ok
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "upgrade failed", http.StatusBadRequest)
			return
		}

		if err := h.Register(appID, conn); err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("error: room full"))
			conn.Close()
			return
		}
		defer func() {
			h.Unregister(appID, conn)
			conn.Close()
		}()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil || mt != websocket.TextMessage {
				break
			}
			h.Broadcast(appID, conn, msg)
		}
	})
}
