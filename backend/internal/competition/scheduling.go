package competition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// IM05 scheduling administration ("reschedule the country's calendar").
//
// Three surfaces share one persistence shape — a JSONB `allowed_weekdays`
// array (ISO weekdays 1..7) under scheduling_rules / default_scheduling_rules
// — and one enforcement rule: a calendar re-pace moves only matchdays with no
// live/completed fixture (started matchdays are frozen). Re-pacing never
// reorders fixtures; it keeps matchday order and the deterministic kickoff
// rotation, sliding unfinished matchdays forward so every gap is at least two
// game-days and, when weekdays are configured, every matchday lands on an
// allowed weekday.

// RescheduleResult reports what an IM05 scheduling update materialized.
type RescheduleResult struct {
	// CalendarUpdated is true when fixtures actually moved (or a country/league
	// weekday set was persisted); for cups it stays true only on persist, since
	// existing cup dates stand until the next campaign.
	CalendarUpdated  bool `json:"calendar_updated"`
	MatchdaysRePaced int  `json:"matchdays_re_paced,omitempty"`
	FixturesMoved    int  `json:"fixtures_moved,omitempty"`
}

// ErrCompetitionTypeMismatch guards the admin scheduling surface: a league
// route only takes leagues, a cup route only domestic cups.
var ErrCompetitionTypeMismatch = errors.New("competition does not match the requested type")

// updateRulesWeekdays persists a competition's allowed_weekdays override
// (setting it to JSON null clears the override and restores the fallback
// chain: per-league → country default → legacy pacing).
func updateRulesWeekdays(ctx context.Context, tx pgx.Tx, competitionID uuid.UUID, weekdaysJSON []byte) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO competition.competition_rules (competition_id, format, scheduling_rules)
		VALUES ($1, 'round_robin', $2::jsonb)
		ON CONFLICT (competition_id) DO UPDATE SET scheduling_rules =
			jsonb_set(COALESCE(competition_rules.scheduling_rules, '{}'::jsonb),
				'{allowed_weekdays}', $2::jsonb)`,
		competitionID, weekdaysJSON); err != nil {
		return fmt.Errorf("update scheduling rules: %w", err)
	}
	return nil
}

// UpdateCountryScheduling persists the country's weekday-default and re-paces
// every league in it that has no override of its own (and that yields a
// weekday set), so a country-wide rule change propagates to its whole calendar
// in one shot. Clearing the default (empty weekdays) only persists the rules —
// existing fixtures are never pulled backward or re-stamped on a clear.
func (s *Service) UpdateCountryScheduling(ctx context.Context, worldID, countryID uuid.UUID, weekdays []int) (*RescheduleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin country scheduling update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM world.countries WHERE id = $1 AND world_id = $2)`,
		countryID, worldID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}
	if !exists {
		return nil, ErrCountryNotFound
	}

	wj := weekdaysJSON(weekdays)
	if _, err := tx.Exec(ctx, `
		UPDATE world.countries SET default_scheduling_rules = $3
		WHERE world_id = $1 AND id = $2`,
		worldID, countryID, wj); err != nil {
		return nil, fmt.Errorf("persist country scheduling: %w", err)
	}

	res := &RescheduleResult{CalendarUpdated: true}
	if len(weekdays) > 0 {
		leagueIDs, err := s.fallbackLeagues(ctx, tx, worldID, countryID)
		if err != nil {
			return nil, err
		}
		for _, leagueID := range leagueIDs {
			n, f, err := s.repaceLeague(ctx, tx, worldID, leagueID)
			if err != nil {
				return nil, err
			}
			res.MatchdaysRePaced += n
			res.FixturesMoved += f
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit country scheduling update: %w", err)
	}
	return res, nil
}

