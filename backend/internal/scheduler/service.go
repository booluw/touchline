// Package scheduler owns the configurable world clock (S02-03): it reads tick
// cadence strings from world.world_config and fires world-scoped WORLD_TICK
// events on that runtime schedule. Cadences are re-read every poll interval, so
// a config change takes effect without an application redeploy.
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/touchline/backend/pkg/eventbus"
)

// WorldClockGranularities are the cadences driven by the world clock (technical
// plan §5). Match ticks are owned by the per-live-match goroutines of the match
// engine, not by this scheduler, so they are absent here.
var WorldClockGranularities = []string{"hourly", "daily", "weekly", "monthly", "seasonal"}

// Publishable is the event sink used to fan ticks out to subscribers. May be
// nil: the authoritative log (world.events) is always written regardless.
type Publishable interface {
	Publish(ctx context.Context, event *eventbus.Event) error
}

// JobScheduler registers cron specs. *cron.Cron satisfies it in production;
// tests substitute a fake so entries can be triggered deterministically.
type JobScheduler interface {
	AddFunc(spec string, cmd func()) (cron.EntryID, error)
	Remove(id cron.EntryID)
	Start()
	Stop() context.Context
}

// ref identifies one scheduled cadence: a granularity within a world.
type ref struct {
	worldID     uuid.UUID
	granularity string
}

// Service runs one cron entry per playable world per cadence granularity.
type Service struct {
	pool *pgxpool.Pool
	bus  Publishable
	cron JobScheduler

	mu      sync.RWMutex
	entries map[ref]cron.EntryID
	specs   map[ref]string
}

// NewService builds the world clock over robfig/cron.
func NewService(pool *pgxpool.Pool, bus Publishable) *Service {
	return &Service{
		pool:    pool,
		bus:     bus,
		cron:    cron.New(),
		entries: make(map[ref]cron.EntryID),
		specs:   make(map[ref]string),
	}
}

// newServiceWith injects a scheduler implementation (tests).
func newServiceWith(pool *pgxpool.Pool, bus Publishable, sched JobScheduler) *Service {
	s := NewService(pool, bus)
	s.cron = sched
	return s
}

// Run drives the world clock until ctx is cancelled. It first acquires a
// Postgres advisory lock so only one scheduler instance is the leader (a second
// instance would double-fire every cadence), then reconciles cron registrations
// against the current playable-world configuration every poll.
func (s *Service) Run(ctx context.Context, poll time.Duration) error {
	release, err := s.acquireLeaderLock(ctx)
	if err != nil {
		return err
	}
	defer release()

	s.cron.Start()
	defer s.cron.Stop()

	if err := s.sync(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// A transient DB hiccup must not kill the world clock.
			if err := s.sync(ctx); err != nil {
				log.Printf("scheduler: sync: %v", err)
			}
		}
	}
}

