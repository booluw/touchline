//go:build integration

package scout

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// seedScoutWorld mirrors the competition integration helpers: a world with a
// 4-team tier-1 league, fully seeded (clubs + players).
func seedScoutWorld(t *testing.T) (*pgxpool.Pool, *competition.Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "scout-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	svc := competition.NewService(pool, nil)
	country, err := svc.CreateCountry(ctx, w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	premier, err := svc.CreateLeague(ctx, competition.LeagueParams{
		CountryID: country.ID, Name: "Premier", Tier: 1, TeamCount: 4, Relegations: 1,
	})
	if err != nil {
		t.Fatalf("create premier: %v", err)
	}
	if _, err := svc.SeedWorld(ctx, w.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pool, svc, w.ID, premier.ID
}

func TestNextFixtureScout(t *testing.T) {
	pool, svc, worldID, premierID := seedScoutWorld(t)
	ctx := context.Background()
	sc := NewService(pool, svc)

	var clubID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT club_id FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'league' ORDER BY club_id LIMIT 1`, premierID).
		Scan(&clubID); err != nil {
		t.Fatalf("pick premier member: %v", err)
	}

	// Off-season: the club exists but has no fixtures → a nil view (200 null).
	view, err := sc.NextFixture(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("off-season next fixture: %v", err)
	}
	if view != nil {
		t.Fatalf("off-season view = %+v, want nil", view)
	}

	// Scoping errors mirror the club fixture reads.
	if _, err := sc.NextFixture(ctx, uuid.New(), clubID); !errors.Is(err, competition.ErrClubWorldMismatch) {
		t.Fatalf("cross-world err = %v, want ErrClubWorldMismatch", err)
	}
	if _, err := sc.Report(ctx, worldID, uuid.New()); !errors.Is(err, competition.ErrClubNotFound) {
		t.Fatalf("missing opponent err = %v, want ErrClubNotFound", err)
	}

	if _, err := svc.StartSeason(ctx, worldID, premierID); err != nil {
		t.Fatalf("start season: %v", err)
	}

	view, err = sc.NextFixture(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("in-season next fixture: %v", err)
	}
	if view == nil {
		t.Fatal("in-season view must not be nil")
	}
	f := view.Fixture
	if f.ScheduledAt.IsZero() {
		t.Fatal("next fixture must carry a real scheduled_at")
	}
	if view.Gameweek != f.Matchday || f.Gameweek != f.Matchday || view.Gameweek != 1 {
		t.Fatalf("gameweek mirroring wrong: view %d fixture %d matchday %d, want all 1",
			view.Gameweek, f.Gameweek, f.Matchday)
	}
	if view.HomeOrAway != "home" && view.HomeOrAway != "away" {
		t.Fatalf("home_or_away = %q, want home|away", view.HomeOrAway)
	}
	if view.Derby || view.GoldenGoal {
		t.Fatalf("league fixture derby=%v golden_goal=%v, want both false", view.Derby, view.GoldenGoal)
	}
	opp := view.Opponent
	if opp == nil {
		t.Fatal("opponent dossier missing")
	}
	if opp.Club.ID == clubID {
		t.Fatalf("opponent %v is the scouted club itself", opp.Club.ID)
	}
	if !opp.IsAIControlled {
		t.Fatal("a seeded-world opponent must be AI-controlled")
	}
	if opp.Reputation <= 0 || opp.Tier <= 0 {
		t.Fatalf("opponent reputation %d tier %d, want positive", opp.Reputation, opp.Tier)
	}
	if opp.SquadCount == 0 || len(opp.TopPlayers) == 0 {
		t.Fatalf("opponent squad %d top %+v, want a squad", opp.SquadCount, opp.TopPlayers)
	}
	// No matches played → no standings line, no form history.
	if opp.LeaguePosition != nil {
		t.Fatalf("league_position before results = %v, want nil", *opp.LeaguePosition)
	}
	if opp.Form.FormString != "" {
		t.Fatalf("form before results = %q, want empty", opp.Form.FormString)
	}

	// Give the opponent a form row (the deterministic engine writes these during
	// PlayFixture; seed it directly here) and play the whole of matchday 1 so
	// every club has a league line.
	if _, err := pool.Exec(ctx, `
		INSERT INTO club.form_state (club_id, current_rating, last_updated_tick, form_string)
		VALUES ($1, 1.060, 0, 'W-D-L-W')`, opp.Club.ID); err != nil {
		t.Fatalf("seed opponent form: %v", err)
	}
	md1, err := svc.GetFixtures(ctx, premierID, worldID, newInt(1))
	if err != nil {
		t.Fatalf("matchday 1 fixtures: %v", err)
	}
	if len(md1) != 2 {
		t.Fatalf("matchday 1 fixture count = %d, want 2", len(md1))
	}
	for _, fx := range md1 {
		if err := svc.ApplyResult(ctx, fx.ID, 1, 0); err != nil {
			t.Fatalf("apply matchday 1 result: %v", err)
		}
	}

	view, err = sc.NextFixture(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("next fixture after md1: %v", err)
	}
	if view == nil {
		t.Fatal("view must be present after matchday 1")
	}
	if view.Gameweek != view.Fixture.Matchday || view.Gameweek != 2 {
		t.Fatalf("after md1 gameweek = %d (matchday %d), want 2",
			view.Gameweek, view.Fixture.Matchday)
	}
	opp = view.Opponent
	if opp == nil {
		t.Fatal("opponent dossier missing after md1")
	}
	if opp.LeaguePosition == nil || *opp.LeaguePosition < 1 || *opp.LeaguePosition > 4 {
		t.Fatalf("opponent league position = %v, want 1..4", opp.LeaguePosition)
	}
	if opp.Form.FormString != "W-D-L-W" {
		t.Fatalf("opponent form string = %q, want W-D-L-W", opp.Form.FormString)
	}
	if opp.Form.CurrentRating < 1.05 || opp.Form.CurrentRating > 1.07 {
		t.Fatalf("opponent rating = %v, want ~1.06", opp.Form.CurrentRating)
	}
	for _, kp := range opp.TopPlayers {
		if kp.Rating < 1 || kp.Rating > 100 {
			t.Fatalf("key player rating %d out of bounds", kp.Rating)
		}
	}
}

func newInt(v int) *int { return &v }
