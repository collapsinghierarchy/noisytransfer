package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/collapsinghierarchy/noisytransfer/api"
	"github.com/collapsinghierarchy/noisytransfer/handler"
	"github.com/collapsinghierarchy/noisytransfer/hub"
	"github.com/collapsinghierarchy/noisytransfer/storage"
	"github.com/collapsinghierarchy/noisytransfer/turn"
)

func main() {
	addr := flag.String("addr", ":1234", "HTTP listen address")
	dev := flag.Bool("dev", false, "allow empty Origin / any Origin on WebSocket upgrades")
	dataDir := flag.String("data", "./data", "data directory for objects")
	baseURL := flag.String("base", "http://localhost:1234", "public base URL")
	corsOrigin := flag.String("cors", "*", "CORS allowed origin")
	gcTTL := flag.Duration("gc_ttl", 24*time.Hour, "GC TTL for objects")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, err := storage.NewFSStore(*dataDir)
	if err != nil {
		log.Error("fsstore", "err", err)
		os.Exit(1)
	}

	apiSrv := &api.Server{Store: store, BaseURL: *baseURL, TTL: *gcTTL}

	mux := http.NewServeMux()
	h := hub.NewHub()

	ws := handler.NewWSHandler(h, []string{"http://localhost:9200"}, log.With("sys", "ws"), *dev)

	// WS mailbox stays exactly as you have it:
	mux.Handle("/ws", ws)

	// HTTP data plane:
	apiSrv.Register(mux)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           withCORS(mux, *corsOrigin),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 1) TURN (async)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	apiSrv.StartGC(ctx)

	go func() {
		if err := turn.Start(ctx, turn.Config{
			Realm:    "example.com",
			Username: "testuser",
			Password: "testpass",
			Logger:   log.With("sys", "turn"),
		}); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("turn server", "err", err)
		}
	}()

	// graceful shutdown
	go func() {
		log.Info("HTTP listen", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP serve", "err", err)
		}
	}()
	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("server stopped")
}

func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
