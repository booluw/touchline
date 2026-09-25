//go:build integration

package competition

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestRegionalCupIntegrationLifecycle drives the IM08 service entry points end
// to end for a first campaign: create the regional cup, preview its field, let
// the engine agree, start the campaign, read the cup back, and hit the error
// paths (double campaign, out-of-region bands, re-band).
func TestRegionalCupIntegrationLifecycle(t *testing.T) {
	pool, worldID, englandID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	spain, err := svc.CreateCountry(ctx, worldID, "esp", "Spain")
	if err != nil {
		t.Fatalf("create spain: %v", err)
	}
	spainID := spain.ID
	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	for _, c := range []uuid.UUID{englandID, spainID} {
		if _, err := svc.SetCountryRegion(ctx, c, &region.ID); err != nil {
			t.Fatalf("assign country %s: %v", c, err)
		}
	}

	engPremier, engChamp := twoTierLeague(t, svc, englandID)
	spaPremier, spaChamp := twoTierLeague(t, svc, spainID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, engPremier, engChamp, spaPremier, spaChamp)

	bands := []QualBandInput{
		{LeagueID: engPremier.ID, From: 1, To: newInt(2)},
		{LeagueID: spaPremier.ID, From: 1, To: newInt(2)},
	}
	cup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, RegionID: region.ID, Tier: newInt(1),
		Name: "Europe Cup", Qualification: bands,
	})
	if err != nil {
		t.Fatalf("create regional cup: %v", err)
	}
	if cup.Region == nil || cup.Region.ID != region.ID {
		t.Fatalf("cup region = %+v, want Europe", cup.Region)
	}
	if cup.Tier == nil || *cup.Tier != 1 {
		t.Fatalf("cup tier = %+v, want 1", cup.Tier)
	}
	if cup.Country != nil {
		t.Fatalf("regional cup must expose a nil country, got %+v", cup.Country)
	}

	preview, err := svc.PreviewCupField(ctx, CupPreviewParams{
		WorldID: worldID, RegionID: region.ID, CupID: &cup.ID, Qualification: bands,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.FieldSize != 4 {
		t.Fatalf("field size = %d, want 4 (eng 1..2 + spa 1..2, first campaign has no champion)", preview.FieldSize)
	}
	if preview.Champion != nil {
		t.Fatalf("first campaign has no reigning champion: %+v", preview.Champion)
	}
	if len(preview.Warnings) != 0 {
		t.Fatalf("preview warnings = %v, want none", preview.Warnings)
	}
	for _, e := range preview.Field {
		if e.Origin != OriginPosition {
			t.Fatalf("entrant %s origin = %s, want position", e.ClubID, e.Origin)
		}
	}

	// The standalone engine and the preview must agree on the field.
	field, err := svc.ComputeCupField(ctx, cup.ID)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != preview.FieldSize {
		t.Fatalf("engine club count = %d, preview = %d", field.ClubCount, preview.FieldSize)
	}

	res, err := svc.StartRegionalCupCampaign(ctx, worldID, cup.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Season == nil || res.Season.Status != "in_progress" {
		t.Fatalf("season = %+v, want in_progress", res.Season)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", res.Warnings)
	}

	// Round 1 of the bracket must have been materialized for all 4 clubs.
	r1, err := svc.GetFixtures(ctx, cup.ID, worldID, newInt(1))
	if err != nil {
		t.Fatalf("round 1 fixtures: %v", err)
	}
	if len(r1) != 2 {
		t.Fatalf("round 1 = %d fixtures, want 2 (4 clubs)", len(r1))
	}

	// Every entrant is a cup member.
	var members int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'cup'`, cup.ID).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 4 {
		t.Fatalf("memberships = %d, want 4", members)
	}

	// Reads carry both cup kinds and the new region fields.
	cups, err := svc.ListCups(ctx, worldID)
	if err != nil {
		t.Fatalf("list cups: %v", err)
	}
	var listed *Cup
	for i := range cups {
		if cups[i].ID == cup.ID {
			listed = &cups[i]
		}
	}
	if listed == nil || listed.Region == nil || listed.Tier == nil {
		t.Fatalf("regional cup missing from ListCups: %+v", listed)
	}
	cc, err := svc.GetCup(ctx, worldID, cup.ID)
	if err != nil {
		t.Fatalf("get cup: %v", err)
	}
	if cc.Cup.Region == nil || cc.Cup.Region.ID != region.ID {
		t.Fatalf("get cup region = %+v, want Europe", cc.Cup.Region)
	}

	// A second campaign while the first is live is a hard error.
	if _, err := svc.StartRegionalCupCampaign(ctx, worldID, cup.ID); !errors.Is(err, ErrCupCampaignExists) {
		t.Fatalf("second start err = %v, want ErrCupCampaignExists", err)
	}

	// Spain leaves the region: its league's band is out-of-scope for both a
	// new creation and re-banding the existing cup.
	if _, err := svc.SetCountryRegion(ctx, spainID, nil); err != nil {
		t.Fatalf("unassign spain: %v", err)
	}
	if _, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, RegionID: region.ID, Name: "Balkan Cup",
		Qualification: []QualBandInput{{LeagueID: spaPremier.ID, From: 1, To: newInt(1)}},
	}); !errors.Is(err, ErrRegionMismatch) {
		t.Fatalf("out-of-region band err = %v, want ErrRegionMismatch", err)
	}
	if _, err := svc.SetQualification(ctx, cup.ID, []QualBandInput{
		{LeagueID: spaPremier.ID, From: 1, To: newInt(1)},
	}); !errors.Is(err, ErrRegionMismatch) {
		t.Fatalf("set qual out-of-region err = %v, want ErrRegionMismatch", err)
	}
	if _, err := svc.SetQualification(ctx, cup.ID, []QualBandInput{
		{LeagueID: engPremier.ID, From: 1, To: newInt(1)},
	}); err != nil {
		t.Fatalf("set qual valid: %v", err)
	}
}

// TestRegionalCupIntegrationChampionDraft proves the re-preview: once a cup
// exists with a reigning champion, the +1 slot shows up in the draft preview
// and the standalone engine agrees — while still writing nothing.
func TestRegionalCupIntegrationChampionDraft(t *testing.T) {
	pool, worldID, englandID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, englandID, &region.ID); err != nil {
		t.Fatalf("assign country: %v", err)
	}
	engPremier, engChamp := twoTierLeague(t, svc, englandID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, engPremier, engChamp)

	engTable, err := svc.LastCompletedStandings(ctx, engPremier.ID)
	if err != nil {
		t.Fatalf("eng table: %v", err)
	}

	bands := []QualBandInput{{LeagueID: engPremier.ID, From: 1, To: newInt(2)}}
	cup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, RegionID: region.ID, Name: "Europe Cup", Qualification: bands,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// The champion from the completed previous edition finished 4th (out of
	// the 1..2 band) and is still entitled to defend.
	declareReigningChampion(t, pool, svc, worldID, cup.ID, engTable.Ranks[3])

	preview, err := svc.PreviewCupField(ctx, CupPreviewParams{
		WorldID: worldID, RegionID: region.ID, CupID: &cup.ID, Qualification: bands,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.FieldSize != 3 {
		t.Fatalf("field size = %d, want 3 (eng 1..2 + defending champion)", preview.FieldSize)
	}
	if preview.Champion == nil || preview.Champion.ClubID != engTable.Ranks[3] {
		t.Fatalf("champion = %+v, want %s", preview.Champion, engTable.Ranks[3])
	}
	origins := map[string]int{}
	for _, e := range preview.Field {
		origins[e.Origin]++
	}
	if origins[OriginPosition] != 2 || origins[OriginChampionOutOfBand] != 1 {
		t.Fatalf("origins = %v, want 2x position + 1x champion_out_of_band", origins)
	}

	field, err := svc.ComputeCupField(ctx, cup.ID)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != preview.FieldSize {
		t.Fatalf("engine club count = %d, preview = %d", field.ClubCount, preview.FieldSize)
	}
}

// TestRegionalCupIntegrationUnavailableLeague starts a campaign whose banded
// league has no completed season: the preview warns, and the campaign refuses
// to start (422-class) rather than entering a half field.
func TestRegionalCupIntegrationUnavailableLeague(t *testing.T) {
	pool, worldID, englandID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, englandID, &region.ID); err != nil {
		t.Fatalf("assign country: %v", err)
	}
	engPremier, engChamp := twoTierLeague(t, svc, englandID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, engPremier, engChamp)

	vacant, err := svc.CreateLeague(ctx, LeagueParams{CountryID: englandID, Name: "Vacant", Tier: 3, TeamCount: 4})
	if err != nil {
		t.Fatalf("create vacant league: %v", err)
	}

	bands := []QualBandInput{
		{LeagueID: engPremier.ID, From: 1, To: newInt(1)},
		{LeagueID: vacant.ID, From: 1, To: newInt(1)},
	}
	cup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, RegionID: region.ID, Name: "UEFA 322", Qualification: bands,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	preview, err := svc.PreviewCupField(ctx, CupPreviewParams{
		WorldID: worldID, RegionID: region.ID, CupID: &cup.ID, Qualification: bands,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.FieldSize != 1 {
		t.Fatalf("field size = %d, want 1 (premier band only)", preview.FieldSize)
	}
	if len(preview.Warnings) == 0 {
		t.Fatal("preview must warn about the league with no completed season")
	}

	if _, err := svc.StartRegionalCupCampaign(ctx, worldID, cup.ID); !errors.Is(err, ErrQualificationUnavailable) {
		t.Fatalf("start err = %v, want ErrQualificationUnavailable", err)
	}
}

// TestRegionalCupIntegrationCapExemption proves the IM08 cap exemption: a club
// already entered in three other cups cannot be a positional entrant (the
// 3-cup membership cap applies), but a defending champion at the same cap is
// still entitled to its slot and enters.
func TestRegionalCupIntegrationCapExemption(t *testing.T) {
	pool, worldID, englandID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, englandID, &region.ID); err != nil {
		t.Fatalf("assign country: %v", err)
	}
	engPremier, engChamp := twoTierLeague(t, svc, englandID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, engPremier, engChamp)

	engTable, err := svc.LastCompletedStandings(ctx, engPremier.ID)
	if err != nil {
		t.Fatalf("eng table: %v", err)
	}
	champion := engTable.Ranks[3] // out of the 1..2 band -> champion_out_of_band

	// Three existing cup memberships: a positional entrant would hit the cap.
	for k := 0; k < 3; k++ {
		sib := createContinentalCup(t, pool, worldID, region.ID, "Other", 1)
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
			VALUES ($1, $2, $3, 'cup')`, worldID, champion, sib); err != nil {
			t.Fatalf("seat %d for champion: %v", k, err)
		}
	}

	bands := []QualBandInput{{LeagueID: engPremier.ID, From: 1, To: newInt(2)}}
	cup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, RegionID: region.ID, Name: "Europe Cup", Qualification: bands,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	declareReigningChampion(t, pool, svc, worldID, cup.ID, champion)

	if _, err := svc.StartRegionalCupCampaign(ctx, worldID, cup.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	var members int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'cup'`, cup.ID).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != 3 {
		t.Fatalf("memberships = %d, want 3 (eng 1..2 + champion despite the cap)", members)
	}
	var champIn bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM competition.club_competitions
			WHERE competition_id = $1 AND club_id = $2 AND role = 'cup')`,
		cup.ID, champion).Scan(&champIn); err != nil {
		t.Fatalf("champion membership: %v", err)
	}
	if !champIn {
		t.Fatal("champion must enter Europe Cup despite holding 3 other cup memberships")
	}
}
