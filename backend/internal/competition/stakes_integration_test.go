//go:build integration

package competition

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/pkg/playergen"
)

// TestStakesSixPointerBands drives a real seeded two-tier season and crafts
// the table so each matchday-2 fixture sits in exactly one battle band:
// the Premier (relegations only) has a two-club drop fight, the Championship
// (promotions only) a two-club promotion chase.
func TestStakesSixPointerBands(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedCompetition(ctx, worldID, countryID, premier.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Before any result exists there is nothing to stake against — the calls
	// must report "no stakes" without erroring.
	md1, err := svc.GetFixtures(ctx, premier.ID, worldID, newInt(1))
	if err != nil {
		t.Fatalf("matchday 1 fixtures: %v", err)
	}
	for _, f := range md1 {
		if sp, err := svc.IsSixPointer(ctx, f.ID); err != nil || sp {
			t.Fatalf("early six-pointer: sp=%v err=%v, want false/nil", sp, err)
		}
		if dr, err := svc.IsDeadRubber(ctx, f.ID); err != nil || dr {
			t.Fatalf("early dead rubber: dr=%v err=%v, want false/nil", dr, err)
		}
	}

	for name, league := range map[string]*League{"premier": premier, "champ": champ} {
		fixtures, err := svc.GetFixtures(ctx, league.ID, worldID, newInt(1))
		if err != nil {
			t.Fatalf("%s matchday 1: %v", name, err)
		}
		md2, err := svc.GetFixtures(ctx, league.ID, worldID, newInt(2))
		if err != nil {
			t.Fatalf("%s matchday 2: %v", name, err)
		}
		if len(md2) != 2 {
			t.Fatalf("%s matchday 2 fixtures = %d, want 2", name, len(md2))
		}

		// pair1 clubs play each other on MD2; pair2 clubs play each other on
		// MD2 (the two MD2 matches partition the league). Make pair1 win MD1,
		// pair2 lose — pair1 then owns positions 1&2, pair2 positions 3&4.
		pair1 := map[uuid.UUID]bool{md2[0].HomeClubID: true, md2[0].AwayClubID: true}
		for _, f := range fixtures {
			if pair1[f.HomeClubID] {
				if err := svc.ApplyResult(ctx, f.ID, 2, 0); err != nil {
					t.Fatalf("%s apply md1: %v", name, err)
				}
			} else if err := svc.ApplyResult(ctx, f.ID, 0, 2); err != nil {
				t.Fatalf("%s apply md1: %v", name, err)
			}
		}

		for _, f := range md2 {
			sp, err := svc.IsSixPointer(ctx, f.ID)
			if err != nil {
				t.Fatalf("%s six-pointer: %v", name, err)
			}
			dr, err := svc.IsDeadRubber(ctx, f.ID)
			if err != nil {
				t.Fatalf("%s dead rubber: %v", name, err)
			}
			bothWinner := pair1[f.HomeClubID]

			// Premier (1 relegation): only the two-win pair is a six-pointer.
			if league.ID == premier.ID {
				if bothWinner {
					// winning pair holds positions 1&2 — above the drop band.
					if sp {
						t.Fatalf("premier winner pair: six-pointer, want false (out of drop band)")
					}
				} else if !sp {
					t.Fatalf("premier loser pair in drop band: six-pointer, want true")
				}
			}
			// Championship (1 promotion): only the two-win pair is a six-pointer.
			if league.ID == champ.ID {
				if bothWinner && !sp {
					t.Fatalf("champ winner pair in promotion band: six-pointer, want true")
				}
				if !bothWinner && sp {
					t.Fatalf("champ loser pair below promotion band: six-pointer, want false")
				}
			}
			// Mid-season nothing is dead for either club.
			if dr {
				t.Fatalf("matchday 2 %s: dead rubber, want false", name)
			}
		}
	}
}

// TestStakesDeadRubberFabricated builds an 8-team league (2 up / 2 down) and
// materializes a late-season table directly so the risk bands are exact.
func TestStakesDeadRubberFabricated(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	league, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Fab League", Tier: 1, TeamCount: 8, Promotions: 2, Relegations: 2})
	if err != nil {
		t.Fatalf("create league: %v", err)
	}

	clubs := genClubs(t, pool, ctx, worldID, 8)
	season := fabricateSeason(t, pool, ctx, worldID, league.ID, clubs)

	// Table (points): S1=10 S2=9 S3=8 S4=5 S5=4 S6=3 S7=2 S8=1.
	// Positions: S1=1 S2=2 S3=3 S4=4 S5=5 S6=6 S7=7 S8=8.
	pts := []int{10, 9, 8, 5, 4, 3, 2, 1}
	for i, club := range clubs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.standings
				(season_id, club_id, played, won, drawn, lost, goals_for, goals_against, points)
			VALUES ($1, $2, 8, 2, 0, 6, $3, 12, $4)`,
			season, club, i+1, pts[i]); err != nil {
			t.Fatalf("insert standing: %v", err)
		}
	}

	// Three matchday-5 fixtures with distinct kickoffs:
	//   f1: S1 v S3   (positions 1 and 3 — promotion band, gap 2)
	//   f2: S5 v S6   (positions 5 and 6 — provably safe middle)
	//   f3: S7 v S8   (positions 7 and 8 — relegation band)
	type want struct {
		home, away int
		six, dead  bool
	}
	cases := []want{
		{home: 0, away: 2, six: true, dead: false},  // promotion six-pointer
		{home: 4, away: 5, six: false, dead: true},  // safe mid-table dead rubber
		{home: 6, away: 7, six: true, dead: false},  // relegation six-pointer
	}
	base := time.Date(2030, 5, 10, 15, 0, 0, 0, time.UTC)
	for i, c := range cases {
		var fixtureID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO match.fixtures
				(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
			VALUES ($1, $2, $3, $4, 5, $5, 'scheduled') RETURNING id`,
			worldID, league.ID, clubs[c.home], clubs[c.away], base.Add(time.Duration(i)*time.Hour)).Scan(&fixtureID); err != nil {
			t.Fatalf("insert fixture: %v", err)
		}

		sp, err := svc.IsSixPointer(ctx, fixtureID)
		if err != nil {
			t.Fatalf("six-pointer: %v", err)
		}
		dr, err := svc.IsDeadRubber(ctx, fixtureID)
		if err != nil {
			t.Fatalf("dead rubber: %v", err)
		}
		if sp != c.six {
			t.Fatalf("fixture %d six-pointer = %v, want %v", i, sp, c.six)
		}
		if dr != c.dead {
			t.Fatalf("fixture %d dead rubber = %v, want %v", i, dr, c.dead)
		}
	}
}

