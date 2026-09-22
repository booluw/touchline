// Package matchday is the S04-02 bridge between the world clock and the live
// match engine (OPD-21). A daily world tick kicks off every due matchday —
// fixtures become 'live' in match.matches with a frozen simulation snapshot —
// and a per-world goroutine then paces those matches in real time (one
// simulated minute per tick.match_cadence), finalizes each at full time, and
// pushes the result into the competition layer via ApplyResult. Live matches
// never run on the world clock (OPD-17(2)); the pacing goroutine owns them.
// Both steps are idempotent, so at-least-once event delivery cannot double
// advance a match or season.
package matchday

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/realtime"
)

// Runner owns one world's kickoff + live pacing lifecycle.
type Runner struct {
	pool    *pgxpool.Pool
	matches *match.Service
	comp    *competition.Service
	rt      realtime.Broker // optional S04-03 live feed fan-out; nil disables pushes

	mu     sync.Mutex
	active map[uuid.UUID]bool // worlds with a live pacing goroutine already running
}

// NewRunner wires the match orchestration and competition services together
// and installs the competition service as the match engine's StandingsContext
// (six-pointer / dead-rubber input for the squad pipeline).
func NewRunner(pool *pgxpool.Pool, matches *match.Service, comp *competition.Service) *Runner {
	matches.WithStandingsContext(comp)
	return &Runner{pool: pool, matches: matches, comp: comp, active: make(map[uuid.UUID]bool)}
}

// WithRealtime installs the realtime broker the pacing loop pushes match_tick
// envelopes through (S04-03). It mirrors the optional-fan-out pattern of
// WithStandingsContext: nil (the default) publishes nothing.
func (r *Runner) WithRealtime(rt realtime.Broker) *Runner {
	r.rt = rt
	return r
}

// Summary reports one KickoffDue pass.
type Summary struct {
	WorldID   uuid.UUID
	Matchdays int // distinct matchdays kicked off this pass
	Kicked    int // fixtures newly kicked off this pass
	Skipped   int // matchdays deferred because a match in the world is already live
}

// KickoffDue kicks off every matchday of a world whose kickoff date the world
// clock has already passed: each scheduled fixture is frozen into a live match
// (status 'live', snapshot persisted) awaiting the pacing goroutine. Idempotent:
// on redelivery only fixtures still marked scheduled kick off again, and the
// no-overlap gate defers a matchday while an earlier one is still live.
func (r *Runner) KickoffDue(ctx context.Context, worldID uuid.UUID) (*Summary, error) {
	asOf, err := r.worldDate(ctx, worldID)
	if err != nil {
		return nil, err
	}
	matchdays, err := r.dueMatchdays(ctx, worldID, asOf)
	if err != nil {
		return nil, err
	}

	sum := &Summary{WorldID: worldID}
	if len(matchdays) == 0 {
		return sum, nil
	}

	// No-overlap gate (OPD-21): a world with a match still live must not kick
	// off the next matchday; the pacing goroutine re-runs KickoffDue after
	// completion. Without this, live matches would pile up across matchdays.
	live, err := r.worldHasLive(ctx, worldID)
	if err != nil {
		return sum, err
	}
	if live {
		sum.Skipped = len(matchdays)
		return sum, nil
	}

	for _, md := range matchdays {
		sessions, err := r.matches.KickoffMatchday(ctx, worldID, md)
		if err != nil {
			return sum, fmt.Errorf("kickoff matchday: %w", err)
		}
		sum.Matchdays++
		sum.Kicked += len(sessions)
	}
	return sum, nil
}

// RunLive paces every in-progress live match of a world to full time in real
// time, finalizes each, and applies the result to the standings. It also
// reconciles the crash window (fixture live + match completed + standings not
// yet applied). It runs until no in-progress matches remain, sleeping the
// smallest pacing across pending matches between simulated minutes. Only one
// goroutine per world runs at a time (claim guard).
func (r *Runner) RunLive(ctx context.Context, worldID uuid.UUID) error {
	if !r.claim(ctx, worldID) {
		return nil // another goroutine already paces this world
	}
	defer r.release(worldID)

	for {
		if err := r.reconcileApplied(ctx, worldID); err != nil {
			return err
		}
		sessions, err := r.matches.LoadLiveSessions(ctx, worldID)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			return nil
		}

		pending := 0
		minPacing := time.Duration(1<<63 - 1)
		for _, sess := range sessions {
			rows, finished, err := r.matches.PaceMinute(ctx, sess)
			if err != nil {
				return err
			}
			if err := r.publishTick(ctx, sess, sess.NextMinute()-1, rows); err != nil {
				return err
			}
			if finished {
				res, err := r.matches.Finalize(ctx, sess)
				if err != nil {
					return err
				}
				if err := r.publishCompleted(ctx, sess, res); err != nil {
					return err
				}
				continue
			}
			pending++
			if p := sess.Pacing(); p < minPacing {
				minPacing = p
			}
		}
		if pending == 0 {
			continue // finalize already ran; next loop reconciles + exits
		}
		if err := sleepCtx(ctx, minPacing); err != nil {
			return err
		}
	}
}

