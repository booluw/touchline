//go:build integration

package competition

import (
	"context"
	"reflect"
	"testing"
)

// TestCompetitionScreenReads drives a real seeded season through matchday 1
// and checks the competitions-screen reads: standings form/position (IM55),
// upcoming fixtures with difficulty (IM59) and the league outlook with a
// regional-cup attachment (IM56/IM57).
func TestCompetitionScreenReads(t *testing.T) {
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
	md1, err := svc.GetFixtures(ctx, premier.ID, worldID, newInt(1))
	if err != nil || len(md1) != 2 {
		t.Fatalf("matchday 1 fixtures = %d, err %v", len(md1), err)
	}
	winner := md1[0].HomeClub.ID
	if err := svc.ApplyResult(ctx, md1[0].ID, 2, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := svc.ApplyResult(ctx, md1[1].ID, 1, 1); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// IM55: positions and last-5 form.
	st, err := svc.GetStandings(ctx, premier.ID, worldID)
	if err != nil {
		t.Fatalf("standings: %v", err)
	}
	for i, r := range st.Rows {
		if r.Position != i+1 {
			t.Fatalf("row %d position = %d", i, r.Position)
		}
		if len(r.Form) != 1 {
			t.Fatalf("%s form = %v, want one result", r.Club.Name, r.Form)
		}
	}
	if st.Rows[0].Club.ID != winner || !reflect.DeepEqual(st.Rows[0].Form, []string{"W"}) {
		t.Fatalf("leader = %s form %v, want the winner with [W]", st.Rows[0].Club.Name, st.Rows[0].Form)
	}

	// IM59: next fixtures with difficulty, kickoff order, scheduled only.
	up, err := svc.UpcomingClubFixtures(ctx, worldID, winner, 2)
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(up) == 0 || len(up) > 2 {
		t.Fatalf("upcoming = %d, want 1..2", len(up))
	}
	for i, f := range up {
		if f.Status != "scheduled" && f.Status != "postponed" {
			t.Fatalf("upcoming %d status %s", i, f.Status)
		}
		if i > 0 && f.ScheduledAt.Before(up[i-1].ScheduledAt) {
			t.Fatal("upcoming not in kickoff order")
		}
		if f.Difficulty.Level < 1 || f.Difficulty.Level > 5 || len(f.Difficulty.Factors) != 3 {
			t.Fatalf("difficulty = %+v", f.Difficulty)
		}
	}

	// IM56: a regional cup fed by positions 1-2 shows up as an attachment.
	var cupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type)
		VALUES ($1, 'Regional Shield', 'regional') RETURNING id`, worldID).Scan(&cupID); err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.cup_qualification (cup_id, league_id, from_position, to_position)
		VALUES ($1, $2, 1, 2)`, cupID, premier.ID); err != nil {
		t.Fatalf("band: %v", err)
	}

	o, err := svc.ClubOutlook(ctx, worldID, premier.ID, winner)
	if err != nil {
		t.Fatalf("outlook: %v", err)
	}
	if o.Position != 1 || o.GamesLeft == 0 || o.NextMatch == nil {
		t.Fatalf("outlook position %d, games left %d, next %v", o.Position, o.GamesLeft, o.NextMatch)
	}
	kinds := []string{}
	for _, r := range o.Races {
		kinds = append(kinds, r.Kind)
	}
	// Premier: relegation only (no promotion out of the top tier).
	if !reflect.DeepEqual(kinds, []string{RaceTitle, RaceRelegation}) {
		t.Fatalf("races = %v, want title + relegation", kinds)
	}
	if len(o.Attachments) != 1 || o.Attachments[0].Cup.Scope != "regional" || !o.Attachments[0].Inside {
		t.Fatalf("attachments = %+v, want the regional cup band we sit in", o.Attachments)
	}
	if o.Attachments[0].Status == RaceClinched || o.Races[0].Status == RaceClinched {
		t.Fatal("nothing can be clinched after one matchday")
	}
	if o.Stakes == nil || o.Stakes.Kind != RaceTitle {
		t.Fatalf("stakes = %+v, want the title race", o.Stakes)
	}
	if o.Projection.Runs != projectionRuns || o.Projection.Range[0] > o.Projection.Position || o.Projection.Position > o.Projection.Range[1] {
		t.Fatalf("projection = %+v", o.Projection)
	}

	// Same matchday, same answer.
	again, err := svc.ClubOutlook(ctx, worldID, premier.ID, winner)
	if err != nil {
		t.Fatalf("outlook again: %v", err)
	}
	if again.Projection.Position != o.Projection.Position || again.Projection.Range != o.Projection.Range {
		t.Fatalf("projection not deterministic: %+v vs %+v", again.Projection, o.Projection)
	}
}
