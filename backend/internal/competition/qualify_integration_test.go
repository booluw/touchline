//go:build integration

package competition

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createContinentalCup inserts a continental cup straight into the schema.
// IM08 will introduce the service entry point that does this; until then SQL
// is the honest seam the qualification engine reads from, and exactly what
// an IM08 campaign start would have produced.
func createContinentalCup(t *testing.T, pool *pgxpool.Pool, worldID, regionID uuid.UUID, name string, tier int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var cupID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions
			(world_id, region_id, name, competition_type, tier, team_count, status)
		VALUES ($1, $2, $3, 'continental', $4, 16, 'active')
		RETURNING id`, worldID, regionID, name, tier).Scan(&cupID); err != nil {
		t.Fatalf("insert continental cup %s: %v", name, err)
	}
	return cupID
}

// addQualBand writes one cup_qualification row.
func addQualBand(t *testing.T, pool *pgxpool.Pool, cupID, leagueID uuid.UUID, from int, to *int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO competition.cup_qualification (cup_id, league_id, from_position, to_position)
		VALUES ($1, $2, $3, $4)`, cupID, leagueID, from, to); err != nil {
		t.Fatalf("insert band %v for %s: %v", from, cupID, err)
	}
}

// playCompleteSeason starts both leagues, plays every fixture 2-1, then marks
// the seasons completed — the state a league has when qualification runs.
func playCompleteSeason(t *testing.T, pool *pgxpool.Pool, svc *Service, worldID uuid.UUID, leagues ...*League) {
	t.Helper()
	ctx := context.Background()
	for _, l := range leagues {
		if _, err := svc.StartSeason(ctx, worldID, l.ID); err != nil {
			t.Fatalf("start season %s: %v", l.Name, err)
		}
	}
	played := map[uuid.UUID]bool{}
	for round := 0; round < 6; round++ {
		for _, l := range leagues {
			fixtures, err := svc.GetFixtures(ctx, l.ID, worldID, newInt(round+1))
			if err != nil {
				t.Fatalf("matchday %d fixtures of %s: %v", round+1, l.Name, err)
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
	for _, l := range leagues {
		if _, err := pool.Exec(ctx,
			`UPDATE competition.seasons SET status = 'completed' WHERE competition_id = $1`, l.ID); err != nil {
			t.Fatalf("complete season of %s: %v", l.Name, err)
		}
	}
}

// declareReigningChampion back-dates a completed cup campaign with a champion.
func declareReigningChampion(t *testing.T, pool *pgxpool.Pool, svc *Service, worldID, cupID, clubID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var seasonID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.seasons (world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, '1', 1, now()::date, 'completed')
		RETURNING id`, worldID, cupID).Scan(&seasonID); err != nil {
		t.Fatalf("insert cup season: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.competition_entries (season_id, club_id, status)
		VALUES ($1, $2, 'champion')`, seasonID, clubID); err != nil {
		t.Fatalf("insert champion entry: %v", err)
	}
}

func TestQualifyIntegrationComputeCupField(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, premier, champ)

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, countryID, &region.ID); err != nil {
		t.Fatalf("assign country: %v", err)
	}

	cup := createContinentalCup(t, pool, worldID, region.ID, "Europe Cup", 1)
	addQualBand(t, pool, cup, premier.ID, 1, newInt(2))
	addQualBand(t, pool, cup, champ.ID, 1, newInt(1))

	// Two runs of the same input must yield identical fields.
	a, err := svc.ComputeCupField(ctx, cup)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	b, err := svc.ComputeCupField(ctx, cup)
	if err != nil {
		t.Fatalf("compute again: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic field: %+v vs %+v", a, b)
	}
	if a.ClubCount != 3 {
		t.Fatalf("club count = %d, want 3 (premier ranks 1..2 + champ rank 1)", a.ClubCount)
	}
	for _, e := range a.Entrants {
		if e.Origin != OriginPosition {
			t.Fatalf("entrant %s origin = %s, want position", e.ClubID, e.Origin)
		}
	}
	if len(a.Conflicts) != 0 || len(a.Unavailable) != 0 {
		t.Fatalf("no siblings/unavailable expected: %+v / %+v", a.Conflicts, a.Unavailable)
	}

	// The same field via the explicit engine call with this service as the
	// provider must agree with the convenience wrapper.
	explicit, err := ComputeField(ctx, QualifyField{Cup: a.Cup, Bands: aCupBands(t, pool, cup)},
		svc, svc, svc)
	if err != nil {
		t.Fatalf("explicit compute: %v", err)
	}
	if !reflect.DeepEqual(a.Entrants, explicit.Entrants) {
		t.Fatalf("wrappers diverge: %+v vs %+v", a.Entrants, explicit.Entrants)
	}
}

