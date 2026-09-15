//go:build integration

package social_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfertest"
)

// newSocialFixture provisions the S06-01 world (one human manager + two AI
// managed clubs) and repurposes the AI-one club's manager row as a second
// in-world profile target.
func newSocialFixture(t *testing.T, name string) (*social.Service, *pgxpool.Pool, transfertest.World) {
	t.Helper()
	pool := testdb.New(t)
	tw := transfertest.Provision(t, pool, name, name+"-owner@example.com")
	return social.NewService(pool, nil), pool, tw
}

// attachPerson gives a manager a deterministic display name.
func attachPerson(t *testing.T, pool *pgxpool.Pool, worldID, managerID uuid.UUID, first, last, display string) {
	t.Helper()
	ctx := context.Background()
	var personID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO person.people (world_id, first_name, last_name, display_name, date_of_birth, nationality_code)
		VALUES ($1, $2, $3, $4, '1980-01-01', 'eng') RETURNING id`,
		worldID, first, last, display).Scan(&personID); err != nil {
		t.Fatalf("insert person: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE manager.managers SET person_id = $2 WHERE id = $1`, managerID, personID); err != nil {
		t.Fatalf("attach person: %v", err)
	}
}

// seedClubHistory pins one employment history row so career/trophy queries
// have a deterministic attribution window.
func seedClubHistory(t *testing.T, pool *pgxpool.Pool, worldID, managerID, clubID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var historyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO manager.manager_history (manager_id, world_id, club_id, role, start_date, end_date)
		VALUES ($1, $2, $3, 'manager', '2020-01-01', NULL) RETURNING id`,
		managerID, worldID, clubID).Scan(&historyID); err != nil {
		t.Fatalf("insert club history: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM manager.manager_history WHERE id = $1`, historyID)
	})
}

// seedCompletedFixture inserts one completed league fixture between the two
// clubs with a scoreline and lands it inside the attribution window.
func seedCompletedFixture(t *testing.T, pool *pgxpool.Pool, worldID, homeClub, awayClub uuid.UUID, home, away int) {
	t.Helper()
	ctx := context.Background()
	var competitionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Social Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&competitionID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status, ht_score, at_score, completed_at)
		VALUES ($1, $2, $3, $4, 1, now() - interval '2 days', 'completed', $5, $6, now() - interval '2 days')`,
		worldID, competitionID, homeClub, awayClub, home, away); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
}

// seedTrophy writes one club.club_history trophy for the club.
func seedTrophy(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID, season int, description string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO club.club_history (club_id, season, event_type, description)
		VALUES ($1, $2, 'trophy', $3)`, clubID, season, description); err != nil {
		t.Fatalf("insert trophy: %v", err)
	}
}