// fallbackLeagues lists the country's leagues whose effective weekday set comes
// from the country default (no own non-empty allowed_weekdays override) and
// which already have fixtures — exactly the competitions a country-default
// change re-paces.
func (s *Service) fallbackLeagues(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id
		FROM competition.competitions l
		LEFT JOIN competition.competition_rules r ON r.competition_id = l.id
		WHERE l.world_id = $1 AND l.country_id = $2 AND l.competition_type = 'league'
		  AND NOT (
			COALESCE(r.scheduling_rules, '{}'::jsonb) ? 'allowed_weekdays'
			AND jsonb_typeof(COALESCE(r.scheduling_rules, '{}'::jsonb) -> 'allowed_weekdays') = 'array'
			AND jsonb_array_length(COALESCE(r.scheduling_rules, '{}'::jsonb) -> 'allowed_weekdays') > 0
		  )
		  AND EXISTS (
			SELECT 1 FROM match.fixtures f WHERE f.competition_id = l.id AND f.world_id = $1
		  )`, worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("fallback leagues: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan fallback league: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// UpdateLeagueScheduling persists a league's allowed_weekdays override and
// re-paces it when the resulting weekday set is non-empty. Clearing the
// override (empty weekdays) restores the fallback chain but does not re-pace:
// a mid-season clear leaves already-stamped fixtures where they are.
func (s *Service) UpdateLeagueScheduling(ctx context.Context, leagueID uuid.UUID, weekdays []int) (*RescheduleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin league scheduling update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	worldID, err := s.requireCompetition(ctx, tx, leagueID, "league")
	if err != nil {
		return nil, err
	}
	if err := updateRulesWeekdays(ctx, tx, leagueID, weekdaysJSON(weekdays)); err != nil {
		return nil, err
	}

	res := &RescheduleResult{CalendarUpdated: true}
	if len(weekdays) > 0 {
		res.MatchdaysRePaced, res.FixturesMoved, err = s.repaceLeague(ctx, tx, worldID, leagueID)
		if err != nil {
			return nil, err
		}
		if res.FixturesMoved > 0 {
			name, nameErr := s.competitionName(ctx, tx, leagueID)
			if nameErr != nil {
				return nil, nameErr
			}
			if newsErr := s.publishSchedulingNews(ctx, tx, worldID, leagueID,
				fmt.Sprintf("%s fixture calendar rescheduled", name),
				fmt.Sprintf("%d matchdays (%d fixtures) were moved onto the new scheduling weekdays.", res.MatchdaysRePaced, res.FixturesMoved)); newsErr != nil {
				return nil, newsErr
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit league scheduling update: %w", err)
	}
	return res, nil
}

// UpdateCupScheduling persists a cup's allowed_weekdays override. The cup's
// existing fixture dates stand until the next round is materialized (cup
// calendars are anchored to the league season, see planCupCalendar / the
// materializeRound slot).
func (s *Service) UpdateCupScheduling(ctx context.Context, cupID uuid.UUID, weekdays []int) (*RescheduleResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cup scheduling update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := s.requireCup(ctx, tx, cupID); err != nil {
		return nil, err
	}
	if err := updateRulesWeekdays(ctx, tx, cupID, weekdaysJSON(weekdays)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cup scheduling update: %w", err)
	}
	return &RescheduleResult{CalendarUpdated: true}, nil
}

// requireCup asserts the competition exists and is a knockout cup (country or
// regional scope), returning its world id for scoping downstream reads.
func (s *Service) requireCup(ctx context.Context, tx pgx.Tx, cupID uuid.UUID) (uuid.UUID, error) {
	var (
		ctype   string
		worldID uuid.UUID
	)
	err := tx.QueryRow(ctx, `
		SELECT competition_type, world_id FROM competition.competitions
		WHERE id = $1`, cupID).Scan(&ctype, &worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrCompetitionNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load competition: %w", err)
	}
	if ctype != "domestic_cup" && ctype != "continental" {
		return uuid.Nil, ErrCompetitionTypeMismatch
	}
	return worldID, nil
}

// requireCompetition asserts the competition exists (errors otherwise) and
// matches the expected type, returning its world id for scoping downstream
// reads.
func (s *Service) requireCompetition(ctx context.Context, tx pgx.Tx, competitionID uuid.UUID, competitionType string) (uuid.UUID, error) {
	var (
		ctype   string
		worldID uuid.UUID
	)
	err := tx.QueryRow(ctx, `
		SELECT competition_type, world_id FROM competition.competitions
		WHERE id = $1`, competitionID).Scan(&ctype, &worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrCompetitionNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load competition: %w", err)
	}
	if ctype != competitionType {
		return uuid.Nil, ErrCompetitionTypeMismatch
	}
	return worldID, nil
}

// repaceLeague slides a league's unfinished matchdays forward onto a valid
// weekday calendar (IM05), leaving started matchdays frozen. The first
// unstarted matchday lands on the earliest allowed weekday at least two
// game-days after the frozen horizon (or after the season anchor with none
// frozen); each later one steps the same way off its predecessor. Matchday
// order and the deterministic kickoff rotation are preserved. Returns the
// number of matchdays whose date moved and how many fixture rows were
// re-stamped. A league with no weekday set (unconfigured) is untouched.
func (s *Service) repaceLeague(ctx context.Context, tx pgx.Tx, worldID, leagueID uuid.UUID) (int, int, error) {
	p, err := s.scheduleParams(ctx, tx, leagueID, worldID)
	if err != nil {
		return 0, 0, err
	}
	if len(p.allowedWeekdays) == 0 {
		return 0, 0, nil // no weekday calendar to enforce
	}

	type mdInfo struct {
		dates  []time.Time
		frozen bool
		order  int
	}
	rows, err := tx.Query(ctx, `
		SELECT matchday, scheduled_at, status
		FROM match.fixtures
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'cancelled'
		ORDER BY matchday, scheduled_at`, leagueID, worldID)
	if err != nil {
		return 0, 0, fmt.Errorf("repacing: load fixtures: %w", err)
	}
	defer rows.Close()

	maxMD := 0
	group := map[int]*mdInfo{}
	horizon := 0
	horizonDate := time.Time{}
	for rows.Next() {
		var (
			md      int
			kickoff time.Time
			status  string
		)
		if err := rows.Scan(&md, &kickoff, &status); err != nil {
			return 0, 0, fmt.Errorf("repacing: scan fixture: %w", err)
		}
		info, ok := group[md]
		if !ok {
			info = &mdInfo{order: md}
			group[md] = info
		}
		info.dates = append(info.dates, kickoff)
		if status == "live" || status == "completed" {
			info.frozen = true
		}
		if md > maxMD {
			maxMD = md
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for md, info := range group {
		if info.frozen && md > horizon {
			horizon = md
			for _, d := range info.dates {
				if d.After(horizonDate) {
					horizonDate = d
				}
			}
		}
	}
	if horizon == maxMD {
		return 0, 0, nil // everything already played or in progress
	}

	var seed int64
	var worldRef time.Time
	worldErr := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0), COALESCE(launched_at, created_at)
		FROM world.worlds WHERE id = $1`, worldID).
		Scan(&seed, &worldRef)
	if worldErr != nil {
		return 0, 0, fmt.Errorf("repacing: load world: %w", worldErr)
	}

	last := daysTruncate(worldRef)
	if horizon > 0 {
		last = daysTruncate(horizonDate)
	}
	matchdaysMoved, fixturesMoved := 0, 0
	first := true
	for md := horizon + 1; md <= maxMD; md++ {
		info, ok := group[md]
		if !ok {
			continue
		}
		var start time.Time
		if first {
			first = false
			if horizon == 0 {
				// Nothing played yet: matchday 1 mirrors fresh materialization
				// (earliest allowed weekday on/after the season anchor + 1).
				start = nextAllowedWeekday(last.AddDate(0, 0, 1), p.allowedWeekdays)
			} else {
				start = nextAllowedWeekday(last.AddDate(0, 0, 2), p.allowedWeekdays)
			}
		} else {
			start = nextAllowedWeekday(last.AddDate(0, 0, 2), p.allowedWeekdays)
		}
		last = start
		day := kickOff(start, kickoffHour(seed, leagueID, p.kickoffHours, md))
		changed := false
		for _, existing := range info.dates {
			if !existing.Equal(day) {
				changed = true
				break
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE match.fixtures SET scheduled_at = $3
			WHERE competition_id = $1 AND world_id = $2 AND matchday = $4 AND status <> 'cancelled'`,
			leagueID, worldID, day, md); err != nil {
			return 0, 0, fmt.Errorf("repacing: restamp matchday %d: %w", md, err)
		}
		if changed {
			matchdaysMoved++
			fixturesMoved += len(info.dates)
		}
	}
	return matchdaysMoved, fixturesMoved, nil
}

// publishSchedulingNews records a calendar event and a country-scoped news
// story (category 'scheduling') inside the caller's transaction. A cup's
// scheduling news goes to the cup's country feed (country_id); world-wide
// stories keep NULL. All-or-nothing with the fixtures, so feeds never report a
// schedule the engine didn't keep.
func (s *Service) publishSchedulingNews(ctx context.Context, tx pgx.Tx, worldID, competitionID uuid.UUID, headline, body string) error {
	var countryID *uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT country_id FROM competition.competitions WHERE id = $1`,
		competitionID).Scan(&countryID); err != nil {
		return fmt.Errorf("load cup for scheduling news: %w", err)
	}
	ev := &eventbus.Event{
		WorldID:   worldID,
		EventType: "COMPETITION_SCHEDULE_UPDATED",
		Payload: mustJSON(map[string]any{
			"competition_id": competitionID,
			"headline":       headline,
		}),
	}
	if err := s.recordSeedEvent(ctx, tx, ev); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO world.news_stories (world_id, headline, body, category, related_event_id, country_id)
		VALUES ($1, $2, $3, 'scheduling', $4, $5)`,
		worldID, headline, body, ev.ID, countryID); err != nil {
		return fmt.Errorf("publish scheduling news: %w", err)
	}
	return nil
}

// publishAnnouncementNews records a country-scoped press-release story
// (category 'announcement') inside the caller's transaction, linked to an
// already-recorded engine event (related_event_id). Unlike publishSchedulingNews
// it does not raise its own event: season-start releases share the SEASON_CREATED
// event so readers can group the fixture-list and kickoff bulletins together.
// All-or-nothing with the season they describe.
func (s *Service) publishAnnouncementNews(ctx context.Context, tx pgx.Tx, worldID, competitionID uuid.UUID, relatedEventID uuid.UUID, headline, body string) error {
	var countryID *uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT country_id FROM competition.competitions WHERE id = $1`,
		competitionID).Scan(&countryID); err != nil {
		return fmt.Errorf("load competition for announcement: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO world.news_stories (world_id, headline, body, category, related_event_id, country_id)
		VALUES ($1, $2, $3, 'announcement', $4, $5)`,
		worldID, headline, body, relatedEventID, countryID); err != nil {
		return fmt.Errorf("publish announcement news: %w", err)
	}
	return nil
}

// competitionName is a tiny read helper for news headlines.
func (s *Service) competitionName(ctx context.Context, tx pgx.Tx, competitionID uuid.UUID) (string, error) {
	var name string
	if err := tx.QueryRow(ctx, `
		SELECT name FROM competition.competitions WHERE id = $1`,
		competitionID).Scan(&name); err != nil {
		return "", fmt.Errorf("load competition name: %w", err)
	}
	return name, nil
}
