//go:build integration

package matchday

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// runnerWorld returns a seeded two-tier world whose starter club is human
// (with a full preferred lineup) and its competition service.
func runnerWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, *competition.Service) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "matchday-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour City FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	starter := res.ClubID

	// The human club owns its XI.
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET is_ai_controlled = FALSE WHERE id = $1`, starter); err != nil {
		t.Fatalf("make human: %v", err)
	}
	rows, err := pool.Query(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY squad_number LIMIT 11`, starter)
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	slot := 0
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan player: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO club.club_lineups (slot, club_id, player_id) VALUES ($1, $2, $3)`,
			slot, starter, id); err != nil {
			t.Fatalf("write lineup slot %d: %v", slot, err)
		}
		slot++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate roster: %v", err)
	}
	if slot != 11 {
		t.Fatalf("lineup has %d slots, want 11", slot)
	}

	compSvc := competition.NewService(pool, nil)
	country, err := compSvc.CreateCountry(ctx, w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	premier, err := compSvc.CreateLeague(ctx, competition.LeagueParams{CountryID: country.ID, Name: "Premier", Tier: 1, TeamCount: 4, Relegations: 1})
	if err != nil {
		t.Fatalf("create premier: %v", err)
	}
	champ, err := compSvc.CreateLeague(ctx, competition.LeagueParams{CountryID: country.ID, Name: "Championship", Tier: 2, TeamCount: 4, Promotions: 1})
	if err != nil {
		t.Fatalf("create championship: %v", err)
	}
	if err := compSvc.UpdateLeagueAdjacency(ctx, premier.ID, nil, &champ.ID); err != nil {
		t.Fatalf("link premier->champ: %v", err)
	}
	if err := compSvc.UpdateLeagueAdjacency(ctx, champ.ID, &premier.ID, nil); err != nil {
		t.Fatalf("link champ->premier: %v", err)
	}
	if _, err := compSvc.SeedCompetition(ctx, w.ID, country.ID, premier.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return pool, w.ID, compSvc
}

func TestRunnerAdvancesMatchdaysAndRollsOver(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, compSvc)

	advance := func(days int) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`UPDATE world.worlds SET current_tick = current_tick + $1 WHERE id = $2`, days, worldID); err != nil {
			t.Fatalf("advance tick: %v", err)
		}
	}

	countCompleted := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'completed'`, worldID).Scan(&n); err != nil {
			t.Fatalf("count completed: %v", err)
		}
		return n
	}

	// Before any tick nothing is due.
	sum, err := runner.RunDue(ctx, worldID)
	if err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if sum.Played != 0 {
		t.Fatalf("initial played = %d, want 0", sum.Played)
	}

	// One matchday per daily tick: 6 matchdays x 2 leagues x 2 fixtures.
	for day := 1; day <= 6; day++ {
		advance(1)
		sum, err := runner.RunDue(ctx, worldID)
		if err != nil {
			t.Fatalf("day %d run: %v", day, err)
		}
		if sum.Played != 4 || sum.Applied != 4 {
			t.Fatalf("day %d: played=%d applied=%d, want 4/4 (one matchday across both leagues)", day, sum.Played, sum.Applied)
		}
		if got := countCompleted(); got != day*4 {
			t.Fatalf("day %d completed = %d, want %d", day, got, day*4)
		}
	}

	// Every completed fixture carries the engine's scoreline and the
	// standings write; the match row mirrors the fixture score.
	var mismatch, noApply int
	if err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE f.ht_score IS NULL OR f.at_score IS NULL),
			COUNT(*) FILTER (WHERE f.standings_applied_at IS NULL)
		FROM match.fixtures f
		WHERE f.world_id = $1 AND f.status = 'completed'`, worldID).Scan(&noApply, &mismatch); err != nil {
		t.Fatalf("scoreline check: %v", err)
	}
	if mismatch != 0 || noApply != 0 {
		t.Fatalf("missing fixture scores=%d or standings=%d", noApply, mismatch)
	}
	var scoreMismatch int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM match.fixtures f
		JOIN match.matches m ON m.fixture_id = f.id
		WHERE f.ht_score IS DISTINCT FROM m.home_score OR f.at_score IS DISTINCT FROM m.away_score`,
		).Scan(&scoreMismatch); err != nil {
		t.Fatalf("score mirror: %v", err)
	}
	if scoreMismatch != 0 {
		t.Fatalf("%d fixtures disagree with their match rows", scoreMismatch)
	}

	// Season 1 completed and rolled over into season 2 (upcoming) for both
	// leagues, with promotion/relegation movement events recorded.
	var s1Completed, s2Rows, movement int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons WHERE status = 'completed' AND world_id = $1`, worldID).Scan(&s1Completed); err != nil {
		t.Fatalf("season 1 status: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons WHERE season_number = 2 AND world_id = $1`, worldID).Scan(&s2Rows); err != nil {
		t.Fatalf("season 2 rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('CLUB_PROMOTED','CLUB_RELEGATED')`, worldID).Scan(&movement); err != nil {
		t.Fatalf("movement events: %v", err)
	}
	if s1Completed != 2 || s2Rows != 2 || movement != 2 {
		t.Fatalf("rollover s1completed=%d s2rows=%d movement=%d, want 2/2/2", s1Completed, s2Rows, movement)
	}

	// Redelivery at the same tick is a no-op.
	sum, err = runner.RunDue(ctx, worldID)
	if err != nil {
		t.Fatalf("redelivery run: %v", err)
	}
	if sum.Played != 0 || sum.Applied != 0 {
		t.Fatalf("redelivery played=%d applied=%d, want 0/0", sum.Played, sum.Applied)
	}

	// The next daily tick opens season 2: matchday 1 across both leagues.
	advance(1)
	sum, err = runner.RunDue(ctx, worldID)
	if err != nil {
		t.Fatalf("season 2 run: %v", err)
	}
	if sum.Played != 4 || sum.Applied != 4 {
		t.Fatalf("season 2: played=%d applied=%d, want 4/4", sum.Played, sum.Applied)
	}
	if got := countCompleted(); got != 28 {
		t.Fatalf("completed = %d, want 28", got)
	}

	// Season 2 now has standings and is playing.
	var s2Playing int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.standings st
		JOIN competition.seasons s ON s.id = st.season_id
		WHERE s.season_number = 2 AND s.world_id = $1`, worldID).Scan(&s2Playing); err != nil {
		t.Fatalf("season 2 standings: %v", err)
	}
	if s2Playing != 8 {
		t.Fatalf("season 2 standings rows = %d, want 8", s2Playing)
	}

	// MATCH_PLAYED events cover every simulated match.
	var playedEvents int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'MATCH_PLAYED'`, worldID).Scan(&playedEvents); err != nil {
		t.Fatalf("count MATCH_PLAYED: %v", err)
	}
	if playedEvents != 28 {
		t.Fatalf("MATCH_PLAYED events = %d, want 28", playedEvents)
	}
}

func TestRunnerNoFixturesIsNoop(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "matchday-empty")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, competition.NewService(pool, nil))

	sum, err := runner.RunDue(ctx, w.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.Played != 0 || sum.Applied != 0 || sum.Matchdays != 0 {
		t.Fatalf("noop run played=%d applied=%d matchdays=%d, want 0/0/0", sum.Played, sum.Applied, sum.Matchdays)
	}
}