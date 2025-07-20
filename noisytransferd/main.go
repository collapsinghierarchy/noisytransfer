// main.go
package main

import (
	"log"
	"net/http"

	"github.com/collapsinghierarchy/noisytransfer/handler"
	"github.com/collapsinghierarchy/noisytransfer/hub"
	"github.com/collapsinghierarchy/noisytransfer/service"
)

func main() {
	// 1) Instantiate in‑memory storage & pub/pull service
	svc := service.NewService() // Service keeps keys & message blobs

	// 2) Create a Hub with a 32‑message buffer per connection
	h := hub.NewHub(32)

	// 3) Define which Origins you’ll accept WebSocket connections from
	allowed := []string{
		"localhost:1234",
	}

	// 4) Build your WS handler (with strict origin check & buffered queues)
	wsHandler := handler.NewWSHandler(h, allowed)

	// 5) Wire up HTTP routes
	mux := http.NewServeMux()
	mux.Handle("/api/",
		http.StripPrefix("/api", handler.SetupAPIRoutes(svc)),
	) // /api/key, /api/pub, /api/push, /api/pull
	mux.Handle("/ws", wsHandler) // WebSocket endpoint

	// 6) Start the server
	addr := ":1234"
	log.Printf("Listening on %s …", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
