package main

import (
	"log"
	"net/http"

	"github.com/collapsinghierarchy/noisytransfer/handler"
	"github.com/collapsinghierarchy/noisytransfer/hub"
)

func main() {
	// Instantiate hub
	h := hub.NewHub()

	// Define allowed origins
	allowed := []string{
		"http://localhost:9200",
	}

	// Build WS handler
	ws := handler.NewWSHandler(h, allowed)

	// Setup HTTP mux
	mux := http.NewServeMux()
	mux.Handle("/ws", ws)

	// Start server
	addr := ":1234"
	log.Printf("Listening on %s…", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
