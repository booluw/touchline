package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	workerPort := os.Getenv("WORKER_PORT")
	if workerPort == "" {
		workerPort = "8082"
	}

	pool, err := connectDB()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	go serveHealth(workerPort)

	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		log.Fatalf("init event bus: %v", err)
	}

	// Phase 0 handler: proves the publish -> queue -> consume round-trip.
	// Engine handlers (WORLD_TICK dispatch, match ticks, economic/social,
	// daily/weekly/monthly/seasonal) register here as they land.
	if err := bus.Subscribe(ctx, "WORLD_TICK", func(ev eventbus.Event) error {
		log.Printf("handled event %s (%s) for world %s at tick %d", ev.ID, ev.EventType, ev.WorldID, ev.WorldTick)
		return nil
	}); err != nil {
		log.Fatalf("subscribe: %v", err)
	}

	if err := bus.Start(ctx); err != nil {
		log.Fatalf("start worker: %v", err)
	}
	log.Printf("worker started; consuming events from the event bus")

	<-ctx.Done()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := bus.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("stop: %v", err)
	}
	log.Printf("worker stopped")
}

// connectDB requires DATABASE_URL and fails fast with an actionable error if
// the variable is missing or the database is unreachable.
func connectDB() (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required — set it in .env (see docs/development.md)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("cannot reach PostgreSQL: %w", err)
	}
	return pool, nil
}

// serveHealth exposes a liveness endpoint used by Docker Compose. It runs until
// the process exits; errors are logged but never take the worker down.
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
