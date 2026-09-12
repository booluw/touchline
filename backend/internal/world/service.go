package world

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// Sentinel errors. Handlers map them to HTTP status codes; everything else
// surfaces as 500.
var (
	ErrWorldNotFound     = errors.New("world not found")
	ErrInvalidTransition = errors.New("invalid world status transition")
	ErrNameCollision     = errors.New("world name already taken")
)

// defaultConfigKeys are the runtime cadences seeded at launch (S02-03 reads
// these exact keys). Values are JSON-configurable per world, never compiled-in
// — the scheduling contract lives in the DB, not the code.
var defaultConfigKeys = map[string]any{
	"tick.match_cadence":    "*/15 * * * *",
	"tick.hourly_cadence":   "0 * * * *",
	"tick.daily_cadence":    "0 0 * * *",
	"tick.weekly_cadence":   "0 0 * * 0",
	"tick.monthly_cadence":  "0 0 1 * *",
	"tick.seasonal_cadence": "0 0 1 1 *",
}

// Publishable is the event sink used to fan lifecycle events out to
// subscribers (S02-03 onwards). May be nil: the authoritative log (world.events)
// is always written transactionally regardless of the bus.
type Publishable interface {
	Publish(ctx context.Context, event *eventbus.Event) error
}

// Service owns the world lifecycle contract (S02-02): worlds are created in
// 'provisioning', launched into 'active'/'open_beta' (playable), paused and
// resumed, and archived (terminal). All engines are already world_id-scoped by
// the schema, so every subsequent operation inherits the world boundary.
type Service struct {
	pool *pgxpool.Pool
	bus  Publishable
}

// NewService builds the world lifecycle service.
func NewService(pool *pgxpool.Pool, bus Publishable) *Service {
	return &Service{pool: pool, bus: bus}
}

// Playable reports whether a world accepts gameplay operations (assignments,
// offers, scheduler ticks). Provisional and archived worlds reject them.
func Playable(status string) bool {
	return status == "active" || status == "open_beta"
}

// CreateWorld provisions a new world. It starts 'provisioning' — admin must
// launch it before anything gameplay can happen.
func (s *Service) CreateWorld(ctx context.Context, name string) (*World, error) {
	if name == "" {
		return nil, fmt.Errorf("world name is required")
	}

	w := &World{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO world.worlds (name, status) VALUES ($1, 'provisioning') RETURNING id, name, status, created_at`,
		name,
	).Scan(&w.ID, &w.Name, &w.Status, &w.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrNameCollision
	}
	if err != nil {
		return nil, fmt.Errorf("create world: %w", err)
	}

	if err := s.writeEvent(ctx, w.ID, "WORLD_CREATED", systemActor, 0, nil); err != nil {
		return nil, err
	}
	return w, nil
}

// GetWorld fetches a single world.
func (s *Service) GetWorld(ctx context.Context, id uuid.UUID) (*World, error) {
	w := &World{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, status, created_at FROM world.worlds WHERE id = $1`, id,
	).Scan(&w.ID, &w.Name, &w.Status, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get world: %w", err)
	}
	return w, nil
}

// ListWorlds returns all worlds, newest first.
func (s *Service) ListWorlds(ctx context.Context) ([]*World, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, status, created_at FROM world.worlds ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list worlds: %w", err)
	}
	defer rows.Close()

	var out []*World
	for rows.Next() {
		w := &World{}
		if err := rows.Scan(&w.ID, &w.Name, &w.Status, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan world: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SetStatus applies a lifecycle transition.
//
//	provisioning | open_beta -> active   (launch; seeds world_config, sets launched_at)
//	active | open_beta        -> paused
//	paused                    -> active
//	any non-terminal          -> archived (terminal)
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, to string) (*World, error) {
	if to != "active" && to != "paused" && to != "open_beta" && to != "archived" {
		return nil, ErrInvalidTransition
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transition tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		from       string
		launchedAt any
	)
	err = tx.QueryRow(ctx,
		`SELECT status, launched_at FROM world.worlds WHERE id = $1 FOR UPDATE`, id,
	).Scan(&from, &launchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if !canTransition(from, to) {
		return nil, ErrInvalidTransition
	}

	switch to {
	case "active":
		// launching: stamp launch time once, seed runtime cadence defaults
		if _, err := tx.Exec(ctx, `
			UPDATE world.worlds SET status = 'active', launched_at = COALESCE(launched_at, now())
			WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("launch world: %w", err)
		}
		for key, val := range defaultConfigKeys {
			raw, err := json.Marshal(val)
			if err != nil {
				return nil, fmt.Errorf("marshal config %s: %w", key, err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO world.world_config (world_id, config_key, config_value)
				VALUES ($1, $2, $3)
				ON CONFLICT (world_id, config_key) DO NOTHING`, id, key, raw); err != nil {
				return nil, fmt.Errorf("seed config %s: %w", key, err)
			}
		}
	case "paused":
		if _, err := tx.Exec(ctx, `UPDATE world.worlds SET status = 'paused' WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("pause world: %w", err)
		}
	case "archived":
		if _, err := tx.Exec(ctx, `UPDATE world.worlds SET status = 'archived' WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("archive world: %w", err)
		}
	case "open_beta":
		if _, err := tx.Exec(ctx, `UPDATE world.worlds SET status = 'open_beta' WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("beta world: %w", err)
		}
	}

	eventType := "WORLD_" + toUpper(to)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transition: %w", err)
	}

	if err := s.writeEvent(ctx, id, eventType, systemActor, 0, nil); err != nil {
		return nil, err
	}
	return s.GetWorld(ctx, id)
}

func (s *Service) writeEvent(ctx context.Context, worldID uuid.UUID, eventType, actorType string, worldTick int64, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}

	var e eventbus.Event
	err = s.pool.QueryRow(ctx, `
		INSERT INTO world.events (world_id, world_tick, event_type, actor_type, payload)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, occurred_at`,
		worldID, worldTick, eventType, actorType, raw,
	).Scan(&e.ID, &e.OccurredAt)
	if err != nil {
		return fmt.Errorf("record %s event: %w", eventType, err)
	}
	e.WorldID = worldID
	e.WorldTick = worldTick
	e.EventType = eventType
	e.Payload = raw
	actor := actorType
	e.ActorType = &actor

	if s.bus == nil {
		return nil
	}
	if err := s.bus.Publish(ctx, &e); err != nil {
		return fmt.Errorf("publish %s event: %w", eventType, err)
	}
	return nil
}

func canTransition(from, to string) bool {
	switch to {
	case "active":
		return from == "provisioning" || from == "open_beta" || from == "paused"
	case "open_beta":
		return from == "provisioning"
	case "paused":
		return from == "active" || from == "open_beta"
	case "archived":
		return from == "provisioning" || from == "open_beta" || from == "active" || from == "paused"
	}
	return false
}

const systemActor = "system"

func toUpper(s string) string {
	switch s {
	case "open_beta":
		return "OPEN_BETA"
	case "paused":
		return "PAUSED"
	case "active":
		return "ACTIVE"
	case "archived":
		return "ARCHIVED"
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
