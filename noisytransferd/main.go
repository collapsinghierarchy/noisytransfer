package main

import (
	"log"
	"net/http"

	"github.com/collapsinghierarchy/noisytransfer/handler"
	"github.com/collapsinghierarchy/noisytransfer/hub"
	"github.com/collapsinghierarchy/noisytransfer/service"
)

func main() {
	hub := hub.NewHub()
	svc := service.NewService()

	root := http.NewServeMux()
	root.Handle("/api/", http.StripPrefix("/api", handler.SetupAPIRoutes(svc)))
	root.HandleFunc("/ws", handler.WSHandler(hub))

	log.Println("noisytransfer listening on :1234")
	log.Fatal(http.ListenAndServe(":1234", root))
}