func fabricateSeason(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldID, leagueID uuid.UUID, clubs []uuid.UUID) uuid.UUID {
	t.Helper()
	var seasonID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.seasons
			(world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, '2029/30', 1, '2029-08-01', 'in_progress') RETURNING id`,
		worldID, leagueID).Scan(&seasonID); err != nil {
		t.Fatalf("insert season: %v", err)
	}
	for _, club := range clubs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.competition_entries (season_id, club_id) VALUES ($1, $2)`,
			seasonID, club); err != nil {
			t.Fatalf("insert entry: %v", err)
		}
	}
	return seasonID
}

// genClubs persists n brand-new AI clubs with deterministic squads.
func genClubs(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin clubs: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	generator, natPool, err := bootstrap.LoadPools(ctx, tx)
	if err != nil {
		t.Fatalf("load pools: %v", err)
	}
	out := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		factory := playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(int64(900+i)))).
			WithRegistry(playergen.NewNameRegistry())
		club, err := bootstrap.GenerateAIClub(ctx, tx, worldID, fmt.Sprintf("Fabricated FC %d", i+1), "", "england", factory)
		if err != nil {
			t.Fatalf("generate club %d: %v", i, err)
		}
		out = append(out, club.ClubID)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit clubs: %v", err)
	}
	return out
}