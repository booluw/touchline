package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/matchday"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
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

	// Realtime fan-out transport (S02-04): every WORLD_TICK is pushed to
	// connected WebSocket clients through Redis, so API pods (and browsers) see
	// ticks live. Degrades to the in-process broker when Redis is unavailable;
	// Redis is never authoritative gameplay storage.
	realtimeBroker := newRealtimeBroker(ctx)
	defer realtimeBroker.Close()

	// S04-02: the deterministic match engine consumes the daily world tick.
	// Each daily tick advances the world's due matchdays and applies the
	// simulated results to the standings. RunDue is idempotent, so river's
	// at-least-once redelivery cannot double-advance a season.
	matches := match.NewService(pool, bus, squad.NewStore(pool), form.NewStore(pool))
	compSvc := competition.NewService(pool, bus)
	matchdayRunner := matchday.NewRunner(pool, matches, compSvc)

	// Phase 0 handler: proves the publish -> queue -> consume round-trip and
	// demonstrates granularity-aware dispatch (S02-03). Engine handlers consume
	// only the WORLD_TICK granularities they own by inspecting payload
	// granularity; everything else is ignored. Ticks are dispatched to engine
	// handlers as those engines land (S03-01 onwards).
	if err := bus.Subscribe(ctx, "WORLD_TICK", func(ev eventbus.Event) error {
		var payload struct {
			Granularity string `json:"granularity"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			log.Printf("world tick %s: unreadable payload (%v); skipping", ev.ID, err)
			return nil
		}
		log.Printf("handled event %s (%s, granularity %s) for world %s at tick %d",
			ev.ID, ev.EventType, payload.Granularity, ev.WorldID, ev.WorldTick)

		tickEvent, err := realtime.BuildWorldTick(ev.WorldID, ev.ID.String(), payload.Granularity, ev.WorldTick)
		if err != nil {
			log.Printf("world tick %s: skip realtime push (%v)", ev.ID, err)
			return nil
		}
		if err := realtimeBroker.Publish(ctx, tickEvent); err != nil {
			log.Printf("world tick %s: realtime push failed (%v)", ev.ID, err)
			return nil
		}

		if payload.Granularity == "daily" {
			sum, err := matchdayRunner.RunDue(ctx, ev.WorldID)
			if err != nil {
				log.Printf("world %s daily tick: advance matchday: %v", ev.WorldID, err)
				// Returning the error lets river retry the job; RunDue only
				// touches fixtures still scheduled, so redelivery is safe.
				return err
			}
			if sum != nil && (sum.Played > 0 || sum.Applied > 0) {
				log.Printf("world %s daily tick: advanced %d matchday(s), played %d, applied %d",
					ev.WorldID, sum.Matchdays, sum.Played, sum.Applied)
			}
		}
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

// newRealtimeBroker builds the Redis pub/sub transport for realtime fan-out,
// degrading gracefully to the in-process broker when Redis is unavailable so
// bare `go run` still works.
func newRealtimeBroker(ctx context.Context) realtime.Broker {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return realtime.NewLocalBroker()
	}
	broker, err := realtime.NewRedisBroker(ctx, url)
	if err != nil {
		log.Printf("warning: REDIS_URL unreachable (%v); falling back to in-process realtime fan-out", err)
		return realtime.NewLocalBroker()
	}
	return broker
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
