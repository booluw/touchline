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
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/world"
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
	asOf, err := r.worldNow(ctx, worldID)
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
//
// Resumability is the contract: every step is durable per simulated minute, so
// returning early — on a transient error, or because another goroutine already
// holds the claim — never loses a match. A later call (the worker's kickoff
// poll, or the startup sweep) picks the world up from the last persisted
// minute. That is why the worker re-enters this loop for every playable world
// on every poll instead of only right after a kickoff: a loop that died must
// resume on its own, and a stranded live match would otherwise block the
// world's whole matchday ladder behind the no-overlap gate.
func (r *Runner) RunLive(ctx context.Context, worldID uuid.UUID) error {
	if !r.claim(ctx, worldID) {
		return nil // another goroutine already paces this world
	}
	defer r.release(worldID)

	// Resume diagnostics are logged once per entry, not once per minute: the
	// entry itself is the interesting event (a poll pass or a restart adopting
	// a match mid-flight), and the loop may run for hundreds of steps.
	resumed := false
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
		if !resumed {
			resumed = true
			for _, sess := range sessions {
				logResumed(sess)
			}
		}

		pending := 0
		minPacing := time.Duration(1<<63 - 1)
		for _, sess := range sessions {
			rows, finished, err := paceWithRetry(ctx, r.matches, sess)
			if err != nil {
				return fmt.Errorf("live %s: match %s minute %d: %w", worldID, sess.MatchID, sess.NextMinute(), err)
			}
			if err := r.publishTick(ctx, sess, sess.NextMinute()-1, rows); err != nil {
				return err
			}
			if finished {
				res, err := finalizeWithRetry(ctx, r.matches, sess)
				if err != nil {
					return fmt.Errorf("live %s: match %s full time: %w", worldID, sess.MatchID, err)
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

// liveRetryBackoff is the retry schedule for a single pacing step. One flaky
// write (a dropped connection, a restarted database) must not strand a live
// match, so the step is retried in place before the loop gives the world back
// to the poll. PaceMinute and Finalize are both safe to retry: a failed
// transaction rolls back whole minutes, and Finalize is idempotent.
var liveRetryBackoff = []time.Duration{0, 2 * time.Second, 10 * time.Second}

// paceWithRetry paces one simulated minute, retrying transient failures.
func paceWithRetry(ctx context.Context, matches *match.Service, sess *match.LiveSession) ([]*match.MatchEventRow, bool, error) {
	var (
		rows     []*match.MatchEventRow
		finished bool
		err      error
	)
	for i, wait := range liveRetryBackoff {
		if err = sleepCtx(ctx, wait); err != nil {
			return nil, false, err
		}
		if rows, finished, err = matches.PaceMinute(ctx, sess); err == nil {
			return rows, finished, nil
		}
		logLiveRetry("pace minute", sess, i+1, len(liveRetryBackoff), err)
	}
	return nil, false, err
}

// finalizeWithRetry completes a match at full time, retrying transient
// failures. Idempotent, so a retry after a partial failure is a no-op.
func finalizeWithRetry(ctx context.Context, matches *match.Service, sess *match.LiveSession) (*match.MatchFinalized, error) {
	var (
		res *match.MatchFinalized
		err error
	)
	for i, wait := range liveRetryBackoff {
		if err = sleepCtx(ctx, wait); err != nil {
			return nil, err
		}
		if res, err = matches.Finalize(ctx, sess); err == nil {
			return res, nil
		}
		logLiveRetry("finalize", sess, i+1, len(liveRetryBackoff), err)
	}
	return nil, err
}

func logLiveRetry(step string, sess *match.LiveSession, attempt, of int, err error) {
	log.Printf("live %s: match %s: %s at minute %d failed (attempt %d/%d): %v",
		sess.WorldID, sess.MatchID, step, sess.NextMinute(), attempt, of, err)
}

// logResumed reports where each live match of a resumed world stands: the minute
// already persisted and how long it has been live. A match that has been live
// far longer than pacing × minute is the signature of a pacing loop that was not
// running (stalled worker, restart) — the "match never ends" symptom.
func logResumed(sess *match.LiveSession) {
	log.Printf("live %s: resuming fixture %s match %s from minute %d (pacing %s/minute, live for %s)",
		sess.WorldID, sess.FixtureID, sess.MatchID, sess.NextMinute(), sess.Pacing(), sess.LiveFor().Round(time.Second))
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
// authoritative completion (final score, status completed). The minute is the one
// the match actually ended on — the last minute PaceMinute persisted, i.e. 90 for
// a league match but 91 for a golden-goal tie, whose deciding goal arrived in the
// extra-time flush. Hardcoding 90 would walk the clock backwards for exactly the
// matches whose last event is the one that mattered.
func (r *Runner) publishCompleted(ctx context.Context, sess *match.LiveSession, res *match.MatchFinalized) error {
	if r.rt == nil {
		return nil
	}
	ev := realtime.MustEvent(realtime.EventMatchTick, sess.WorldID, match.MatchTickPayload{
		Match:     apiref.MatchRef{ID: sess.MatchID},
		Fixture:   apiref.FixtureRef{ID: sess.FixtureID},
		Minute:    sess.NextMinute() - 1,
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

// PlayableWorlds lists every playable world id, for the worker's intra-day
// kickoff poll (IM16: kickoffs happen at scheduled_at moments, between daily
// ticks, so the worker scans playable worlds on its own cadence).
func (r *Runner) PlayableWorlds(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM world.worlds WHERE status IN ('active', 'open_beta') ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("matchday: playable worlds: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("matchday: playable worlds: scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matchday: playable worlds: iterate: %w", err)
	}
	return out, nil
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

// worldNow maps the real clock to the world's continuous current moment at the
// world's fixed scale (IM16): with the default tick.day_length of one game-day
// per real day it is real UTC time, so a fixture's scheduled_at ("Sep 27
// 20:00") matures at that exact moment — the kickoff time is real, not just its
// calendar date.
func (r *Runner) worldNow(ctx context.Context, worldID uuid.UUID) (time.Time, error) {
	_, epoch, dayLength, err := world.LoadScale(ctx, r.pool, worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("matchday: world %s not found", worldID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("matchday: world clock: %w", err)
	}
	return world.ScaleNow(time.Now(), epoch, dayLength), nil
}

// dueMatchdays lists the distinct matchdays with scheduled fixtures whose
// scheduled kickoff moment (scheduled_at) the world clock has already reached,
// ascending. IM16: this is a timestamp comparison against the continuous world
// clock, so a matchday with a 20:00 kickoff only becomes due at 20:00 — its
// fixtures never simulate on the date alone.
func (r *Runner) dueMatchdays(ctx context.Context, worldID uuid.UUID, asOf time.Time) ([]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT matchday FROM match.fixtures
		WHERE world_id = $1
		  AND matchday IS NOT NULL
		  AND status = 'scheduled'
		  AND scheduled_at <= $2
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
