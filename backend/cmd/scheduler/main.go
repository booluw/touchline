// Command scheduler runs the Touchline world clock standalone (probe :8081).
// Prefer `go run ./cmd/touchline serve` (one process for the whole game); this
// binary exists for prod isolation (one scheduler leader pod).
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

	port := envOr("SCHEDULER_PORT", "8081")
	go serveHealth(port)

	if err := a.RunScheduler(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("scheduler: %v", err)
	}
	log.Printf("scheduler stopped")
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
