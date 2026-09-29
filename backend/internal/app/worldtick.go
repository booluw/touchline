package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/academy"
	"github.com/touchline/backend/pkg/eventbus"
)

// handleWorldTick is the single daily WORLD_TICK dispatch (IM02):
// only the 'daily' granularity does gameplay work; every other periodic system
// is derived from the world's day counter (world.worlds.current_day, read with
// the calendar.days_per_week/days_per_month steps). Order is stable so a day
// that is both a week and a month boundary runs weekly before monthly, and the
// board review lands after wages/market exactly once per month.
//
// Legacy note: WORLD_TICK events of other granularities (hourly/weekly/monthly/
// seasonal) that were still in flight at deploy are ignored — the passes they
// used to drive now run off the new day gates, so replaying them would double-
// post wages/reviews.
func (a *App) handleWorldTick(ctx context.Context, ev eventbus.Event, granularity string) error {
	if granularity != "daily" {
		log.Printf("world %s tick %d: ignoring legacy %s granularity (single-daily clock)", ev.WorldID, ev.WorldTick, granularity)
		return nil
	}

	currentDay, week, month, err := a.World.Calendar(ctx, ev.WorldID)
	if err != nil {
		return fmt.Errorf("world %s daily tick: calendar config: %w", ev.WorldID, err)
	}
	// IM23: the emission's own day, not the counter at processing time — a
	// multi-day rollover commits all its increments at once. Emissions from
	// before IM23 carry no day and fall back to the counter.
	day := currentDay
	if d, ok := tickDay(ev.Payload); ok {
		day = d
	}

	if err := a.runDaily(ctx, ev); err != nil {
		return err
	}
	// Weekly work (training, player pass, rivalry reconciliation) once per
	// days_per_week days: days 7/14/21/28 with the default 7-day week.
	if week > 0 && day%int64(week) == 0 {
		if err := a.runWeekly(ctx, ev, day); err != nil {
			return err
		}
	}
	// Monthly work (wages, academy maintenance, the board review — re-purposed
	// from weekly to monthly, IM02) once per days_per_month days: day 30 with
	// the default 30-day month. Board runs last so it grades post-wage books.
	if month > 0 && day%int64(month) == 0 {
		if err := a.runMonthly(ctx, ev); err != nil {
			return err
		}
	}
	// Seasonal fallback (S08-01): a world without leagues never emits
	// SEASON_COMPLETED, so reaching a season boundary drives the full lifecycle
	// once per academy.DaysPerSeason days — intake, retirement, pool replenish
	// (A06). The hooks dedup per season. Leagues worlds are driven by their own
	// SEASON_COMPLETED subscription instead.
	if day%int64(academy.DaysPerSeason) == 0 {
		if err := a.runSeasonal(ctx, ev, day); err != nil {
			return err
		}
	}

	// Home dashboard realtime sweep (S07-01): after the cadence passes have
	// run, re-snapshot every managed club and push newly surfaced items to the
	// affected managers' socket feeds. Best-effort; the GET read stays
	// authoritative.
	if err := a.Dashboard.PushWorldDelta(ctx, ev.WorldID); err != nil {
		return fmt.Errorf("world %s dashboard sweep: %w", ev.WorldID, err)
	}
	return nil
}

// runDaily is the every-day pass: season activation + kickoffs (runner leader
// only), absent-manager bid responses, and the transfer market tick.
func (a *App) runDaily(ctx context.Context, ev eventbus.Event) error {
	if a.runnerEnabled {
		// IM01: a rollover-created 'upcoming' season flips to 'in_progress'
		// (and emits SEASON_STARTED) the moment its first fixture is due,
		// before the kickoff pass so the season reads in_progress as its
		// first matchday simulates.
		if err := a.kickDueWorld(ctx, ev.WorldID); err != nil {
			return fmt.Errorf("world %s daily tick: %w", ev.WorldID, err)
		}
	}
	if err := a.Policy.RespondToBidsForAbsent(ctx, ev.WorldID); err != nil {
		return fmt.Errorf("world %s daily policy bids: %w", ev.WorldID, err)
	}
	if err := a.Transfers.DailyTick(ctx, ev.WorldID, ev.WorldTick); err != nil {
		return fmt.Errorf("world %s daily transfer market: %w", ev.WorldID, err)
	}
	return nil
}

// runWeekly is the week-boundary pass: policy training, training deltas, the
// weekly player pass, and rivalry reconciliation.
func (a *App) runWeekly(ctx context.Context, ev eventbus.Event, day int64) error {
	if err := a.Policy.EnsureTraining(ctx, ev.WorldID); err != nil {
		return fmt.Errorf("world %s weekly policy training (day %d): %w", ev.WorldID, day, err)
	}
	if _, err := a.Training.ApplyWeekly(ctx, ev.WorldID, ev.WorldTick); err != nil {
		return fmt.Errorf("world %s weekly training: %w", ev.WorldID, err)
	}
	if err := a.Players.WeeklyTick(ctx, ev.WorldID, ev.WorldTick); err != nil {
		return fmt.Errorf("world %s weekly player pass: %w", ev.WorldID, err)
	}
	reconciled, err := a.Social.ReconcileRivalries(ctx, ev.WorldID)
	if err != nil {
		return fmt.Errorf("world %s rivalries reconcile: %w", ev.WorldID, err)
	}
	if reconciled > 0 {
		log.Printf("world %s rivalries reconciled: %d fixtures backfilled", ev.WorldID, reconciled)
	}
	return nil
}

