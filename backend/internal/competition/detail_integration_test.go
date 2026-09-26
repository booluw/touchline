//go:build integration

package competition

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/apiref"
)

// addGoalEvent inserts a completed match row for a fixture and a goal (or
// penalty) event for a player, so top-scorer aggregation sees it.
func addGoalEvent(t *testing.T, pool *pgxpool.Pool, worldID, fixtureID, playerID uuid.UUID, penalty bool) {
	t.Helper()
	ctx := context.Background()
	eventType := "goal"
	if penalty {
		eventType = "penalty_scored"
	}
	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.matches (fixture_id, world_id, seed, engine_version, status)
		VALUES ($1, $2, 1, 'test', 'completed')
		ON CONFLICT (fixture_id) DO UPDATE SET status = 'completed'
		RETURNING id`, fixtureID, worldID).Scan(&matchID); err != nil {
		t.Fatalf("insert match row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match.match_events (match_id, sequence, minute, event_type, player_id)
		VALUES ($1, 1, 10, $2, $3)`, matchID, eventType, playerID); err != nil {
		t.Fatalf("insert %s event: %v", eventType, err)
	}
}

// leaguePlayer returns the first active player of a club.
func leaguePlayer(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		SELECT id FROM player.players WHERE club_id = $1 LIMIT 1`, clubID).Scan(&id)
	if err != nil {
		t.Fatalf("load player of %s: %v", clubID, err)
	}
	return id
}

// TestCompetitionDetailLeague asserts the league dossier: base identity,
// enriched table with country/streak/next fixture, history, scorers, and an
// empty movement for a first season.
func TestCompetitionDetailLeague(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start season: %v", err)
	}

	fixtures, err := svc.GetFixtures(ctx, premier.ID, worldID, newInt(1))
	if err != nil {
		t.Fatalf("matchday 1: %v", err)
	}
	for _, f := range fixtures {
		if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
			t.Fatalf("apply result: %v", err)
		}
	}

	detail, err := svc.CompetitionDetail(ctx, premier.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}

	if detail.CompetitionType != "league" || detail.League == nil || detail.Cup != nil {
		t.Fatalf("discriminator: type=%s league=%v cup=%v", detail.CompetitionType, detail.League, detail.Cup)
	}
	if detail.Country == nil || detail.Country.Code != "eng" || detail.Country.Name != "England" {
		t.Fatalf("country = %+v, want eng/England", detail.Country)
	}
	if detail.Tier == nil || *detail.Tier != 1 {
		t.Fatalf("tier = %v, want 1", detail.Tier)
	}
	if detail.TeamCount != 4 {
		t.Fatalf("team_count = %d, want 4", detail.TeamCount)
	}
	if detail.SeasonsTotal != 1 {
		t.Fatalf("seasons_total = %d, want 1", detail.SeasonsTotal)
	}
	if len(detail.PastWinners) != 0 {
		t.Fatalf("past winners = %+v, want none for an uncompleted first season", detail.PastWinners)
	}

	lg := detail.League
	if lg.Season == nil || lg.Season.Status != "in_progress" {
		t.Fatalf("season = %+v, want in_progress", lg.Season)
	}
	if lg.Season.FixturesPlayed != 2 || lg.Season.FixturesTotal != 12 {
		t.Fatalf("fixture progress = %d/%d, want 2/12", lg.Season.FixturesPlayed, lg.Season.FixturesTotal)
	}
	if len(lg.Seasons) != 1 {
		t.Fatalf("seasons = %d, want 1", len(lg.Seasons))
	}
	if len(lg.Standings) != 4 {
		t.Fatalf("standings rows = %d, want 4", len(lg.Standings))
	}
	for _, r := range lg.Standings {
		if r.Country.Name != "England" || r.Country.Code != "eng" {
			t.Fatalf("standing country = %+v, want England/eng", r.Country)
		}
		if r.Streak.Outcome != "W" && r.Streak.Outcome != "D" && r.Streak.Outcome != "L" {
			t.Fatalf("streak outcome = %q, want W/D/L", r.Streak.Outcome)
		}
		if r.Streak.Length != 1 {
			t.Fatalf("streak length = %d, want 1 after one fixture", r.Streak.Length)
		}
		if r.NextFixture == nil {
			t.Fatalf("next fixture = nil, want a scheduled matchday-2 fixture")
		}
	}
	if len(lg.Movement.PromotedIn) != 0 || len(lg.Movement.RelegatedOut) != 0 {
		t.Fatalf("movement = %+v, want empty for the first season", lg.Movement)
	}

	// Scorer history: a goal + a penalty for the top club's player, one goal
	// for the runner-up's, all inside the current season window.
	top, second := lg.Standings[0], lg.Standings[1]
	topPlayer := leaguePlayer(t, pool, top.Club.ID)
	secondPlayer := leaguePlayer(t, pool, second.Club.ID)

	var topFixtureID uuid.UUID
	for _, f := range fixtures {
		if f.HomeClub.ID == top.Club.ID || f.AwayClub.ID == top.Club.ID {
			topFixtureID = f.ID
			break
		}
	}
	var secondFixtureID uuid.UUID
	for _, f := range fixtures {
		if f.HomeClub.ID == second.Club.ID || f.AwayClub.ID == second.Club.ID {
			secondFixtureID = f.ID
			break
		}
	}
	addGoalEvent(t, pool, worldID, topFixtureID, topPlayer, false)
	addGoalEvent(t, pool, worldID, topFixtureID, topPlayer, true)
	addGoalEvent(t, pool, worldID, secondFixtureID, secondPlayer, false)

	detail, err = svc.CompetitionDetail(ctx, premier.ID)
	if err != nil {
		t.Fatalf("detail after goals: %v", err)
	}
	cur := detail.TopScorers.CurrentSeason
	if len(cur) != 2 || cur[0].Player.ID != topPlayer {
		t.Fatalf("current scorers = %+v, want top player first", cur)
	}
	if cur[0].Goals != 2 || cur[0].Penalties != 1 {
		t.Fatalf("top scorer goals = %d (pens %d), want 2/1", cur[0].Goals, cur[0].Penalties)
	}
	if cur[0].Club == nil || cur[0].Club.ID != top.Club.ID {
		t.Fatalf("top scorer club = %+v, want %s", cur[0].Club, top.Club.Name)
	}
	if cur[1].Player.ID != secondPlayer || cur[1].Goals != 1 {
		t.Fatalf("second scorer = %+v, want %s with 1 goal", cur[1], secondPlayer)
	}
	if detail.TopScorers.AllTime[len(detail.TopScorers.AllTime)-1].Player.ID != secondPlayer {
		t.Fatalf("all-time tail = %+v, want the 1-goal scorer", detail.TopScorers.AllTime)
	}
}

// playAllFixtures applies a 2-1 result to every fixture until none remain,
// letting the season completion cascade (and the country rollover) run.
func playAllFixtures(t *testing.T, svc *Service, worldID uuid.UUID, leagues ...*League) {
	t.Helper()
	ctx := context.Background()
	played := map[uuid.UUID]bool{}
	for md := 1; ; md++ {
		advanced := false
		for _, l := range leagues {
			fixtures, err := svc.GetFixtures(ctx, l.ID, worldID, newInt(md))
			if err != nil {
				t.Fatalf("matchday %d fixtures of %s: %v", md, l.Name, err)
			}
			for _, f := range fixtures {
				if played[f.ID] {
					continue
				}
				if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
					t.Fatalf("apply result: %v", err)
				}
				played[f.ID] = true
				advanced = true
			}
		}
		if !advanced {
			return
		}
	}
}

// TestCompetitionDetailLeagueMovement completes a real two-tier season so the
// rollover creates the next season and movement; asserts the dossier's
// movement matches the actual membership churn and past winners = the
// completed season's champion.
func TestCompetitionDetailLeagueMovement(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier season: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ season: %v", err)
	}
	playAllFixtures(t, svc, worldID, premier, champ)

	// The rollover created a second (upcoming) season for each league.
	seasons, err := svc.detailSeasons(ctx, premier.ID)
	if err != nil {
		t.Fatalf("seasons: %v", err)
	}
	if len(seasons) != 2 {
		t.Fatalf("premier seasons = %d, want 2 after rollover", len(seasons))
	}
	wantJoined, wantDeparted, err := svc.membershipDiff(ctx, seasons[0].ref.ID, seasons[1].ref.ID)
	if err != nil {
		t.Fatalf("membership diff: %v", err)
	}
	if len(wantJoined) == 0 {
		t.Fatal("expected membership churn between the two seasons")
	}

	detail, err := svc.CompetitionDetail(ctx, premier.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.PastWinners) != 1 {
		t.Fatalf("past winners = %+v, want the one completed season", detail.PastWinners)
	}

	lg := detail.League
	if lg.Season == nil || lg.Season.Status != "upcoming" {
		t.Fatalf("latest season = %+v, want upcoming after rollover", lg.Season)
	}
	if lg.Season.FixturesPlayed != 0 || lg.Season.FixturesTotal != 12 {
		t.Fatalf("latest fixture progress = %d/%d, want 0/12", lg.Season.FixturesPlayed, lg.Season.FixturesTotal)
	}
	if len(lg.Standings) != 0 {
		t.Fatalf("standings = %+v, want empty for an upcoming season", lg.Standings)
	}

	if !sameClubs(wantJoined, lg.Movement.PromotedIn) {
		t.Fatalf("promoted_in = %+v, want %v", lg.Movement.PromotedIn, wantJoined)
	}
	if !sameClubs(wantDeparted, lg.Movement.RelegatedOut) {
		t.Fatalf("relegated_out = %+v, want %v", lg.Movement.RelegatedOut, wantDeparted)
	}
}

// TestCompetitionDetailCup asserts the cup dossier: past winners from a
// completed campaign and the current campaign with ties, per-entrant
// elimination, round reached, streaks, and a decisive champion absent.
func TestCompetitionDetailCup(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var clubs []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY name LIMIT 4`, worldID)
	if err != nil {
		t.Fatalf("clubs: %v", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan club: %v", err)
		}
		clubs = append(clubs, id)
	}
	rows.Close()
	if len(clubs) != 4 {
		t.Fatalf("clubs = %d, want 4", len(clubs))
	}

	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID:   worldID,
		CountryID: countryID,
		Name:      "National Cup",
		Tier:      newInt(1),
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}

	// Completed campaign #1 with a champion.
	declareReigningChampion(t, pool, svc, worldID, cup.ID, clubs[0])

	// Live campaign #2: rounds with two decided ties (round 1) and entries.
	var seasonID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.seasons (world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, '2', 2, now()::date, 'in_progress')
		RETURNING id`, worldID, cup.ID).Scan(&seasonID); err != nil {
		t.Fatalf("insert live cup season: %v", err)
	}
	statuses := []string{"eliminated", "registered", "registered", "registered"}
	for i, clubID := range clubs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.competition_entries (season_id, club_id, status)
			VALUES ($1, $2, $3)`, seasonID, clubID, statuses[i]); err != nil {
			t.Fatalf("insert cup entry: %v", err)
		}
	}
	kickoff := time.Now().Add(24 * time.Hour)
	ties := [][2]int{{0, 1}, {2, 3}}
	for idx, pair := range ties {
		home, away := clubs[pair[0]], clubs[pair[1]]
		if _, err := pool.Exec(ctx, `
			INSERT INTO match.fixtures
				(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status, ht_score, at_score)
			VALUES ($1, $2, $3, $4, 1, $5, 'completed', $6, $7)`,
			worldID, cup.ID, home, away, kickoff.Add(time.Duration(idx)*time.Hour), 2, 0); err != nil {
			t.Fatalf("insert cup fixture: %v", err)
		}
	}

	detail, err := svc.CompetitionDetail(ctx, cup.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.CompetitionType != "domestic_cup" || detail.Cup == nil || detail.League != nil {
		t.Fatalf("discriminator: type=%s cup=%v league=%v", detail.CompetitionType, detail.Cup, detail.League)
	}
	if detail.Country == nil || detail.Country.Code != "eng" {
		t.Fatalf("country = %+v, want eng", detail.Country)
	}

	if len(detail.PastWinners) != 1 || detail.PastWinners[0].Champion.ID != clubs[0] {
		t.Fatalf("past winners = %+v, want %s", detail.PastWinners, clubs[0])
	}

	cd := detail.Cup
	if cd.Season == nil || cd.Season.Label != "2" || cd.Season.Status != "in_progress" {
		t.Fatalf("campaign season = %+v, want live season 2", cd.Season)
	}
	if cd.Champion != nil {
		t.Fatalf("champion = %+v, want nil for the undecided live campaign", cd.Champion)
	}
	if len(cd.Rounds) != 1 || len(cd.Rounds[0].Ties) != 2 {
		t.Fatalf("rounds = %+v, want one round with two ties", cd.Rounds)
	}
	if len(cd.Clubs) != 4 {
		t.Fatalf("entrants = %d, want 4", len(cd.Clubs))
	}
	for _, row := range cd.Clubs {
		if row.Country.Name != "England" || row.Country.Code != "eng" {
			t.Fatalf("entrant country = %+v, want England/eng", row.Country)
		}
		if row.CurrentRound != 1 {
			t.Fatalf("entrant %s round = %d, want 1 (played a round-1 tie)", row.Club.Name, row.CurrentRound)
		}
	}
	// clubs[0] lost its round-1 tie (2-0 home win for clubs[1]); the others won.
	byID := map[uuid.UUID]CupClubRow{}
	for _, row := range cd.Clubs {
		byID[row.Club.ID] = row
	}
	if !byID[clubs[0]].Eliminated {
		t.Fatalf("clubs[0] should be eliminated")
	}
	for _, id := range clubs[1:] {
		if byID[id].Eliminated {
			t.Fatalf("clubs %s should still be alive", id)
		}
	}
	if byID[clubs[0]].Streak.Outcome != "L" || byID[clubs[0]].Streak.Length != 1 {
		t.Fatalf("clubs[0] streak = %+v, want L/1", byID[clubs[0]].Streak)
	}
	if byID[clubs[1]].Streak.Outcome != "W" || byID[clubs[1]].Streak.Length != 1 {
		t.Fatalf("clubs[1] streak = %+v, want W/1", byID[clubs[1]].Streak)
	}
}

// sameClubs reports whether a list of club refs matches an unordered id set.
func sameClubs(ids []uuid.UUID, refs []apiref.ClubRef) bool {
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	got := map[uuid.UUID]bool{}
	for _, r := range refs {
		got[r.ID] = true
	}
	if len(want) != len(got) {
		return false
	}
	for id := range want {
		if !got[id] {
			return false
		}
	}
	return true
}