// seedTrust appends trust events for a manager.
func seedTrust(t *testing.T, pool *pgxpool.Pool, managerID uuid.UUID, deltas ...int) {
	t.Helper()
	for i, d := range deltas {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO social.trust_events (manager_id, delta, reason)
			VALUES ($1, $2, $3)`, managerID, d, "test-"+time.Now().Format("150405")+string(rune('a'+i))); err != nil {
			t.Fatalf("insert trust event: %v", err)
		}
	}
}

// seedRivalry writes one manager↔manager and one club↔club rivalry edge so the
// profile's rivals tab has both kinds (S06-04c shape).
func seedRivalry(t *testing.T, pool *pgxpool.Pool, worldID, mgrA, clubA, mgrB, clubB uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type, relationship_type, strength, trust, sentiment, last_interaction_at)
		VALUES ($1, $2, 'manager', $3, 'manager', 'rivalry', 30, 5, -10, now())`,
		worldID, mgrA, mgrB); err != nil {
		t.Fatalf("insert manager rivalry: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type, relationship_type, strength, trust, sentiment, last_interaction_at)
		VALUES ($1, $2, 'club', $3, 'club', 'rivalry', 25, 0, 0, now())`,
		worldID, clubA, clubB); err != nil {
		t.Fatalf("insert club rivalry: %v", err)
	}
}

// seedHumanProfile assembles the shared fixtures: person, history, two
// completed fixtures, a trophy, trust deltas and rivalry edges for the human
// manager.
func seedHumanProfile(t *testing.T, pool *pgxpool.Pool, w transfertest.World) {
	t.Helper()
	attachPerson(t, pool, w.WorldID, w.HumanMgr, "Ada", "Lovelace", "Ada Lovelace")
	seedClubHistory(t, pool, w.WorldID, w.HumanMgr, w.HumanClub)
	seedClubHistory(t, pool, w.WorldID, w.AIOneMgr, w.AIOneClub)
	// Viewer (Human) beats AI-one 2-1 at home, then loses 0-3 away.
	seedCompletedFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub, 2, 1)
	seedCompletedFixture(t, pool, w.WorldID, w.AIOneClub, w.HumanClub, 3, 0)
	seedTrophy(t, pool, w.HumanClub, 2025, "Touchline League Champions")
	seedTrust(t, pool, w.HumanMgr, +15, -5)
	seedRivalry(t, pool, w.WorldID, w.HumanMgr, w.HumanClub, w.AIOneMgr, w.AIOneClub)
}

func TestGetManagerProfileFull(t *testing.T) {
	svc, pool, w := newSocialFixture(t, "profile-full")
	seedHumanProfile(t, pool, w)
	ctx := context.Background()

	p, err := svc.GetManagerProfile(ctx, w.WorldID, w.HumanMgr, w.HumanMgr)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}

	if p.ID != w.HumanMgr {
		t.Errorf("profile id = %v", p.ID)
	}
	if p.Name != "Ada Lovelace" {
		t.Errorf("name = %q, want Ada Lovelace", p.Name)
	}
	if p.Status != "active" {
		t.Errorf("status = %q, want active", p.Status)
	}
	if p.IsPolicyBot {
		t.Error("human manager flagged as policy bot")
	}
	if p.ActiveClub == nil || p.ActiveClub.ID != w.HumanClub {
		t.Fatalf("active club = %+v, want %v", p.ActiveClub, w.HumanClub)
	}

	// Two completed fixtures for the human club: 1W 1L, 2 scored / 4 conceded.
	want := struct {
		matches, wins, draws, losses, gf, ga int
	}{2, 1, 0, 1, 2, 4}
	if p.Career.Matches != want.matches || p.Career.Wins != want.wins ||
		p.Career.Draws != want.draws || p.Career.Losses != want.losses ||
		p.Career.GoalsFor != want.gf || p.Career.GoalsAgainst != want.ga {
		t.Errorf("career = %+v, want %+v", p.Career, want)
	}

	if len(p.Trophies) != 1 {
		t.Fatalf("trophies = %+v, want 1", p.Trophies)
	}
	if p.Trophies[0].ClubName != p.ActiveClub.Name || p.Trophies[0].Season != 2025 {
		t.Errorf("trophy = %+v", p.Trophies[0])
	}

	if p.TrustScore != 10 {
		t.Errorf("trust = %d, want 10", p.TrustScore)
	}

	// Self-view: no h2h record.
	if p.H2HVsViewer != nil {
		t.Errorf("self h2h = %+v, want nil", p.H2HVsViewer)
	}

	// Rivals: personal manager edge (strength 30) then the club edge (25).
	if len(p.Rivalries) != 2 {
		t.Fatalf("rivalries = %+v, want 2 edges", p.Rivalries)
	}
	if p.Rivalries[0].RelationshipType != "rivalry" || p.Rivalries[0].Strength != 30 || p.Rivalries[0].EntityType != "manager" {
		t.Errorf("first rival = %+v, want manager rivalry 30", p.Rivalries[0])
	}
	if p.Rivalries[1].EntityType != "club" || p.Rivalries[1].Strength != 25 {
		t.Errorf("second rival = %+v, want club rivalry 25", p.Rivalries[1])
	}
}

func TestGetManagerProfileH2HVsOther(t *testing.T) {
	svc, pool, w := newSocialFixture(t, "profile-h2h")
	seedHumanProfile(t, pool, w)
	ctx := context.Background()

	p, err := svc.GetManagerProfile(ctx, w.WorldID, w.HumanMgr, w.AIOneMgr)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}

	// AI target has no person row -> generic label + policy-bot flag.
	if p.Name != "AI Manager" {
		t.Errorf("AI name = %q, want AI Manager", p.Name)
	}
	if !p.IsPolicyBot {
		t.Error("AI manager not flagged as policy bot")
	}
	if p.ActiveClub == nil || p.ActiveClub.ID != w.AIOneClub {
		t.Fatalf("AI active club = %+v, want %v", p.ActiveClub, w.AIOneClub)
	}

	// Head-to-head FROM the viewer (human): won the home leg 2-1, lost 0-3 away.
	if p.H2HVsViewer == nil {
		t.Fatal("h2h missing")
	}
	if p.H2HVsViewer.Matches != 2 || p.H2HVsViewer.Wins != 1 || p.H2HVsViewer.Draws != 0 ||
		p.H2HVsViewer.Losses != 1 || p.H2HVsViewer.GoalsFor != 2 || p.H2HVsViewer.GoalsAgainst != 4 {
		t.Errorf("h2h = %+v, want 2 / 1 / 0 / 1 / 2 / 4", p.H2HVsViewer)
	}

	// The target's club edges still resolve their club rivals.
	found := false
	for _, r := range p.Rivalries {
		if r.EntityType == "club" && r.Strength == 25 {
			found = true
		}
	}
	if !found {
		t.Errorf("AI profile missing club rival edge: %+v", p.Rivalries)
	}
}

func TestGetManagerProfileWorldBoundary(t *testing.T) {
	svc, pool, w := newSocialFixture(t, "profile-boundary")
	seedHumanProfile(t, pool, w)
	ctx := context.Background()

	// A foreign world id reject the same manager.
	otherWorld := testdb.CreateWorld(t, pool, "W-OTHER")
	if _, err := svc.GetManagerProfile(ctx, otherWorld, w.HumanMgr, w.HumanMgr); !errors.Is(err, social.ErrManagerNotInWorld) {
		t.Fatalf("cross-world err = %v, want ErrManagerNotInWorld", err)
	}

	// A manager that does not exist.
	if _, err := svc.GetManagerProfile(ctx, w.WorldID, w.HumanMgr, uuid.New()); !errors.Is(err, social.ErrManagerNotFound) {
		t.Fatalf("unknown err = %v, want ErrManagerNotFound", err)
	}
}

func TestGetManagerProfileNoHeadToHeadWhenClubsNeverMet(t *testing.T) {
	svc, pool, w := newSocialFixture(t, "profile-no-h2h")
	attachPerson(t, pool, w.WorldID, w.HumanMgr, "Ada", "Lovelace", "Ada Lovelace")
	ctx := context.Background()

	p, err := svc.GetManagerProfile(ctx, w.WorldID, w.HumanMgr, w.AIOneMgr)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if p.H2HVsViewer != nil {
		t.Errorf("h2h = %+v, want nil (never met)", p.H2HVsViewer)
	}
	if p.Career.Matches != 0 {
		t.Errorf("career matches = %d, want 0", p.Career.Matches)
	}
}
