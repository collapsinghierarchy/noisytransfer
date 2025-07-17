package handler

import (
	"encoding/json"
	"net/http"

	"github.com/collapsinghierarchy/noisytransfer/hub"
	"github.com/collapsinghierarchy/noisytransfer/service"
	"github.com/gorilla/websocket"
)

// ---------- REST ----------
func SetupAPIRoutes(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

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
		svc.RegisterKey(req.AppID, req.Pub)
	})

	mux.HandleFunc("/pub", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		appID := r.URL.Query().Get("appID")
		if pub, ok := svc.GetKey(appID); ok {
			json.NewEncoder(w).Encode(map[string]string{"appID": appID, "pub": pub})
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

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
		svc.PushBlob(req.AppID, req.Blob)
	})

	mux.HandleFunc("/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		appID := r.URL.Query().Get("appID")
		json.NewEncoder(w).Encode(svc.PullBlobs(appID))
	})

	return mux
}

// ---------- WebSocket ----------
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func WSHandler(hub *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Query().Get("appID")
		if appID == "" {
			http.Error(w, "appID required", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		if err := hub.Register(appID, conn); err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("error: room full"))
			conn.Close()
			return
		}
		defer func() { hub.Unregister(appID, conn); conn.Close() }()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil || mt != websocket.TextMessage {
				break
			}
			hub.Broadcast(appID, conn, msg)
		}
	}
}
