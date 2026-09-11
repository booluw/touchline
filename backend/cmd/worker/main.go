package main

import (
	"context"
	"errors"
	"log"
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

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

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