// acquireLeaderLock blocks until this process holds the scheduler advisory
// lock. The lock is bound to one dedicated connection held for the whole Run;
// releasing the connection on shutdown drops the lock. The lock releases
// automatically if the process dies.
func (s *Service) acquireLeaderLock(ctx context.Context) (func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: acquire leader connection: %w", err)
	}
	for {
		var got bool
		if err := conn.QueryRow(ctx,
			`SELECT pg_try_advisory_lock(hashtext('touchline:scheduler'))`).Scan(&got); err != nil {
			conn.Release()
			return nil, fmt.Errorf("scheduler: acquire leader lock: %w", err)
		}
		if got {
			return func() { conn.Release() }, nil
		}
		log.Printf("scheduler: waiting for another scheduler instance to release the leader lock")
		select {
		case <-ctx.Done():
			conn.Release()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// sync reads the cadence configuration of every playable world and reconciles
// the cron registry with it, so launches, pauses/resumes, archives, and cadence
// changes all take effect on the next poll.
func (s *Service) sync(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, c.config_key, c.config_value
		FROM world.worlds w
		JOIN world.world_config c ON c.world_id = w.id
		WHERE w.status IN ('active', 'open_beta')
		  AND c.config_key LIKE 'tick.%'
		ORDER BY w.id, c.config_key`)
	if err != nil {
		return fmt.Errorf("scheduler: load cadences: %w", err)
	}
	defer rows.Close()

	desired := make(map[ref]string)
	for rows.Next() {
		var (
			worldID uuid.UUID
			key     string
			raw     []byte
		)
		if err := rows.Scan(&worldID, &key, &raw); err != nil {
			return fmt.Errorf("scheduler: scan cadence: %w", err)
		}
		g, ok := granularityFromKey(key)
		if !ok {
			continue
		}
		var spec string
		if err := json.Unmarshal(raw, &spec); err != nil {
			log.Printf("scheduler: world %s key %s: cadence value is not a string, skipping", worldID, key)
			continue
		}
		desired[ref{worldID: worldID, granularity: g}] = spec
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.reconcile(desired)
}

// reconcile makes the cron registry match the desired cadence set. It is pure
// over in-memory state and the injected scheduler, so it is unit-testable.
func (s *Service) reconcile(desired map[ref]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for r, entryID := range s.entries {
		if _, ok := desired[r]; !ok {
			s.cron.Remove(entryID)
			delete(s.entries, r)
			delete(s.specs, r)
			log.Printf("scheduler: unregistered %s tick for world %s", r.granularity, r.worldID)
		}
	}

	for r, spec := range desired {
		if existing, ok := s.specs[r]; ok && existing == spec {
			continue
		}
		if entryID, ok := s.entries[r]; ok {
			s.cron.Remove(entryID)
		}
		entryID, err := s.cron.AddFunc(spec, func() { s.fireScheduledTick(r.worldID, r.granularity) })
		if err != nil {
			delete(s.entries, r)
			delete(s.specs, r)
			log.Printf("scheduler: world %s %s cadence %q: %v", r.worldID, r.granularity, spec, err)
			continue
		}
		s.entries[r] = entryID
		s.specs[r] = spec
		log.Printf("scheduler: registered %s tick for world %s (%s)", r.granularity, r.worldID, spec)
	}
	return nil
}

// fireScheduledTick is the cron callback: it runs on the scheduler's own
// goroutine and keeps short-lived failures out of the cron loop.
func (s *Service) fireScheduledTick(worldID uuid.UUID, granularity string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.FireTick(ctx, worldID, granularity); err != nil {
		log.Printf("scheduler: fire %s tick for world %s: %v", granularity, worldID, err)
	}
}

// FireTick advances a world's monotonic tick counter, records the WORLD_TICK
// event in the event log, and publishes it to subscribers. Worlds that are no
// longer playable (paused/archived) are a no-op, so a stale cron entry can
// never tick a world changed underneath the scheduler.
func (s *Service) FireTick(ctx context.Context, worldID uuid.UUID, granularity string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("scheduler: begin tick tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var tick int64
	err = tx.QueryRow(ctx, `
		UPDATE world.worlds SET current_tick = current_tick + 1
		WHERE id = $1 AND status IN ('active', 'open_beta')
		RETURNING current_tick`, worldID).Scan(&tick)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // world gone or not playable; nothing to tick
	}
	if err != nil {
		return fmt.Errorf("scheduler: advance world tick: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"granularity": granularity})
	actor := "system"
	ev := eventbus.Event{
		ID:        uuid.New(),
		WorldID:   worldID,
		WorldTick: tick,
		EventType: "WORLD_TICK",
		ActorType: &actor,
		Payload:   payload,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO world.events (id, world_id, world_tick, event_type, actor_type, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING occurred_at`, ev.ID, worldID, tick, "WORLD_TICK", "system", payload,
	).Scan(&ev.OccurredAt)
	if err != nil {
		return fmt.Errorf("scheduler: record WORLD_TICK event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("scheduler: commit tick: %w", err)
	}

	if s.bus == nil {
		return nil
	}
	if err := s.bus.Publish(ctx, &ev); err != nil {
		return fmt.Errorf("scheduler: publish WORLD_TICK: %w", err)
	}
	return nil
}

// granularityFromKey maps a world_config cadence key to a tick granularity.
// Keys follow 'tick.<name>_cadence' (e.g. tick.daily_cadence -> daily). The
// match cadence is deliberately excluded: live match ticks belong to the match
// engine's per-match goroutines, not the world clock.
func granularityFromKey(key string) (string, bool) {
	const (
		prefix    = "tick."
		suffix    = "_cadence"
		matchGran = "match"
	)
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return "", false
	}
	g := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
	if g == matchGran {
		return "", false
	}
	for _, known := range WorldClockGranularities {
		if g == known {
			return g, true
		}
	}
	return "", false
}
