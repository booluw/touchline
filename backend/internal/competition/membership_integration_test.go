//go:build integration

package competition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/playerpool"
	internalworld "github.com/touchline/backend/internal/world"
)

// createLeagueLessClub materializes one free-floating AI club (no league
// membership) in the country, replenishing the free-agent pool first so its
// squad draft succeeds. The club is the IM14 raw material for
// AddClubToLeague and for the rollover auto-fill pool.
func createLeagueLessClub(t *testing.T, pool *pgxpool.Pool, worldID, countryID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin club tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 1) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		t.Fatalf("load seed: %v", err)
	}
	var worldRef time.Time
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1`, worldID).Scan(&worldRef); err != nil {
		t.Fatalf("load world ref: %v", err)
	}
	factory, err := squadFactory(ctx, tx, hashMix(seed, worldID))
	if err != nil {
		t.Fatalf("squad factory: %v", err)
	}
	if err := playerpool.ReplenishPool(ctx, tx, nil, worldID, &countryID, playerpool.PoolTargetSize, factory, daysTruncate(worldRef)); err != nil {
		t.Fatalf("replenish pool: %v", err)
	}
	club, err := bootstrap.GenerateAIClub(ctx, nil, tx, worldID, name, "AAA", "England", &countryID)
	if err != nil {
		t.Fatalf("generate league-less club: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit club: %v", err)
	}
	return club.ClubID
}

func seasonClubIDs(t *testing.T, pool *pgxpool.Pool, leagueID uuid.UUID, number int) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT e.club_id FROM competition.competition_entries e
		JOIN competition.seasons s ON s.id = e.season_id
		WHERE s.competition_id = $1 AND s.season_number = $2 AND s.status <> 'completed'
		ORDER BY e.club_id`, leagueID, number)
	if err != nil {
		t.Fatalf("query season entries: %v", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan entry: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate entries: %v", err)
	}
	return out
}

// playThrough runs every fixture of both leagues to completion with a fixed
// result, returning the number of fixtures played (24 for a 4-team two-tier
// country; the last result triggers the country rollover).
func playThrough(t *testing.T, svc *Service, worldID uuid.UUID, leagues ...uuid.UUID) int {
	t.Helper()
	ctx := context.Background()
	played := map[uuid.UUID]bool{}
	for round := 0; round < 6; round++ {
		for _, leagueID := range leagues {
			fixtures, err := svc.GetFixtures(ctx, leagueID, worldID, newInt(round+1))
			if err != nil {
				t.Fatalf("matchday %d fixtures: %v", round+1, err)
			}
			for _, f := range fixtures {
				if played[f.ID] {
					continue
				}
				if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
					t.Fatalf("apply result: %v", err)
				}
				played[f.ID] = true
			}
		}
	}
	return len(played)
}

// TestMembershipAddNoSeasonComposes: a league that has never played composes
// its first season from declared members exactly; the add-cap allows team_count
// adds and refuses the team_count+1th.
func TestMembershipAddNoSeasonComposes(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	league, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Premier Only", Tier: 1, TeamCount: 4})
	if err != nil {
		t.Fatalf("create league: %v", err)
	}

	added := []uuid.UUID{}
	for i := 0; i < 4; i++ {
		clubID := createLeagueLessClub(t, pool, worldID, countryID, "FC Drafted "+string(rune('A'+i)))
		admission, err := svc.AddClubToLeague(ctx, worldID, clubID, league.ID)
		if err != nil {
			t.Fatalf("add club %d: %v", i, err)
		}
		if admission.League.TeamCount != 4 {
			t.Fatalf("admission league team_count = %d, want 4", admission.League.TeamCount)
		}
		added = append(added, clubID)
	}

	// The club's country was normalised to the league's country.
	var countryText string
	if err := pool.QueryRow(ctx,
		`SELECT country FROM club.clubs WHERE id = $1`, added[0]).Scan(&countryText); err != nil {
		t.Fatalf("read club country: %v", err)
	}
	if countryText != "England" {
		t.Fatalf("club country = %q, want normalized England", countryText)
	}

	// team_count+1th add is refused by the add-cap.
	overfill := createLeagueLessClub(t, pool, worldID, countryID, "FC Overflow")
	if _, err := svc.AddClubToLeague(ctx, worldID, overfill, league.ID); !errors.Is(err, ErrLeagueFull) {
		t.Fatalf("overfill err = %v, want ErrLeagueFull", err)
	}

	season, err := svc.StartSeason(ctx, worldID, league.ID)
	if err != nil {
		t.Fatalf("start season: %v", err)
	}
	got := seasonClubIDs(t, pool, league.ID, season.SeasonNumber)
	if len(got) != 4 {
		t.Fatalf("first-season entries = %d, want 4", len(got))
	}
	for _, id := range added {
		found := false
		for _, e := range got {
			if e == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("declared club %s missing from first season", id)
		}
	}
}

