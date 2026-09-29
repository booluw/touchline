//go:build integration

package competition

import (
	"context"
	"testing"

	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// TestAdminChangesRecordEvents: admin configuration writes land on the event
// spine in the same transaction as the change (IM27).
func TestAdminChangesRecordEvents(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "im27-admin-events")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	svc := NewService(pool, nil)

	country, err := svc.CreateCountry(ctx, w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	league, err := svc.CreateLeague(ctx, LeagueParams{CountryID: country.ID, Name: "Premier", Tier: 1, TeamCount: 4})
	if err != nil {
		t.Fatalf("create league: %v", err)
	}
	if _, err := svc.SetLeagueReputation(ctx, league.ID, 70); err != nil {
		t.Fatalf("set reputation: %v", err)
	}
	region, err := svc.CreateRegion(ctx, w.ID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, country.ID, &region.ID); err != nil {
		t.Fatalf("assign region: %v", err)
	}
	if _, err := svc.SetCountryRegion(ctx, country.ID, nil); err != nil {
		t.Fatalf("clear region: %v", err)
	}
	if err := svc.DeleteRegion(ctx, region.ID); err != nil {
		t.Fatalf("delete region: %v", err)
	}
	if err := worldSvc.SetConfig(ctx, w.ID, "tick.day_length", 60); err != nil {
		t.Fatalf("set config: %v", err)
	}

	for eventType, want := range map[string]int{
		EventCountryCreated:                   1,
		EventLeagueCreated:                    1,
		EventLeagueReputationSet:              1,
		EventRegionCreated:                    1,
		EventCountryRegionSet:                 2,
		EventRegionDeleted:                    1,
		internalworld.EventWorldConfigChanged: 1,
	} {
		var got int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`, w.ID, eventType).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", eventType, err)
		}
		if got != want {
			t.Errorf("%s events = %d, want %d", eventType, got, want)
		}
	}

	var key, value string
	if err := pool.QueryRow(ctx, `
		SELECT payload->>'key', payload->>'value' FROM world.events
		WHERE world_id = $1 AND event_type = $2`, w.ID, internalworld.EventWorldConfigChanged).Scan(&key, &value); err != nil {
		t.Fatalf("load config event: %v", err)
	}
	if key != "tick.day_length" || value != "60" {
		t.Errorf("config event payload = %s=%s, want tick.day_length=60", key, value)
	}
}
