package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/collapsinghierarchy/noisytransfer/hub"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// NewOriginChecker returns a function you can plug into websocket.Upgrader.CheckOrigin.
// allowedOrigins may include values like:
//
//	"api.whitenoise.systems"      → only exactly that host
//	".whitenoise.systems"         → that domain + any subdomain
//	"*.api.whitenoise.systems"    → same as ".api.whitenoise.systems"
//	"localhost"                   → localhost only
func NewOriginChecker(allowedOrigins []string) func(r *http.Request) bool {
	// normalize patterns: turn "*.foo" → ".foo"
	patterns := make([]string, len(allowedOrigins))
	for i, o := range allowedOrigins {
		if strings.HasPrefix(o, "*.") {
			patterns[i] = o[1:] // "*.foo" → ".foo"
		} else {
			patterns[i] = o
		}
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := u.Hostname()

		for _, pat := range patterns {
			switch {
			// leading “.” means match pat or any subdomain of pat
			case strings.HasPrefix(pat, "."):
				if host == pat[1:] || strings.HasSuffix(host, pat) {
					return true
				}
			default:
				// exact match only
				if host == pat {
					return true
				}
			}
		}
		return false
	}
}

// NewWSHandler returns an http.Handler serving a single /ws endpoint.
func NewWSHandler(h *hub.Hub, allowedOrigins []string) http.Handler {
	// build origin lookup
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = struct{}{}
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: NewOriginChecker(allowedOrigins),
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

		// If this is the second peer, let both know they can start the HPKE flow
		if h.RoomSize(appID) == 2 {
			h.BroadcastEvent(appID, map[string]string{"type": "room_full"})
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