// TestMembershipAddAndResizeRollover: mid-season the admin raises Premier to 6
// and adds one league-less club; the live season is untouched, and the next
// season composes to exactly 6 (declared club in), Championship stays 4.
func TestMembershipAddAndResizeRollover(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}

	declared := createLeagueLessClub(t, pool, worldID, countryID, "FC Add Next Season")

	// Resize first (lowering never allowed; adds are gated by team_count).
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 6, Promotions: 0, Relegations: 1}); err != nil {
		t.Fatalf("resize premier: %v", err)
	}
	if _, err := svc.AddClubToLeague(ctx, worldID, declared, premier.ID); err != nil {
		t.Fatalf("add declared club: %v", err)
	}

	// The live season still has 4 entries each.
	if got := len(seasonClubIDs(t, pool, premier.ID, 1)); got != 4 {
		t.Fatalf("live premier entries = %d, want 4 (live season untouched)", got)
	}

	played := playThrough(t, svc, worldID, premier.ID, champ.ID)
	if played != 24 {
		t.Fatalf("played = %d, want 24", played)
	}

	premierS2 := seasonClubIDs(t, pool, premier.ID, 2)
	champS2 := seasonClubIDs(t, pool, champ.ID, 2)
	if len(premierS2) != 6 {
		t.Fatalf("premier season 2 = %d entries, want 6", len(premierS2))
	}
	if len(champS2) != 4 {
		t.Fatalf("champ season 2 = %d entries, want 4", len(champS2))
	}
	foundDeclared := false
	for _, id := range premierS2 {
		if id == declared {
			foundDeclared = true
		}
	}
	if !foundDeclared {
		t.Fatal("declared club missing from premier season 2")
	}

	// The declared club now has its role='league' membership on Premier.
	var row bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.club_competitions
			WHERE club_id = $1 AND competition_id = $2 AND role = 'league')`,
		declared, premier.ID).Scan(&row); err != nil {
		t.Fatalf("check declared membership: %v", err)
	}
	if !row {
		t.Fatal("declared club has no role='league' row on Premier")
	}

	// Both audit events exist.
	var joins, resizes int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'CLUB_JOINED_LEAGUE'`, worldID).Scan(&joins)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'LEAGUE_CAPACITY_CHANGED'`, worldID).Scan(&resizes)
	if joins != 1 || resizes != 1 {
		t.Fatalf("events joins=%d resizes=%d, want 1/1", joins, resizes)
	}
}

// TestMembershipCapacityAutoAdjustsNeighboursAndAutoFill: resizing the second
// tier to 6 with 2 promotions auto-adjusts the top tier's relegations to 2 (in
// the same transaction) and the rollover composes the enlarged tier, topping up
// from fresh AI clubs when the country has no league-less clubs left.
func TestMembershipCapacityAutoAdjustsNeighboursAndAutoFill(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}

	if _, err := svc.SetLeagueCapacity(ctx, champ.ID, CapacityParams{TeamCount: 6, Promotions: 2, Relegations: 0}); err != nil {
		t.Fatalf("resize champ: %v", err)
	}

	// Reciprocal auto-adjust: the top tier now relegates 2 (it already had a
	// link to the tier below, so the edit moves both sides of the edge).
	premierAfter, err := svc.GetLeague(ctx, worldID, premier.ID)
	if err != nil {
		t.Fatalf("reload premier: %v", err)
	}
	if premierAfter.Relegations != 2 {
		t.Fatalf("premier relegations = %d, want 2 (auto-adjusted)", premierAfter.Relegations)
	}

	played := playThrough(t, svc, worldID, premier.ID, champ.ID)
	if played != 24 {
		t.Fatalf("played = %d, want 24", played)
	}

	premierS2 := seasonClubIDs(t, pool, premier.ID, 2)
	champS2 := seasonClubIDs(t, pool, champ.ID, 2)
	if len(premierS2) != 4 {
		t.Fatalf("premier season 2 = %d entries, want 4", len(premierS2))
	}
	if len(champS2) != 6 {
		t.Fatalf("champ season 2 = %d entries, want 6 (auto-filled)", len(champS2))
	}

	// The two top-up seats became real league members of the tier.
	var champMembers int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'league'`, champ.ID).Scan(&champMembers); err != nil {
		t.Fatalf("count champ members: %v", err)
	}
	if champMembers != 6 {
		t.Fatalf("champ members = %d, want 6", champMembers)
	}

	// A coherent ladder survived the resize.
	var country int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.competition_rules r
		JOIN competition.competitions c ON c.id = r.competition_id
		JOIN competition.competitions above ON above.id = r.promotes_to_competition_id
		JOIN competition.competition_rules ar ON ar.competition_id = above.id
		WHERE c.country_id = $1 AND ar.relegations <> r.promotions`,
		countryID).Scan(&country); err != nil {
		t.Fatalf("ladder coherence check: %v", err)
	}
	if country != 0 {
		t.Fatalf("%d promotion/relegation edges broke adjacency", country)
	}
}

// TestMembershipPoolFillPrecedence: declared members land first, then the
// league-less country pool is consumed (by name), and only then are AI clubs
// generated — so an existing league-less club is reused before a facsimile is
// invented.
func TestMembershipPoolFillPrecedence(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}

	// A league-less club the rollover should reuse before inventing a new one.
	poolClub := createLeagueLessClub(t, pool, worldID, countryID, "FC Drawn From Pool")
	totalClubsBefore := 8 + 1

	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 6, Promotions: 0, Relegations: 1}); err != nil {
		t.Fatalf("resize premier: %v", err)
	}
	playThrough(t, svc, worldID, premier.ID, champ.ID)

	premierS2 := seasonClubIDs(t, pool, premier.ID, 2)
	if len(premierS2) != 6 {
		t.Fatalf("premier season 2 = %d entries, want 6", len(premierS2))
	}
	reused := false
	for _, id := range premierS2 {
		if id == poolClub {
			reused = true
		}
	}
	if !reused {
		t.Fatal("league-less club not reused by the auto-fill")
	}

	// Exactly one new AI club was generated to reach 6 (pool club + 1 new).
	var totalClubs int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.clubs WHERE world_id = $1`, worldID).Scan(&totalClubs); err != nil {
		t.Fatalf("count clubs: %v", err)
	}
	if totalClubs != totalClubsBefore+1 {
		t.Fatalf("clubs = %d, want %d (1 generated AI club)", totalClubs, totalClubsBefore+1)
	}

	// The reused club now carries its role='league' membership on Premier.
	var row bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.club_competitions
			WHERE club_id = $1 AND competition_id = $2 AND role = 'league')`,
		poolClub, premier.ID).Scan(&row); err != nil {
		t.Fatalf("check pooled club membership: %v", err)
	}
	if !row {
		t.Fatal("pooled club has no role='league' row on Premier")
	}
}

// TestMembershipRefusals locks the IM14 validation walls.
func TestMembershipRefusals(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}

	// Shrink is refused (raise first, then lower).
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 6, Promotions: 0, Relegations: 1}); err != nil {
		t.Fatalf("raise premier: %v", err)
	}
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 4, Promotions: 0, Relegations: 1}); !errors.Is(err, ErrLeagueShrink) {
		t.Fatalf("shrink err = %v, want ErrLeagueShrink", err)
	}

	// Invalid counts (sum >= team_count) and odd/too-small team sizes.
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 4, Promotions: 0, Relegations: 4}); !errors.Is(err, ErrInvalidCounts) {
		t.Fatalf("counts err = %v, want ErrInvalidCounts", err)
	}
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 5, Promotions: 0, Relegations: 0}); !errors.Is(err, ErrInvalidTeamCount) {
		t.Fatalf("odd team err = %v, want ErrInvalidTeamCount", err)
	}

	// Promoting out of a tier-1 league without a link is refused.
	if _, err := svc.SetLeagueCapacity(ctx, premier.ID, CapacityParams{TeamCount: 6, Promotions: 1, Relegations: 1}); !errors.Is(err, ErrAdjacencyMismatch) {
		t.Fatalf("promote-without-link err = %v, want ErrAdjacencyMismatch", err)
	}

	// A leagued club cannot be added to another league.
	seeded, err := svc.ListCountryLeagues(ctx, countryID)
	if err != nil || len(seeded) == 0 {
		t.Fatalf("list leagues: %v", err)
	}
	member := seasonClubIDs(t, pool, premier.ID, 1)[0]
	if _, err := svc.AddClubToLeague(ctx, worldID, member, seeded[1].ID); !errors.Is(err, ErrClubAlreadyInLeague) {
		t.Fatalf("already-leagued err = %v, want ErrClubAlreadyInLeague", err)
	}

	// A league-less club cannot join a structurally full league.
	full := createLeagueLessClub(t, pool, worldID, countryID, "FC Full House")
	if _, err := svc.AddClubToLeague(ctx, worldID, full, premier.ID); !errors.Is(err, ErrLeagueFull) {
		t.Fatalf("full-league err = %v, want ErrLeagueFull", err)
	}

	// Cross-world club / league refusals.
	otherWorld, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "membership-it-other")
	if err != nil {
		t.Fatalf("create other world: %v", err)
	}
	otherCountry, err := NewService(pool, nil).CreateCountry(ctx, otherWorld.ID, "deu", "Germany")
	if err != nil {
		t.Fatalf("create other country: %v", err)
	}
	alien := createLeagueLessClub(t, pool, otherWorld.ID, otherCountry.ID, "FC Alien")
	if _, err := svc.AddClubToLeague(ctx, worldID, alien, premier.ID); !errors.Is(err, ErrClubWorldMismatch) {
		t.Fatalf("alien-club err = %v, want ErrClubWorldMismatch", err)
	}
	if _, err := svc.AddClubToLeague(ctx, otherWorld.ID, alien, premier.ID); !errors.Is(err, ErrCompetitionWorldMismatch) {
		t.Fatalf("alien-league err = %v, want ErrCompetitionWorldMismatch", err)
	}

	// Odd member count is refused up front (the round-robin padding pitfall).
	oddLeague, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Odd Tier", Tier: 3, TeamCount: 4})
	if err != nil {
		t.Fatalf("create odd league: %v", err)
	}
	for i := 0; i < 3; i++ {
		c := createLeagueLessClub(t, pool, worldID, countryID, "FC Odd "+string(rune('1'+i)))
		if _, err := svc.AddClubToLeague(ctx, worldID, c, oddLeague.ID); err != nil {
			t.Fatalf("add odd club %d: %v", i, err)
		}
	}
	if _, err := svc.StartSeason(ctx, worldID, oddLeague.ID); !errors.Is(err, ErrOddMemberCount) {
		t.Fatalf("odd start err = %v, want ErrOddMemberCount", err)
	}
}
