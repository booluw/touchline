//go:build integration

package competition

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	internalworld "github.com/touchline/backend/internal/world"
)

// TestRegionsAndLeagueReputation covers the IM06 admin surface end to end:
// world-scoped region CRUD, country assignment with cross-world guards and
// SET-NULL cascade, and reputation writes that round-trip through every
// league read.
func TestRegionsAndLeagueReputation(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	// A second world proves isolation (region visibility, cross-world guards).
	w2, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "regions-it-2")
	if err != nil {
		t.Fatalf("create world 2: %v", err)
	}
	country2, err := svc.CreateCountry(ctx, w2.ID, "esp", "Spain")
	if err != nil {
		t.Fatalf("create country 2: %v", err)
	}

	// Create: success, exact duplicate rejected, same name elsewhere fine.
	west, err := svc.CreateRegion(ctx, worldID, "Western Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if west.WorldID != worldID {
		t.Fatalf("region world = %v, want world 1", west.WorldID)
	}
	if _, err := svc.CreateRegion(ctx, worldID, "Western Europe"); !errors.Is(err, ErrRegionNameCollision) {
		t.Fatalf("duplicate create err = %v, want ErrRegionNameCollision", err)
	}
	if _, err := svc.CreateRegion(ctx, w2.ID, "Western Europe"); err != nil {
		t.Fatalf("same name in another world must be allowed: %v", err)
	}
	if _, err := svc.CreateRegion(ctx, uuid.New(), "Nowhere"); !errors.Is(err, ErrWorldNotFound) {
		t.Fatalf("create in unknown world err = %v, want ErrWorldNotFound", err)
	}

	// List is world-scoped and name-ordered.
	north, err := svc.CreateRegion(ctx, worldID, "Northern Europe")
	if err != nil {
		t.Fatalf("create north: %v", err)
	}
	regions, err := svc.ListRegions(ctx, worldID)
	if err != nil {
		t.Fatalf("list regions: %v", err)
	}
	if len(regions) != 2 || regions[0].Name != "Northern Europe" || regions[1].Name != "Western Europe" {
		t.Fatalf("world-1 regions = %+v, want [Northern Europe, Western Europe]", regions)
	}
	regions2, err := svc.ListRegions(ctx, w2.ID)
	if err != nil {
		t.Fatalf("list world-2 regions: %v", err)
	}
	if len(regions2) != 1 || regions2[0].Name != "Western Europe" {
		t.Fatalf("world-2 regions = %+v, want just the one", regions2)
	}

	// Rename: success, collision, not-found.
	renamed, err := svc.RenameRegion(ctx, west.ID, "Iberia")
	if err != nil {
		t.Fatalf("rename region: %v", err)
	}
	if renamed.Name != "Iberia" {
		t.Fatalf("renamed to %q, want Iberia", renamed.Name)
	}
	if _, err := svc.RenameRegion(ctx, north.ID, "Iberia"); !errors.Is(err, ErrRegionNameCollision) {
		t.Fatalf("rename collision err = %v, want ErrRegionNameCollision", err)
	}
	if _, err := svc.RenameRegion(ctx, uuid.New(), "X"); !errors.Is(err, ErrRegionNotFound) {
		t.Fatalf("rename unknown err = %v, want ErrRegionNotFound", err)
	}

	// Assign country -> region; assignment surfaces on country reads.
	assigned, err := svc.SetCountryRegion(ctx, countryID, &west.ID)
	if err != nil {
		t.Fatalf("assign country region: %v", err)
	}
	if assigned.RegionID == nil || *assigned.RegionID != west.ID {
		t.Fatalf("assigned region = %v, want %v", assigned.RegionID, west.ID)
	}
	countries, err := svc.ListCountries(ctx, worldID)
	if err != nil {
		t.Fatalf("list countries: %v", err)
	}
	if len(countries) != 1 || countries[0].RegionID == nil || *countries[0].RegionID != west.ID {
		t.Fatalf("countries = %+v, want the assignment on the country", countries)
	}

	// Clear with a nil region_id.
	cleared, err := svc.SetCountryRegion(ctx, countryID, nil)
	if err != nil {
		t.Fatalf("clear country region: %v", err)
	}
	if cleared.RegionID != nil {
		t.Fatalf("cleared region = %v, want nil", cleared.RegionID)
	}

	// Guards: unknown country, unknown region, cross-world assignment.
	if _, err := svc.SetCountryRegion(ctx, uuid.New(), nil); !errors.Is(err, ErrCountryNotFound) {
		t.Fatalf("unknown country err = %v, want ErrCountryNotFound", err)
	}
	gone := uuid.New()
	if _, err := svc.SetCountryRegion(ctx, countryID, &gone); !errors.Is(err, ErrRegionNotFound) {
		t.Fatalf("unknown region err = %v, want ErrRegionNotFound", err)
	}
	if _, err := svc.SetCountryRegion(ctx, country2.ID, &west.ID); !errors.Is(err, ErrRegionWorldMismatch) {
		t.Fatalf("cross-world assign err = %v, want ErrRegionWorldMismatch", err)
	}
	// A rejected assignment leaves the country untouched.
	countries, _ = svc.ListCountries(ctx, worldID)
	if countries[0].RegionID != nil {
		t.Fatalf("failed assignment mutated country: %+v", countries[0])
	}

	// Delete nulls its countries' region_id (never deletes the country).
	if _, err := svc.SetCountryRegion(ctx, countryID, &north.ID); err != nil {
		t.Fatalf("re-assign north: %v", err)
	}
	if err := svc.DeleteRegion(ctx, north.ID); err != nil {
		t.Fatalf("delete region: %v", err)
	}
	countries, _ = svc.ListCountries(ctx, worldID)
	if countries[0].RegionID != nil {
		t.Fatalf("country region after region delete = %v, want nil", countries[0].RegionID)
	}
	if err := svc.DeleteRegion(ctx, north.ID); !errors.Is(err, ErrRegionNotFound) {
		t.Fatalf("double delete err = %v, want ErrRegionNotFound", err)
	}

	// League reputation: default 10, round-trips through every read, bounds.
	premier, champ := twoTierLeague(t, svc, countryID)
	updated, err := svc.SetLeagueReputation(ctx, premier.ID, 78)
	if err != nil {
		t.Fatalf("set reputation: %v", err)
	}
	if updated.Reputation != 78 {
		t.Fatalf("updated reputation = %d, want 78", updated.Reputation)
	}
	all, err := svc.ListLeagues(ctx, worldID)
	if err != nil {
		t.Fatalf("list leagues: %v", err)
	}
	repByID := map[uuid.UUID]int{}
	for _, l := range all {
		repByID[l.ID] = l.Reputation
	}
	if repByID[premier.ID] != 78 || repByID[champ.ID] != 10 {
		t.Fatalf("reputation map = %+v, want premier 78 and champ default 10", repByID)
	}
	byCountry, err := svc.ListCountryLeagues(ctx, countryID)
	if err != nil {
		t.Fatalf("list country leagues: %v", err)
	}
	for _, l := range byCountry {
		if l.ID == premier.ID && l.Reputation != 78 {
			t.Fatalf("country read reputation = %d, want 78", l.Reputation)
		}
	}
	got, err := svc.GetLeague(ctx, worldID, premier.ID)
	if err != nil {
		t.Fatalf("get league: %v", err)
	}
	if got.Reputation != 78 {
		t.Fatalf("get reputation = %d, want 78", got.Reputation)
	}

	// Bounds 0 and 100 are legal; outside is rejected without a write.
	if _, err := svc.SetLeagueReputation(ctx, premier.ID, 0); err != nil {
		t.Fatalf("reputation 0 must be accepted: %v", err)
	}
	if _, err := svc.SetLeagueReputation(ctx, premier.ID, 100); err != nil {
		t.Fatalf("reputation 100 must be accepted: %v", err)
	}
	for _, bad := range []int{-1, 101} {
		if _, err := svc.SetLeagueReputation(ctx, premier.ID, bad); !errors.Is(err, ErrReputationOutOfRange) {
			t.Fatalf("reputation %d err = %v, want ErrReputationOutOfRange", bad, err)
		}
	}
	got, _ = svc.GetLeague(ctx, worldID, premier.ID)
	if got.Reputation != 100 {
		t.Fatalf("reputation after rejected writes = %d, want 100", got.Reputation)
	}
	if _, err := svc.SetLeagueReputation(ctx, uuid.New(), 50); !errors.Is(err, ErrCompetitionNotFound) {
		t.Fatalf("reputation for unknown league err = %v, want ErrCompetitionNotFound", err)
	}
}
