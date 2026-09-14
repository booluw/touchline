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

	"github.com/google/uuid"
	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/eventoutbox"
	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/matchday"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/training"
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

	// S04-02: the live match engine consumes the daily world tick. Each daily
	// tick kicks off the world's due matchdays (fixtures → 'live' with a frozen
	// snapshot) and spawns a per-world pacing goroutine that advances those
	// matches in real time and applies the results at full time. KickoffDue is
	// idempotent and RunLive claims each world, so river's at-least-once
	// redelivery cannot double-advance a match or season. Only one worker pod
	// runs the live subsystem (advisory lock, see acquireMatchRunnerLock).
	matches := match.NewService(pool, bus, squad.NewStore(pool), form.NewStore(pool))
	compSvc := competition.NewService(pool, bus)
	trainingSvc := training.NewService(pool, bus)
	financeSvc := finance.NewService(pool, bus)
	matchdayRunner := matchday.NewRunner(pool, matches, compSvc)
	matchdayRunner.WithRealtime(realtimeBroker)

	runnerEnabled, releaseRunnerLock, err := acquireMatchRunnerLock(ctx, pool)
	if err != nil {
		log.Fatalf("match runner lock: %v", err)
	}
	if !runnerEnabled {
		log.Printf("another worker holds the match-runner lock; live subsystem disabled in this pod")
	} else {
		defer releaseRunnerLock()
	}

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

		if payload.Granularity == "daily" && runnerEnabled {
			sum, err := matchdayRunner.KickoffDue(ctx, ev.WorldID)
			if err != nil {
				log.Printf("world %s daily tick: kickoff: %v", ev.WorldID, err)
				// Returning the error lets river retry the job; KickoffDue only
				// touches fixtures still scheduled, so redelivery is safe.
				return err
			}
			if sum != nil && sum.Kicked > 0 {
				log.Printf("world %s daily tick: kicked %d matchday(s), %d fixture(s)",
					ev.WorldID, sum.Matchdays, sum.Kicked)
			}
			go func() {
				if err := matchdayRunner.RunLive(ctx, ev.WorldID); err != nil {
					log.Printf("world %s live runner: %v", ev.WorldID, err)
				}
			}()
		}
		if payload.Granularity == "weekly" {
			if _, err := trainingSvc.ApplyWeekly(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s weekly training: %w", ev.WorldID, err)
			}
		}
		if payload.Granularity == "monthly" {
			if _, err := financeSvc.ApplyMonthlyWages(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s monthly wages: %w", ev.WorldID, err)
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

	// Outbox repair sweep (OPD-23): every committed world.events row must have a
	// dispatch job. Producers write both in one tx, so this is defensive; it
	// re-enqueues any row that slipped through (pre-migration history, pruned
	// jobs, restored DBs) by its original id — idempotent via river's
	// unique-by-args. event_repair_sweep is the operational signal.
	go func() {
		sweepInterval := 60 * time.Second
		if raw := os.Getenv("EVENT_REPAIR_SWEEP_INTERVAL"); raw != "" {
			if d, err := time.ParseDuration(raw); err == nil && d > 0 {
				sweepInterval = d
			}
		}
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rep, err := eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{})
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("event_repair_sweep: error: %v", err)
					continue
				}
				if rep.Repaired > 0 || rep.OldestLagSeconds > 0 {
					log.Printf("event_repair_sweep scanned=%d repaired=%d oldest_lag_s=%.0f",
						rep.Scanned, rep.Repaired, rep.OldestLagSeconds)
				}
			}
		}
	}()

	// Startup sweep (OPD-21 rehydration): a pod restart during a live match
	// resumes every in-progress match of every world. The claim guard plus the
	// advisory lock keep it a single runner.
	if runnerEnabled {
		go func() {
			worlds, err := matchdayRunner.WorldsWithLiveMatches(ctx)
			if err != nil {
				log.Printf("live startup sweep: %v", err)
				return
			}
			for _, w := range worlds {
				go func(worldID uuid.UUID) {
					if err := matchdayRunner.RunLive(ctx, worldID); err != nil {
						log.Printf("world %s live rehydrate: %v", worldID, err)
					}
				}(w)
			}
		}()
	}

	<-ctx.Done()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := bus.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("stop: %v", err)
	}
	log.Printf("worker stopped")
}

// acquireMatchRunnerLock elects a single worker pod to run the live match
// subsystem (OPD-21) via a Postgres advisory lock, mirroring the scheduler's
// leader election. The lock is bound to one dedicated connection held for the
// worker's lifetime; the runner releases it on shutdown. A non-leader pod
// still serves river events and realtime fan-out, but skips kickoffs.
func acquireMatchRunnerLock(ctx context.Context, pool *pgxpool.Pool) (bool, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("match runner: acquire lock connection: %w", err)
	}
	for {
		var got bool
		if err := conn.QueryRow(ctx,
			`SELECT pg_try_advisory_lock(hashtext('touchline:match_runner'))`).Scan(&got); err != nil {
			conn.Release()
			return false, nil, fmt.Errorf("match runner: acquire lock: %w", err)
		}
		if got {
			return true, func() { conn.Release() }, nil
		}
		log.Printf("match runner: waiting for another worker to release the runner lock")
		select {
		case <-ctx.Done():
			conn.Release()
			return false, nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
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
