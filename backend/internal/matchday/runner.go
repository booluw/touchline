// Package matchday is the S04-02 bridge between the world clock and the
// deterministic match engine. It runs a world's due matchdays — fixtures whose
// kickoff the world's current date has passed — plays them through
// internal/match, and pushes every result into the competition layer's
// standings via ApplyResult. The whole step is idempotent: re-running the same
// world date touches only fixtures still scheduled, so at-least-once event
// delivery cannot double-advance a season.
package matchday

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/match"
)

// Runner plays a world's due matchdays and applies their results.
type Runner struct {
	pool    *pgxpool.Pool
	matches *match.Service
	comp    *competition.Service
}

// NewRunner wires the match orchestration and competition services together
// and installs the competition service as the match engine's StandingsContext
// (six-pointer / dead-rubber input for the squad pipeline).
func NewRunner(pool *pgxpool.Pool, matches *match.Service, comp *competition.Service) *Runner {
	matches.WithStandingsContext(comp)
	return &Runner{pool: pool, matches: matches, comp: comp}
}

// Summary reports one RunDue pass.
type Summary struct {
	WorldID   uuid.UUID
	Matchdays int // distinct matchdays advanced this pass
	Played    int // fixtures newly simulated this pass
	Applied   int // standings writes performed this pass
}

// RunDue plays every matchday of a world whose kickoff date the world clock
// has already passed and applies each result to the standings. Idempotent: on
// redelivery only fixtures still marked scheduled play again.
func (r *Runner) RunDue(ctx context.Context, worldID uuid.UUID) (*Summary, error) {
	asOf, err := r.worldDate(ctx, worldID)
	if err != nil {
		return nil, err
	}
	matchdays, err := r.dueMatchdays(ctx, worldID, asOf)
	if err != nil {
		return nil, err
	}

	sum := &Summary{WorldID: worldID}
	for _, md := range matchdays {
		results, err := r.matches.PlayMatchday(ctx, worldID, md)
		if err != nil {
			return sum, fmt.Errorf("matchday %d: %w", md, err)
		}
		sum.Matchdays++
		for _, res := range results {
			if res.Match == nil {
				continue
			}
			err := r.comp.ApplyResult(ctx, res.Match.FixtureID, res.Match.HomeGoals, res.Match.AwayGoals)
			if errors.Is(err, competition.ErrResultAlreadyApplied) {
				continue // an earlier/parallel pass already applied this fixture
			}
			if err != nil {
				return sum, fmt.Errorf("apply result fixture %s: %w", res.Match.FixtureID, err)
			}
			sum.Played++
			sum.Applied++
		}
	}
	return sum, nil
}

// worldDate maps the world's monotonically increasing tick to its calendar
// date: launch day (launched_at or created_at) plus one day per tick. The
// scheduler advances the tick on the world's configured daily cadence, so the
// mapping is deterministic and wall-clock-independent for tests.
func (r *Runner) worldDate(ctx context.Context, worldID uuid.UUID) (time.Time, error) {
	var (
		ref  time.Time
		tick int64
	)
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(launched_at, created_at), current_tick
		FROM world.worlds WHERE id = $1`, worldID).Scan(&ref, &tick)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("matchday: world %s not found", worldID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("matchday: world date: %w", err)
	}
	y, m, d := ref.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(tick)), nil
}

// dueMatchdays lists the distinct matchdays with scheduled fixtures whose
// kickoff calendar day is at or before the world's current date, ascending.
func (r *Runner) dueMatchdays(ctx context.Context, worldID uuid.UUID, asOf time.Time) ([]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT matchday FROM match.fixtures
		WHERE world_id = $1
		  AND matchday IS NOT NULL
		  AND status = 'scheduled'
		  AND scheduled_at::date <= $2::date
		ORDER BY matchday`, worldID, asOf)
	if err != nil {
		return nil, fmt.Errorf("matchday: due matchdays: %w", err)
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var md int
		if err := rows.Scan(&md); err != nil {
			return nil, fmt.Errorf("matchday: scan: %w", err)
		}
		out = append(out, md)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matchday: iterate: %w", err)
	}
	return out, nil
}