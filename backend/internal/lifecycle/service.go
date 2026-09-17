// Package lifecycle implements the player aging-and-retirement engine (A06):
// at seasonal rollover the world ages implicitly (age derives from
// date_of_birth vs the reference date), older players probabilistically
// retire, their contracts terminate, and the free-agent pool is replenished.
// It is the rollover hook for both the league-driven SEASON_COMPLETED path and
// the league-less seasonal fallback.
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/academy"
	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// Service runs the seasonal player lifecycle.
type Service struct {
	pool *pgxpool.Pool
	bus  eventbus.Publisher
	acad *academy.Service
}

// NewService builds the lifecycle service.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher, acad *academy.Service) *Service {
	return &Service{pool: pool, bus: bus, acad: acad}
}

// Result summarises one rollover run.
type Result struct {
	Intake      *academy.IntakeResult `json:"intake"`
	Retired     int                   `json:"retired_count"`
	AutoFilled  int                   `json:"auto_filled"`
	PoolSize    int                   `json:"pool_size"`
	AlreadyDone bool                  `json:"already_done"`
}

// OnSeasonCompleted runs the rollover hook for a country (or the whole world
// when countryID is nil): club + street intake, retirement, pool replenish,
// AI squad auto-fill, and the WORLD_LIFECYCLE_SEASON_COMPLETED event. It is
// idempotent per (world, season): a redelivered event is a no-op guarded on
// the lifecycle event row. Academy intake is delegated to the academy service
// (its own transactions, idempotent via last_intake_season +
// country_academy_intakes); retirement + auto-fill + replenish + the summary
// event run in one tx so a committed rollover is never left half-applied.
func (s *Service) OnSeasonCompleted(ctx context.Context, worldID uuid.UUID, countryID *uuid.UUID, seasonNumber int, ref time.Time) (*Result, error) {
	res := &Result{}
	if err := s.acad.EnsureAcademies(ctx, worldID); err != nil {
		return res, err
	}

	// Club + street intake (ground-truth counts come from the academy pass).
	if countryID == nil {
		ir, err := s.acad.IntakeForWorld(ctx, worldID, seasonNumber, ref)
		if err != nil {
			return res, fmt.Errorf("lifecycle world intake: %w", err)
		}
		res.Intake = ir
	} else {
		ir, err := s.acad.IntakeForCountry(ctx, worldID, *countryID, seasonNumber, ref)
		if err != nil {
			return res, fmt.Errorf("lifecycle country intake: %w", err)
		}
		res.Intake = ir
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("lifecycle: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	done, err := lifecycleDone(ctx, tx, worldID, countryID, seasonNumber)
	if err != nil {
		return res, err
	}
	if done {
		res.AlreadyDone = true
		if err := tx.Commit(ctx); err != nil {
			return res, fmt.Errorf("lifecycle: commit: %w", err)
		}
		return res, nil
	}

	retired, err := s.retire(ctx, tx, worldID, seasonNumber, ref)
	if err != nil {
		return res, err
	}
	res.Retired = retired

	// Replenish per-country pool when scoped, otherwise the world pool.
	if err := s.replenish(ctx, tx, worldID, countryID, seasonNumber, ref); err != nil {
		return res, err
	}

	// AI clubs top their squads back up from the (now replenished) pool (A09).
	cid := countrySeed(countryID)
	filled, err := s.AutoFill(ctx, tx, worldID, cid, seasonNumber, ref)
	if err != nil {
		return res, err
	}
	res.AutoFilled = filled

	size, err := playerpool.PoolCount(ctx, tx, worldID, countryID)
	if err != nil {
		return res, err
	}
	res.PoolSize = size

	if err := s.emitLifecycleCompleted(ctx, tx, worldID, countryID, seasonNumber, res); err != nil {
		return res, err
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("lifecycle: commit: %w", err)
	}
	log.Printf("lifecycle: world %s season %d rolled over (%d retired, %d autofilled, pool=%d)",
		worldID, seasonNumber, retired, filled, size)
	return res, nil
}

// lifecycleDone reports whether the (world, season) rollover already ran. For
// a country-scoped call the guard is surfaced per country; for the world-wide
// fallback it keys on the season alone. Redelivery therefore never double-
// retirees or double-replenishes.
func lifecycleDone(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, countryID *uuid.UUID, season int) (bool, error) {
	if countryID != nil {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM world.events
				WHERE world_id = $1 AND event_type = 'WORLD_LIFECYCLE_SEASON_COMPLETED'
				  AND payload->>'country_id' = $2 AND payload->>'season_number' = $3)`,
			worldID, countryID.String(), fmt.Sprintf("%d", season)).Scan(&exists); err != nil {
			return false, fmt.Errorf("lifecycle guard: %w", err)
		}
		return exists, nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM world.events
			WHERE world_id = $1 AND event_type = 'WORLD_LIFECYCLE_SEASON_COMPLETED'
			  AND payload->>'season_number' = $2
			  AND jsonb_typeof(payload->'country_id') = 'null')`,
		worldID, fmt.Sprintf("%d", season)).Scan(&exists); err != nil {
		return false, fmt.Errorf("lifecycle guard: %w", err)
	}
	return exists, nil
}

// retire evaluates every active player in the world aged ≥ 30 and retires
// those whose probability roll succeeds. Age 37+ is forced. Each retirement
// terminates active contracts and emits PLAYER_RETIRED.
func (s *Service) retire(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, season int, ref time.Time) (int, error) {
	rng := rand.New(rand.NewSource(hashSeed(worldID, uuid.Nil, season)))

	rows, err := tx.Query(ctx, `
		SELECT p.id, p.club_id, p.origin,
		       COALESCE(EXTRACT(YEAR FROM age($2::date, pe.date_of_birth::date))::int, 0) AS age,
		       COALESCE((SELECT AVG(a.value) FROM player.player_attributes a WHERE a.player_id = p.id)::int, 0) AS ability_avg,
		       COALESCE(h.injury_susceptibility, 40) AS injury,
		       COALESCE(ps.ambition, 50) AS ambition
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_hidden_traits h ON h.player_id = p.id
		LEFT JOIN player.player_personality ps ON ps.player_id = p.id
		WHERE p.world_id = $1 AND p.status = 'active'
		  AND COALESCE(EXTRACT(YEAR FROM age($2::date, pe.date_of_birth::date))::int, 0) >= 30`,
		worldID, ref)
	if err != nil {
		return 0, fmt.Errorf("lifecycle retire scan: %w", err)
	}
	type candidate struct {
		playerID   uuid.UUID
		clubID     *uuid.UUID
		origin     string
		age        int
		abilityAvg int
		injury     int
		ambition   int
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.playerID, &c.clubID, &c.origin, &c.age,
			&c.abilityAvg, &c.injury, &c.ambition); err != nil {
			rows.Close()
			return 0, fmt.Errorf("lifecycle retire scan row: %w", err)
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("lifecycle retire iterate: %w", err)
	}

	retired := 0
	for _, c := range cands {
		p := RetireProbability(c.age, c.abilityAvg, c.injury, c.ambition)
		if rng.Float64() >= p {
			continue
		}
		if err := applyRetirement(ctx, s.bus, tx, worldID, season, ref, c); err != nil {
			return 0, err
		}
		retired++
	}
	return retired, nil
}

// applyRetirement sets a player retired, terminates active contracts, records
// the player-history mark, and emits PLAYER_RETIRED.
func applyRetirement(ctx context.Context, pub eventbus.Publisher, tx pgx.Tx,
	worldID uuid.UUID, season int, ref time.Time, c struct {
		playerID   uuid.UUID
		clubID     *uuid.UUID
		origin     string
		age        int
		abilityAvg int
		injury     int
		ambition   int
	},
) error {
	var clubID *uuid.UUID
	if c.clubID != nil {
		id := *c.clubID
		clubID = &id
	}
	if _, err := tx.Exec(ctx, `
		UPDATE player.players SET status = 'retired', club_id = NULL WHERE id = $1`, c.playerID); err != nil {
		return fmt.Errorf("lifecycle retire player: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE player.contracts SET status = 'terminated', end_date = $2::date
		WHERE player_id = $1 AND status = 'active'`, c.playerID, ref); err != nil {
		return fmt.Errorf("lifecycle terminate contract: %w", err)
	}
	actor := "system"
	payload, _ := json.Marshal(map[string]any{
		"player_id":   c.playerID,
		"club_id":     clubID,
		"age":         c.age,
		"ability_avg": c.abilityAvg,
		"origin":      c.origin,
		"season":      season,
	})
	if err := eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
		WorldID:    worldID,
		EventType:  EventPlayerRetired,
		ActorType:  &actor,
		Payload:    payload,
		RandomSeed: nil,
	}); err != nil {
		return fmt.Errorf("lifecycle record %s: %w", EventPlayerRetired, err)
	}
	return nil
}

// replenish tops the country (or world) free-agent pool back to target using
// a deterministically-seeded factory for the season.
func (s *Service) replenish(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, countryID *uuid.UUID, season int, ref time.Time) error {
	generator, natPool, err := bootstrap.LoadPools(ctx, tx)
	if err != nil {
		return fmt.Errorf("lifecycle replenish pools: %w", err)
	}
	registry := playergen.NewNameRegistry()
	factory := playergen.NewPlayerFactory(generator, natPool,
		rand.New(rand.NewSource(hashSeed(worldID, countrySeed(countryID), season)))).WithRegistry(registry)
	if err := playerpool.ReplenishPool(ctx, tx, s.bus, worldID, countryID, playerpool.PoolTargetSize, factory, ref); err != nil {
		return fmt.Errorf("lifecycle replenish: %w", err)
	}
	return nil
}

// emitLifecycleCompleted writes the WORLD_LIFECYCLE_SEASON_COMPLETED summary.
func (s *Service) emitLifecycleCompleted(ctx context.Context, tx pgx.Tx,
	worldID uuid.UUID, countryID *uuid.UUID, season int, res *Result,
) error {
	payload, _ := json.Marshal(map[string]any{
		"country_id":    countryID,
		"season_number": season,
		"retired_count": res.Retired,
		"academy_count": academyCount(res.Intake),
		"street_count":  streetCount(res.Intake),
		"pool_size":     res.PoolSize,
	})
	actor := "system"
	return eventbus.WriteTx(ctx, s.bus, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: EventWorldLifecycleCompleted,
		ActorType: &actor,
		Payload:   payload,
	})
}

func academyCount(ir *academy.IntakeResult) int {
	if ir == nil {
		return 0
	}
	return ir.Prospects
}

func streetCount(ir *academy.IntakeResult) int {
	if ir == nil {
		return 0
	}
	return ir.Street
}

// hashSeed folds the rollover coordinates into a deterministic rng seed so a
// given (world, country, season) always reproduces the same retirement rolls
// and replenishment cohort.
func hashSeed(worldID, countryID uuid.UUID, season int) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("lifecycle:"))
	for _, id := range []uuid.UUID{worldID, countryID} {
		_, _ = h.Write(id[:])
	}
	_, _ = h.Write([]byte(fmt.Sprintf(":%d", season)))
	return int64(h.Sum64())
}

func countrySeed(c *uuid.UUID) uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return *c
}
