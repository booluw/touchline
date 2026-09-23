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

// DaysPerWeekDefault / DaysPerMonthDefault are the compiled fallbacks for the
// day-derived week and month steps (IM02) when a world has no
// calendar.days_per_week / calendar.days_per_month config row. The seeded
// defaults live in defaultConfigKeys; these only cover pre-launch or deleted
// rows.
const (
	DaysPerWeekDefault  = 7
	DaysPerMonthDefault = 30
)

// defaultConfigKeys are the runtime tuning defaults seeded at launch (IM02
// single-daily clock: S02-03 reads tick.daily_cadence; the calendar/season keys
// are gameplay steps read by the worker, competition, and academy engines).
// Values are JSON-configurable per world, never compiled-in — the scheduling
// contract lives in the DB, not the code.
// tick.daily_cadence is the ONE world-clock cadence (default 00:00 UTC, i.e. 1
// game-day per real day): each WORLD_TICK{daily} emission advances the calendar
// by exactly one game day (world.worlds.current_day, OPD-24). Weeks and months
// are derived from the day counter via calendar.days_per_week/days_per_month.
// season.off_season_ticks is the IM01 off-season gap in daily ticks the next
// rollover anchors the new season after (fallback default in the competition
// service: DefaultOffSeasonTicks).
var defaultConfigKeys = map[string]any{
	"tick.match_cadence":      "20s",
	"tick.daily_cadence":      "0 0 * * *",
	"calendar.days_per_week":  DaysPerWeekDefault,
	"calendar.days_per_month": DaysPerMonthDefault,
	"season.off_season_ticks": 30,
}

// Publishable is the event sink used to fan lifecycle events out to
// subscribers (S02-03 onwards). May be nil: the authoritative log (world.events)
// is always written transactionally regardless of the bus. Only the tx-scoped
// outbox method is required (OPD-23).
type Publishable interface {
	eventbus.Publisher
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create world tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	w := &World{}
	err = tx.QueryRow(ctx,
		`INSERT INTO world.worlds (name, status) VALUES ($1, 'provisioning') RETURNING id, name, status, created_at`,
		name,
	).Scan(&w.ID, &w.Name, &w.Status, &w.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrNameCollision
	}
	if err != nil {
		return nil, fmt.Errorf("create world: %w", err)
	}

	if err := s.record(ctx, tx, w.ID, "WORLD_CREATED", 0, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create world: %w", err)
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

// SetConfig upserts a runtime world_config key. A cadence change takes effect
// once the S02-03 scheduler re-reads config on its next poll — no redeploy.
func (s *Service) SetConfig(ctx context.Context, id uuid.UUID, key string, value any) error {
	if key == "" {
		return fmt.Errorf("config key is required")
	}
	if _, err := s.GetWorld(ctx, id); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal config value for %s: %w", key, err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO world.world_config (world_id, config_key, config_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (world_id, config_key)
		DO UPDATE SET config_value = EXCLUDED.config_value, updated_at = now()`,
		id, key, raw); err != nil {
		return fmt.Errorf("set config %s: %w", key, err)
	}
	return nil
}

// Calendar reads the world's day counter and the day-derived week/month steps
// (IM02: calendar.days_per_week / calendar.days_per_month, JSONB ints). Missing
// or non-numeric config rows fall back to DaysPerWeekDefault/DaysPerMonthDefault
// rather than failing the tick pass.
func (s *Service) Calendar(ctx context.Context, worldID uuid.UUID) (day int64, daysPerWeek, daysPerMonth int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT w.current_day,
		       COALESCE(CASE WHEN (dw.config_value #>> '{}') ~ '^-?[0-9]+$'
		                     THEN (dw.config_value #>> '{}')::int END, $2),
		       COALESCE(CASE WHEN (dm.config_value #>> '{}') ~ '^-?[0-9]+$'
		                     THEN (dm.config_value #>> '{}')::int END, $3)
		FROM world.worlds w
		LEFT JOIN world.world_config dw
		  ON dw.world_id = w.id AND dw.config_key = 'calendar.days_per_week'
		LEFT JOIN world.world_config dm
		  ON dm.world_id = w.id AND dm.config_key = 'calendar.days_per_month'
		WHERE w.id = $1`,
		worldID, DaysPerWeekDefault, DaysPerMonthDefault).Scan(&day, &daysPerWeek, &daysPerMonth)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, 0, ErrWorldNotFound
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("calendar config: %w", err)
	}
	if daysPerWeek < 1 {
		daysPerWeek = DaysPerWeekDefault
	}
	if daysPerMonth < 1 {
		daysPerMonth = DaysPerMonthDefault
	}
	return day, daysPerWeek, daysPerMonth, nil
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
		tick       int64
	)
	err = tx.QueryRow(ctx,
		`SELECT status, launched_at, current_tick FROM world.worlds WHERE id = $1 FOR UPDATE`, id,
	).Scan(&from, &launchedAt, &tick)
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
	// Record + enqueue inside the state tx (the transactional outbox, OPD-23):
	// a committed transition is never left without its dispatch job.
	if err := s.record(ctx, tx, id, eventType, tick, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transition: %w", err)
	}
	return s.GetWorld(ctx, id)
}

// record appends a lifecycle event to world.events and (when a bus is wired)
// enqueues its dispatch, all inside tx, stamping the world's current tick.
func (s *Service) record(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, worldTick int64, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	actor := systemActor
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: worldTick,
		EventType: eventType,
		ActorType: &actor,
		Payload:   raw,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s event: %w", eventType, err)
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