func aCupBands(t *testing.T, pool *pgxpool.Pool, cupID uuid.UUID) []Band {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT league_id, from_position, to_position
		FROM competition.cup_qualification WHERE cup_id = $1
		ORDER BY league_id, from_position`, cupID)
	if err != nil {
		t.Fatalf("load bands: %v", err)
	}
	defer rows.Close()
	out := []Band{}
	for rows.Next() {
		var b Band
		if err := rows.Scan(&b.LeagueID, &b.From, &b.To); err != nil {
			t.Fatalf("scan band: %v", err)
		}
		out = append(out, b)
	}
	return out
}

func TestQualifyIntegrationUnavailableLeague(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, premier, champ)

	// A declared league with NO started/completed season — its band cannot be
	// satisfied, and that must surface as a list, not an error.
	vacant, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Vacant", Tier: 3, TeamCount: 4})
	if err != nil {
		t.Fatalf("create vacant league: %v", err)
	}

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	cup := createContinentalCup(t, pool, worldID, region.ID, "Europe Cup", 1)
	addQualBand(t, pool, cup, premier.ID, 1, newInt(2))
	addQualBand(t, pool, cup, vacant.ID, 1, newInt(1))

	field, err := svc.ComputeCupField(ctx, cup)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(field.Unavailable) != 1 || field.Unavailable[0].LeagueID != vacant.ID {
		t.Fatalf("unavailable = %+v, want exactly the vacant league", field.Unavailable)
	}
	if field.ClubCount != 2 {
		t.Fatalf("club count = %d, want 2 (premier band only)", field.ClubCount)
	}
}

func TestQualifyIntegrationChampion(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, premier, champ)

	premierTable, err := svc.LastCompletedStandings(ctx, premier.ID)
	if err != nil {
		t.Fatalf("premier table: %v", err)
	}
	champTable, err := svc.LastCompletedStandings(ctx, champ.ID)
	if err != nil {
		t.Fatalf("champ table: %v", err)
	}

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}

	// --- in-band champion: band 1..1 on its own league, +1 is next-best --
	cupInBand := createContinentalCup(t, pool, worldID, region.ID, "Europe Cup In-Band", 1)
	addQualBand(t, pool, cupInBand, premier.ID, 1, newInt(1))
	declareReigningChampion(t, pool, svc, worldID, cupInBand, premierTable.Ranks[0])

	field, err := svc.ComputeCupField(ctx, cupInBand)
	if err != nil {
		t.Fatalf("compute in-band: %v", err)
	}
	if field.ClubCount != 2 {
		t.Fatalf("in-band club count = %d, want 2 (champion on its rank + next-best)", field.ClubCount)
	}
	if field.Entrants[0].ClubID != premierTable.Ranks[0] || field.Entrants[0].Origin != OriginPosition {
		t.Fatalf("in-band leader = %+v, want champion on position", field.Entrants[0])
	}
	if field.Entrants[1].Origin != OriginChampionNextBest {
		t.Fatalf("in-band second = %+v, want next-best", field.Entrants[1])
	}

	// --- out-of-band champion: finished below band 1..1 on a 4-team league --
	cupOut := createContinentalCup(t, pool, worldID, region.ID, "Europe Cup Out", 1)
	addQualBand(t, pool, cupOut, premier.ID, 1, newInt(1))
	declareReigningChampion(t, pool, svc, worldID, cupOut, premierTable.Ranks[3])

	field, err = svc.ComputeCupField(ctx, cupOut)
	if err != nil {
		t.Fatalf("compute out-of-band: %v", err)
	}
	if field.ClubCount != 2 {
		t.Fatalf("out-of-band club count = %d, want 2 (band 1..1 + champion)", field.ClubCount)
	}
	last := field.Entrants[field.ClubCount-1]
	if last.ClubID != premierTable.Ranks[3] || last.Origin != OriginChampionOutOfBand {
		t.Fatalf("out-of-band champion = %+v", last)
	}

	// --- direct champion: its league (champ tier) has NO band row at all ---
	cupDirect := createContinentalCup(t, pool, worldID, region.ID, "Europe Cup Direct", 1)
	addQualBand(t, pool, cupDirect, premier.ID, 1, newInt(2))
	declareReigningChampion(t, pool, svc, worldID, cupDirect, champTable.Ranks[0])

	field, err = svc.ComputeCupField(ctx, cupDirect)
	if err != nil {
		t.Fatalf("compute direct: %v", err)
	}
	if field.ClubCount != 3 {
		t.Fatalf("direct club count = %d, want 3 (premier 1..2 + champion_direct)", field.ClubCount)
	}
	var direct *Entrant
	for i := range field.Entrants {
		if field.Entrants[i].Origin == OriginChampionDirect {
			direct = &field.Entrants[i]
		}
	}
	if direct == nil || direct.ClubID != champTable.Ranks[0] {
		t.Fatalf("direct champion = %+v, want %s", direct, champTable.Ranks[0])
	}
}

func TestQualifyIntegrationConflicts(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, premier, champ)

	premierTable, err := svc.LastCompletedStandings(ctx, premier.ID)
	if err != nil {
		t.Fatalf("premier table: %v", err)
	}

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}

	main := createContinentalCup(t, pool, worldID, region.ID, "Europe Premier", 1)
	sibling := createContinentalCup(t, pool, worldID, region.ID, "Europe Super", 2)
	addQualBand(t, pool, main, premier.ID, 1, newInt(2))
	addQualBand(t, pool, sibling, premier.ID, 1, newInt(1))

	field, err := svc.ComputeCupField(ctx, main)
	if err != nil {
		t.Fatalf("compute main: %v", err)
	}
	// The premier leader is drawn by both cups.
	conf, ok := field.Conflicts[premierTable.Ranks[0]]
	if !ok {
		t.Fatalf("leader must conflict with the sibling: %+v", field.Conflicts)
	}
	if len(conf) != 1 || conf[0].OtherCupID != sibling || conf[0].Tier != 2 {
		t.Fatalf("conflicts of leader = %+v, want the sibling cup (tier 2)", conf)
	}

	// The sibling's own field flags the main cup back.
	sibField, err := svc.ComputeCupField(ctx, sibling)
	if err != nil {
		t.Fatalf("compute sibling: %v", err)
	}
	sibConf, ok := sibField.Conflicts[premierTable.Ranks[0]]
	if !ok || len(sibConf) != 1 || sibConf[0].OtherCupID != main || sibConf[0].Tier != 1 {
		t.Fatalf("sibling conflicts = %+v, want the main cup (tier 1)", sibConf)
	}
}

func TestQualifyIntegrationErrors(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, premier, champ)

	// No bands → nothing can enter → hard qualification error (never a panic).
	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	bare := createContinentalCup(t, pool, worldID, region.ID, "Bare Cup", 1)
	if _, err := svc.ComputeCupField(ctx, bare); !errors.Is(err, ErrQualificationField) {
		t.Fatalf("bare cup err = %v, want ErrQualificationField", err)
	}

	// Unknown cup → ErrCompetitionNotFound.
	if _, err := svc.ComputeCupField(ctx, uuid.New()); !errors.Is(err, ErrCompetitionNotFound) {
		t.Fatalf("unknown cup err = %v, want ErrCompetitionNotFound", err)
	}
}
