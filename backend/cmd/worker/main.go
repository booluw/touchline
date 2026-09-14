// Command worker runs the Touchline event consumer + live match engine
// standalone (probe :8082). Prefer `go run ./cmd/touchline serve` (one process
// for the whole game); this binary exists for prod isolation (worker pods, with
// a single match-runner leader elected by advisory lock).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/touchline/backend/internal/app"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := app.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	a, err := app.Build(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	port := envOr("WORKER_PORT", "8082")
	go serveHealth(port)

	if err := a.RunWorker(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker: %v", err)
	}
	log.Printf("worker stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// serveHealth exposes the liveness endpoint used by Docker Compose.
func serveHealth(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	log.Printf("health server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Printf("health server: %v", err)
	}
}
