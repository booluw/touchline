// Command api serves the Touchline HTTP + WebSocket surface standalone.
// Prefer `go run ./cmd/touchline serve` (one process for the whole game); this
// binary exists for prod isolation (separate API pods with the shared
// internal/app construction).
package main

import (
	"context"
	"errors"
	"log"
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

	if err := a.RunAPI(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("api: %v", err)
	}
	log.Printf("api stopped")
}
