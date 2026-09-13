// Package eventoutbox holds the repair-side of the transactional outbox
// (OPD-23). Producers write the world.events row and its dispatch job in one
// transaction, so a committed event is normally never undispatched. The sweep
// here closes the residual gap: rows that somehow reached world.events without
// a matching river job (pre-OPD-23 rows, a job row pruned, a replayed restore)
// are re-enqueued by their ORIGINAL id, preserving every causal chain keyed on
// event.ID.
package eventoutbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// Enqueuer re-enqueues the dispatch job for an already-persisted event.
// *eventbus.RiverBus satisfies it via EnqueueRepair.
type Enqueuer interface {
	EnqueueRepair(ctx context.Context, eventID uuid.UUID) error
}

// Report summarizes one sweep pass for the event_repair_sweep operational log.
type Report struct {
	Scanned  int // world.events rows reviewed
	Repaired int // ids re-enqueued (unique-by-args means each dispatches once)
	// OldestLagSeconds is seconds between the oldest *still-undispatched* row
	// (before this pass) and now; 0 when nothing was repaired.
	OldestLagSeconds float64
}

// Options tune a sweep pass.
type Options struct {
	// Schema is the river schema holding river.job (defaults to the eventbus
	// default river schema).
	Schema string
	// Limit bounds how many rows one pass re-enqueues, so a sweep is a
	// cooperative, incremental goroutine instead of a blocking full scan.
	Limit int
}

// Sweep re-enqueues every world.events row that has no touchline_event river
// job for its id. Idempotent by construction: the river job is unique by args,
// so a second pass over the same rows is a no-op and a concurrent producer
// enqueuing the same event races to the same idempotent point.
func Sweep(ctx context.Context, pool *pgxpool.Pool, enq Enqueuer, opts Options) (Report, error) {
	schema := opts.Schema
	if schema == "" {
		schema = eventbus.DefaultRiverSchema
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}

	var rep Report
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events`).Scan(&rep.Scanned); err != nil {
		return rep, fmt.Errorf("eventoutbox: count events: %w", err)
	}

	rows, err := pool.Query(ctx, fmt.Sprintf(`
		SELECT e.id, e.occurred_at
		FROM world.events e
		LEFT JOIN %s.river_job j
		  ON j.kind = 'touchline_event' AND j.args->>'event_id' = e.id::text
		WHERE j.id IS NULL
		ORDER BY e.occurred_at
		LIMIT $1`, schema), limit)
	if err != nil {
		return rep, fmt.Errorf("eventoutbox: find undispatched events: %w", err)
	}
	missing := make([]struct {
		id         uuid.UUID
		occurredAt time.Time
	}, 0)
	for rows.Next() {
		var m struct {
			id         uuid.UUID
			occurredAt time.Time
		}
		if err := rows.Scan(&m.id, &m.occurredAt); err != nil {
			rows.Close()
			return rep, fmt.Errorf("eventoutbox: scan missing event: %w", err)
		}
		missing = append(missing, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, fmt.Errorf("eventoutbox: iterate missing events: %w", err)
	}

	for _, m := range missing {
		if err := enq.EnqueueRepair(ctx, m.id); err != nil {
			return rep, fmt.Errorf("eventoutbox: re-enqueue %s: %w", m.id, err)
		}
		rep.Repaired++
		if rep.OldestLagSeconds == 0 {
			rep.OldestLagSeconds = time.Since(m.occurredAt).Seconds()
		}
	}
	return rep, nil
}
