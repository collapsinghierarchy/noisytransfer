package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/collapsinghierarchy/noisytransfer/hub"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
)

type helloMsg struct {
	Type          string `json:"type"` // "hello"
	SessionID     string `json:"sessionId,omitempty"`
	DeliveredUpTo uint64 `json:"deliveredUpTo"`
}

type sendMsg struct {
	Type    string          `json:"type"` // "send"
	To      string          `json:"to"`   // "A"|"B"
	Payload json.RawMessage `json:"payload"`
}

type deliveredMsg struct {
	Type string `json:"type"` // "delivered"
	UpTo uint64 `json:"upTo"`
}

func NewWSHandler(
	h *hub.Hub,
	allowedOrigins []string,
	lg *slog.Logger,
	dev bool,
) http.Handler {
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allow[o] = struct{}{}
	}

	up := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if dev {
				return true
			}
			origin := r.Header.Get("Origin")
			_, ok := allow[origin]
			return ok
		},
		// Reasonable buffer sizes for larger frames
		ReadBufferSize:  64 << 10,
		WriteBufferSize: 64 << 10,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Query().Get("appID")
		if _, err := uuid.Parse(appID); err != nil {
			http.Error(w, "invalid appID", http.StatusBadRequest)
			return
		}

		side := r.URL.Query().Get("side")
		if side != "A" && side != "B" {
			http.Error(w, "invalid side (want A or B)", http.StatusBadRequest)
			return
		}

		sessionID := r.URL.Query().Get("sid") // optional; client may pass empty

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			lg.Warn("upgrade failed", "err", err)
			return
		}
		defer conn.Close()

		// Set some sane timeouts + pong handler
		conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		// Register A/B mailbox connection in the Hub.
		if err := h.Register(appID, side, sessionID, conn); err != nil {
			lg.Warn("hub register failed", "err", err, "appID", appID, "side", side)
			_ = conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()),
			)
			return
		}
		defer h.Unregister(appID, conn)

		// If both sides are present, tell both (compat signal for your tests/UI)
		if h.RoomSize(appID) == 2 {
			lg.Info("Room full - broadcasting", "sys", "ws", "appID", appID)
			h.BroadcastEvent(appID, map[string]any{"type": "room_full"})
		}

		go func() {
			ticker := time.NewTicker(pingPeriod)
			defer ticker.Stop()
			for range ticker.C {
				// Mailbox (A/B) → ping via hub (exact-connection check).
				if err := h.WritePingConn(appID, conn, writeWait); err != nil {
					_ = conn.Close()
					return
				}
			}
		}()

		// Process messages
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				lg.Warn("ReadMessage error – closing connection", "sys", "ws", "err", err)
				return
			}
			if mt != websocket.TextMessage && mt != websocket.BinaryMessage {
				continue
			}

			var peek struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg, &peek); err != nil {
				lg.Warn("bad json", "err", err)
				continue
			}

			// non-cache lane: mailbox + webrtc signaling
			switch strings.ToLower(peek.Type) {
			case "offer", "answer", "ice":
				h.Broadcast(appID, conn, msg)

			case "hello":
				var m helloMsg
				if err := json.Unmarshal(msg, &m); err != nil {
					lg.Warn("hello unmarshal", "err", err)
					continue
				}
				h.Hello(appID, side, sessionID, m.DeliveredUpTo)

			case "send":
				var m sendMsg
				if err := json.Unmarshal(msg, &m); err != nil {
					lg.Warn("send unmarshal", "err", err)
					continue
				}
				if err := h.Enqueue(appID, side, m.To, m.Payload); err != nil {
					lg.Warn("send enqueue failed", "err", err)
				}

			case "delivered":
				var m deliveredMsg
				if err := json.Unmarshal(msg, &m); err != nil {
					lg.Warn("delivered unmarshal", "err", err)
					continue
				}
				h.AckUpTo(appID, side, m.UpTo)

			default:
				lg.Info("Ignoring unknown frame", "type", peek.Type)
			}
		}
	})
}
