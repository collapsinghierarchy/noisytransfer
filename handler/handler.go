package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/collapsinghierarchy/noisytransfer/hub"
	"github.com/collapsinghierarchy/noisytransfer/service"
)

// SetupAPIRoutes mounts your /key, /pub, /push & /pull endpoints,
// validating each appID as a UUID before calling into Service.
func SetupAPIRoutes(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	// POST /key — register public key
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ AppID, Pub string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if _, err := uuid.Parse(req.AppID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}
		svc.RegisterKey(req.AppID, req.Pub)
	})

	// GET /pub?appID=… — fetch public key
	mux.HandleFunc("/pub", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}
		if pub, ok := svc.GetKey(appID); ok {
			json.NewEncoder(w).Encode(map[string]string{"appID": appID, "pub": pub})
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	// POST /push — store ciphertext
	mux.HandleFunc("/push", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ AppID, Blob string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if _, err := uuid.Parse(req.AppID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}
		svc.PushBlob(req.AppID, req.Blob)
	})

	// GET /pull?appID=… — retrieve & clear blobs
	mux.HandleFunc("/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(svc.PullBlobs(appID))
	})

	return mux
}

var upgrader = websocket.Upgrader{} // CheckOrigin is set by NewWSHandler

// NewWSHandler builds a WS handler with a fixed origin whitelist.
func NewWSHandler(h *hub.Hub, origins []string) http.Handler {
	// Build lookup map
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}

	upgr := upgrader
	upgr.CheckOrigin = func(r *http.Request) bool {
		_, ok := allowed[r.Header.Get("Origin")]
		return ok
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate appID
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}

		conn, err := upgr.Upgrade(w, r, nil)
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

		// Read loop
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil || mt != websocket.TextMessage {
				break
			}
			h.Broadcast(appID, conn, msg)
		}
	})
}