// runMonthly is the month-boundary pass: wages, academy maintenance, then the
// board review.
func (a *App) runMonthly(ctx context.Context, ev eventbus.Event) error {
	if _, err := a.Finance.ApplyMonthlyWages(ctx, ev.WorldID, ev.WorldTick); err != nil {
		return fmt.Errorf("world %s monthly wages: %w", ev.WorldID, err)
	}
	if _, err := a.Academy.Maintenance(ctx, ev.WorldID, ev.WorldTick); err != nil {
		return fmt.Errorf("world %s academy maintenance: %w", ev.WorldID, err)
	}
	reviewed, sacked, err := a.Board.Review(ctx, ev.WorldID, ev.WorldTick)
	if err != nil {
		return fmt.Errorf("world %s monthly board review: %w", ev.WorldID, err)
	}
	log.Printf("world %s monthly board review: %d reviewed, %d sacked", ev.WorldID, reviewed, sacked)
	return nil
}

// runSeasonal is the league-less season-boundary lifecycle fallback. A world
// with any league is skipped (IM24): its countries' SEASON_COMPLETED events
// drive the lifecycle, and a second world-wide pass here would run another
// intake on top of theirs.
func (a *App) runSeasonal(ctx context.Context, ev eventbus.Event, day int64) error {
	var hasLeagues bool
	if err := a.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM competition.competitions
		               WHERE world_id = $1 AND competition_type = 'league')`, ev.WorldID).Scan(&hasLeagues); err != nil {
		return fmt.Errorf("world %s seasonal lifecycle: league check: %w", ev.WorldID, err)
	}
	if hasLeagues {
		return nil
	}
	season, ref, err := a.worldSeasonAt(ctx, ev.WorldID, day)
	if err != nil {
		return fmt.Errorf("world %s seasonal lifecycle: %w", ev.WorldID, err)
	}
	if _, err := a.Lifecycle.OnSeasonCompleted(ctx, ev.WorldID, nil, season, ref); err != nil {
		return fmt.Errorf("world %s seasonal lifecycle: %w", ev.WorldID, err)
	}
	return nil
}

// kickDueWorld activates any due 'upcoming' seasons then kicks every matchday
// the world clock has matured, spawning the live pacing loop for whatever
// kicked. Idempotent: KickoffDue's status guard + no-overlap gate make a
// concurrent pass (the daily tick handler and this poll both call it) safe.
// Shared by the daily-tick handler and the IM16 intra-day kickoff poll.
//
// The pacing loop is started on every pass, not only when something new kicked
// off. RunLive returns immediately when the world has nothing live and the
// claim guard keeps a second copy from double-pacing, so this is a cheap no-op
// in the common case — but it is what makes a live match self-heal: a loop that
// died on a transient error (or a worker restart) resumes within one poll
// interval instead of stranding the match at its last persisted minute, which
// the no-overlap gate would otherwise keep at the head of the world's whole
// matchday ladder forever.
func (a *App) kickDueWorld(ctx context.Context, worldID uuid.UUID) error {
	if activated, err := a.CompSvc.ActivateDueSeasons(ctx, worldID); err != nil {
		return fmt.Errorf("activate seasons: %w", err)
	} else if activated > 0 {
		log.Printf("world %s: activated %d season(s)", worldID, activated)
	}
	sum, err := a.Runner.KickoffDue(ctx, worldID)
	if err != nil {
		return fmt.Errorf("kickoff: %w", err)
	}
	if sum != nil && sum.Kicked > 0 {
		log.Printf("world %s: kicked %d matchday(s), %d fixture(s)", worldID, sum.Matchdays, sum.Kicked)
	}
	go func() {
		if err := a.Runner.RunLive(ctx, worldID); err != nil {
			log.Printf("world %s live runner: %v", worldID, err)
		}
	}()
	return nil
}

// worldSeason derives the canonical season number and world reference date
// from the day counter (worldDate = COALESCE(launched_at, created_at) +
// current_day days; OPD-24). Used by the seasonal academy-intake hooks.
func (a *App) worldSeason(ctx context.Context, worldID uuid.UUID) (int, time.Time, error) {
	return a.worldSeasonAt(ctx, worldID, -1)
}

// worldSeasonAt is worldSeason for an explicit calendar day; a negative day
// reads the world's current counter.
func (a *App) worldSeasonAt(ctx context.Context, worldID uuid.UUID, day int64) (int, time.Time, error) {
	var ref time.Time
	err := a.Pool.QueryRow(ctx, `
		SELECT d.day, COALESCE(w.launched_at, w.created_at) + make_interval(days => d.day::int)
		FROM world.worlds w
		CROSS JOIN LATERAL (SELECT CASE WHEN $2::bigint >= 0 THEN $2::bigint ELSE w.current_day END AS day) d
		WHERE w.id = $1`, worldID, day).Scan(&day, &ref)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("world season: %w", err)
	}
	return academy.SeasonForDay(day), ref, nil
}

// tickDay extracts the calendar day stamped on a WORLD_TICK payload (IM23).
func tickDay(payload []byte) (int64, bool) {
	var p struct {
		Day *int64 `json:"day"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.Day == nil {
		return 0, false
	}
	return *p.Day, true
}
