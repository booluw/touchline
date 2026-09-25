//go:build integration

// End-to-end coverage for StartSeasonKickoff: pinning matchday 1 to a calendar
// date, the default (next-day) behavior, the 422-class validation (past date,
// off-weekday under an IM05 weekday set), and the two country-scoped
// 'announcement' press releases the season start publishes.
package competition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// worldCurrentDay mirrors the matchday runner's world-date mapping
// (date_trunc of launch + current_day days).
func worldCurrentDay(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) time.Time {
	t.Helper()
	var day time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT date_trunc('day', COALESCE(launched_at, created_at)) + make_interval(days => current_day::int)
		FROM world.worlds WHERE id = $1`, worldID).Scan(&day); err != nil {
		t.Fatalf("world current day: %v", err)
	}
	return day
}

func testimonyMatchdayOne(t *testing.T, pool *pgxpool.Pool, competitionID, worldID uuid.UUID) time.Time {
	t.Helper()
	var day time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT MIN(scheduled_at)::date FROM match.fixtures
		WHERE competition_id = $1 AND world_id = $2 AND matchday = 1`,
		competitionID, worldID).Scan(&day); err != nil {
		t.Fatalf("matchday 1 date: %v", err)
	}
	return day
}

func countAnnouncements(t *testing.T, pool *pgxpool.Pool, worldID, countryID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM world.news_stories
		WHERE world_id = $1 AND category = 'announcement' AND country_id = $2`,
		worldID, countryID).Scan(&n); err != nil {
		t.Fatalf("count announcement stories: %v", err)
	}
	return n
}

func TestStartSeasonKickoffPinsMatchdayOne(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	today := worldCurrentDay(t, pool, worldID)
	kickoff := today.AddDate(0, 0, 3)

	season, err := svc.StartSeasonKickoff(ctx, worldID, premier.ID, &kickoff)
	if err != nil {
		t.Fatalf("start season on %v: %v", kickoff, err)
	}
	if season.SeasonNumber != 1 || season.Status != "in_progress" {
		t.Fatalf("unexpected season: %+v", season)
	}
	if got := testimonyMatchdayOne(t, pool, premier.ID, worldID); !got.Equal(daysTruncate(kickoff)) {
		t.Fatalf("matchday 1 = %v, want the pinned kickoff %v", got, daysTruncate(kickoff))
	}

	// The start publishes exactly the two country-scoped press releases.
	if n := countAnnouncements(t, pool, worldID, countryID); n != 2 {
		t.Fatalf("announcement stories = %d, want 2", n)
	}
	var linked int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.news_stories ns
		JOIN world.events e ON e.id = ns.related_event_id
		WHERE ns.world_id = $1 AND ns.category = 'announcement' AND e.event_type = 'SEASON_CREATED'`,
		worldID).Scan(&linked); err != nil {
		t.Fatalf("link announcements to SEASON_CREATED: %v", err)
	}
	if linked != 2 {
		t.Fatalf("announcements linked to SEASON_CREATED = %d, want 2", linked)
	}
}

func TestStartSeasonDefaultKicksOffNextDay(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	today := worldCurrentDay(t, pool, worldID)
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start season: %v", err)
	}
	if got := testimonyMatchdayOne(t, pool, premier.ID, worldID); !got.Equal(today.AddDate(0, 0, 1)) {
		t.Fatalf("matchday 1 = %v, want the world's current date + 1 (%v)", got, today.AddDate(0, 0, 1))
	}
	if n := countAnnouncements(t, pool, worldID, countryID); n != 2 {
		t.Fatalf("announcement stories = %d, want 2 for the default kickoff", n)
	}
}

func TestStartSeasonKickoffValidation(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	today := worldCurrentDay(t, pool, worldID)

	// A date before the world's current date is rejected and leaves no trace.
	past := today.AddDate(0, 0, -1)
	if _, err := svc.StartSeasonKickoff(ctx, worldID, premier.ID, &past); !errors.Is(err, ErrKickoffDateInPast) {
		t.Fatalf("past kickoff err = %v, want ErrKickoffDateInPast", err)
	}
	var seasons int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM competition.seasons WHERE competition_id = $1`, premier.ID).Scan(&seasons); err != nil {
		t.Fatalf("count seasons: %v", err)
	}
	if seasons != 0 {
		t.Fatalf("rejected start must roll back; %d seasons exist", seasons)
	}

	// Under a country weekend default (Fri/Sat/Sun), an off-weekday kickoff is
	// rejected and an allowed weekday lands exactly on the date.
	weekend := []int{5, 6, 7}
	if _, err := svc.UpdateCountryScheduling(ctx, worldID, countryID, weekend); err != nil {
		t.Fatalf("set country weekdays: %v", err)
	}
	off := today.AddDate(0, 0, 1) // tomorrow's weekday is arbitrary; force off-set if allowed
	for isAllowedWeekday(off, weekend) {
		off = off.AddDate(0, 0, 1)
	}
	if _, err := svc.StartSeasonKickoff(ctx, worldID, premier.ID, &off); !errors.Is(err, ErrKickoffNotAllowedWeekday) {
		t.Fatalf("off-weekday kickoff (%v) err = %v, want ErrKickoffNotAllowedWeekday", off, err)
	}

	on := nextAllowedWeekday(today.AddDate(0, 0, 1), weekend)
	if _, err := svc.StartSeasonKickoff(ctx, worldID, premier.ID, &on); err != nil {
		t.Fatalf("allowed-weekday kickoff %v: %v", on, err)
	}
	if got := testimonyMatchdayOne(t, pool, premier.ID, worldID); !got.Equal(on) {
		t.Fatalf("weekday-paced matchday 1 = %v, want %v", got, on)
	}
	assertWeekdayInvariant(t, pool, premier.ID, weekend)
}