// publishTick pushes one match_tick envelope for a paced minute (S04-03).
// rows are exactly the match_events persisted this minute; the scoreline is
// computed by the match service so the client never derives it. Silent minutes
// still emit a tick so the live clock advances. Nil broker = no-op.
func (r *Runner) publishTick(ctx context.Context, sess *match.LiveSession, minute int, rows []*match.MatchEventRow) error {
	if r.rt == nil {
		return nil
	}
	home, away, err := r.matches.ScoreLine(ctx, sess.MatchID)
	if err != nil {
		return fmt.Errorf("match tick: scoreline: %w", err)
	}
	ev := realtime.MustEvent(realtime.EventMatchTick, sess.WorldID, match.MatchTickPayload{
		Match:     apiref.MatchRef{ID: sess.MatchID},
		Fixture:   apiref.FixtureRef{ID: sess.FixtureID},
		Minute:    minute,
		Status:    match.MatchStatusInProgress,
		HomeClub:  apiref.ClubRef{ID: sess.HomeClubID, Name: sess.HomeClubName},
		AwayClub:  apiref.ClubRef{ID: sess.AwayClubID, Name: sess.AwayClubName},
		HomeScore: home,
		AwayScore: away,
		Events:    rows,
	})
	if err := r.rt.Publish(ctx, ev); err != nil {
		return fmt.Errorf("match tick: publish: %w", err)
	}
	return nil
}

// publishCompleted emits the final match_tick after Finalize so clients see the
// authoritative completion (final score, status completed).
func (r *Runner) publishCompleted(ctx context.Context, sess *match.LiveSession, res *match.MatchFinalized) error {
	if r.rt == nil {
		return nil
	}
	ev := realtime.MustEvent(realtime.EventMatchTick, sess.WorldID, match.MatchTickPayload{
		Match:     apiref.MatchRef{ID: sess.MatchID},
		Fixture:   apiref.FixtureRef{ID: sess.FixtureID},
		Minute:    90,
		Status:    match.MatchStatusCompleted,
		HomeClub:  apiref.ClubRef{ID: sess.HomeClubID, Name: sess.HomeClubName},
		AwayClub:  apiref.ClubRef{ID: sess.AwayClubID, Name: sess.AwayClubName},
		HomeScore: res.HomeGoals,
		AwayScore: res.AwayGoals,
	})
	if err := r.rt.Publish(ctx, ev); err != nil {
		return fmt.Errorf("match tick: publish completion: %w", err)
	}
	return nil
}

// reconcileApplied closes the crash window between Finalize (match + fixture
// completed, MATCH_PLAYED recorded) and the standings write: a completed
// fixture whose standings were never applied gets its ApplyResult now.
// Idempotent by design.
func (r *Runner) reconcileApplied(ctx context.Context, worldID uuid.UUID) error {
	rows, err := r.pool.Query(ctx, `
		SELECT f.id, m.home_score, m.away_score
		FROM match.fixtures f
		JOIN match.matches m ON m.fixture_id = f.id
		WHERE f.world_id = $1
		  AND f.status = 'completed'
		  AND f.standings_applied_at IS NULL
		  AND m.status = 'completed'`, worldID)
	if err != nil {
		return fmt.Errorf("reconcile applied: %w", err)
	}
	defer rows.Close()
	var out []struct {
		id         uuid.UUID
		home, away int
	}
	for rows.Next() {
		var f struct {
			id         uuid.UUID
			home, away int
		}
		if err := rows.Scan(&f.id, &f.home, &f.away); err != nil {
			return fmt.Errorf("reconcile applied: scan: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reconcile applied: iterate: %w", err)
	}
	for _, f := range out {
		if err := r.applyResultOnce(ctx, f.id, f.home, f.away); err != nil {
			return err
		}
	}
	return nil
}

// applyResultOnce applies a finalized result to the standings, treating a
// parallel already-applied write as success.
func (r *Runner) applyResultOnce(ctx context.Context, fixtureID uuid.UUID, home, away int) error {
	err := r.comp.ApplyResult(ctx, fixtureID, home, away)
	if errors.Is(err, competition.ErrResultAlreadyApplied) {
		return nil // an earlier/parallel pass already applied this fixture
	}
	if err != nil {
		return fmt.Errorf("apply result fixture %s: %w", fixtureID, err)
	}
	return nil
}

// claim marks a world's pacing loop as owned; false when another goroutine
// already runs it.
func (r *Runner) claim(ctx context.Context, worldID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[worldID] {
		return false
	}
	r.active[worldID] = true
	return true
}

// release returns the pacing claim.
func (r *Runner) release(worldID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, worldID)
}

// worldHasLive reports whether any fixture of the world is currently live.
func (r *Runner) worldHasLive(ctx context.Context, worldID uuid.UUID) (bool, error) {
	var n int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'live'`, worldID).Scan(&n); err != nil {
		return false, fmt.Errorf("matchday: live check: %w", err)
	}
	return n > 0, nil
}

// WorldsWithLiveMatches lists every world with at least one live fixture, for
// the worker's startup rehydration sweep.
func (r *Runner) WorldsWithLiveMatches(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT world_id FROM match.fixtures WHERE status = 'live' ORDER BY world_id`)
	if err != nil {
		return nil, fmt.Errorf("matchday: live worlds: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("matchday: live worlds: scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matchday: live worlds: iterate: %w", err)
	}
	return out, nil
}

// worldDate maps the world's calendar day counter to its date: launch day
// (launched_at or created_at) plus current_day days. only WORLD_TICK{daily}
// emissions advance current_day (the scheduler does both in one tx, OPD-24), so
// hourly/weekly/monthly/seasonal ticks never move the fixture calendar and the
// mapping is deterministic and wall-clock-independent for tests. current_tick
// stays the monotonic ordering counter and is deliberately not used here.
func (r *Runner) worldDate(ctx context.Context, worldID uuid.UUID) (time.Time, error) {
	var (
		ref time.Time
		day int64
	)
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(launched_at, created_at), current_day
		FROM world.worlds WHERE id = $1`, worldID).Scan(&ref, &day)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("matchday: world %s not found", worldID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("matchday: world date: %w", err)
	}
	y, m, d := ref.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(day)), nil
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

// sleepCtx sleeps for d, aborting early when the context is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
