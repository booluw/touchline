//go:build integration

// IM09's campaign sweep, end to end: a club that is double-qualified across
// continental cups has exactly one cup written at campaign start — tier
// precedence wins, a manager's recorded cup choice flips equal-tier conflicts,
// the cascade next-best replacement is cap-exempt, and every campaign start
// that touches a cascade publishes exactly one world news story. These suites
// need a live Postgres (TEST_DATABASE_URL) and only compile-check locally.

package competition

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
)

// sweepTestWorld seeds England + Spain (both in region Europe), a two-tier
// league in each, and a completed season everywhere — the shape IM07 needs
// before any cup field can be computed.
func sweepTestWorld(t *testing.T) (*pgxpool.Pool, *Service, uuid.UUID, *League, *League, *League, *League) {
	t.Helper()
	pool, worldID, englandID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	spain, err := svc.CreateCountry(ctx, worldID, "esp", "Spain")
	if err != nil {
		t.Fatalf("create spain: %v", err)
	}
	region, err := svc.CreateRegion(ctx, worldID, "Europe")
	if err != nil {
		t.Fatalf("create region: %v", err)
	}
	for _, c := range []uuid.UUID{englandID, spain.ID} {
		if _, err := svc.SetCountryRegion(ctx, c, &region.ID); err != nil {
			t.Fatalf("assign country %s to region: %v", c, err)
		}
	}
	engPremier, engChamp := twoTierLeague(t, svc, englandID)
	spaPremier, spaChamp := twoTierLeague(t, svc, spain.ID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed world: %v", err)
	}
	playCompleteSeason(t, pool, svc, worldID, engPremier, engChamp, spaPremier, spaChamp)
	return pool, svc, worldID, engPremier, engChamp, spaPremier, spaChamp
}

func cupMembershipRows(t *testing.T, pool *pgxpool.Pool, cupID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT club_id FROM competition.club_competitions
		 WHERE competition_id = $1 AND role = 'cup' ORDER BY club_id`, cupID)
	if err != nil {
		t.Fatalf("cup members of %s: %v", cupID, err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var clubID uuid.UUID
		if err := rows.Scan(&clubID); err != nil {
			t.Fatalf("scan cup member: %v", err)
		}
		out = append(out, clubID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("cup members rows: %v", err)
	}
	return out
}

func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func cupName(t *testing.T, pool *pgxpool.Pool, cupID uuid.UUID) string {
	t.Helper()
	var name string
	if err := pool.QueryRow(context.Background(),
		`SELECT name FROM competition.competitions WHERE id = $1`, cupID).Scan(&name); err != nil {
		t.Fatalf("cup name %s: %v", cupID, err)
	}
	return name
}

func clubName(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) string {
	t.Helper()
	var name string
	if err := pool.QueryRow(context.Background(),
		`SELECT name FROM club.clubs WHERE id = $1`, clubID).Scan(&name); err != nil {
		t.Fatalf("club name %s: %v", clubID, err)
	}
	return name
}

func campaignEventID(t *testing.T, pool *pgxpool.Pool, cupID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		SELECT id FROM world.events
		WHERE event_type = 'CUP_CAMPAIGN_STARTED'
		  AND payload->>'competition_id' = $1
		ORDER BY occurred_at DESC LIMIT 1`, cupID).Scan(&id)
	if err != nil {
		t.Fatalf("campaign event of cup %s: %v", cupID, err)
	}
	return id
}

// cascadeStory returns the one cascade news story tied to a campaign start
// event, failing if there is not exactly one.
func cascadeStory(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) (headline, body string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM world.news_stories
		WHERE related_event_id = $1 AND category = 'general'`, eventID).Scan(&count); err != nil {
		t.Fatalf("count cascade stories: %v", err)
	}
	if count != 1 {
		t.Fatalf("cascade stories = %d for event %s, want exactly 1", count, eventID)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT headline, body FROM world.news_stories
		WHERE related_event_id = $1 AND category = 'general'`, eventID).Scan(&headline, &body); err != nil {
		t.Fatalf("read cascade story: %v", err)
	}
	return headline, body
}

func planHasOrigin(t *testing.T, pool *pgxpool.Pool, cupID, clubID uuid.UUID, origin string) bool {
	t.Helper()
	var ok bool
	err := pool.QueryRow(context.Background(), `
		SELECT qualification_rules -> 'campaign' -> 'entrants' @>
			jsonb_build_array(jsonb_build_object('club_id', $1::text, 'origin', $2))
		FROM competition.competition_rules WHERE competition_id = $3`, clubID, origin, cupID).Scan(&ok)
	if err != nil {
		t.Fatalf("plan origin check: %v", err)
	}
	return ok
}

// TestRegionalCupIntegrationSweepTierProves the tier-precedence resolution at a
// campaign start. The club X is simultaneously the out-of-band defending
// champion of a tier-3 cup AND an in-band entrant of a tier-1 cup. Starting the
// lower-tier cup must NOT draft X; X stays entitled to the higher-tier cup.
// The lower cup keeps its remaining slots (England's league is exhausted, so no
// next-best replacement exists) and exactly one cascade story is published.
func TestRegionalCupIntegrationSweepTierProves(t *testing.T) {
	pool, svc, worldID, engPremier, engChamp, spaPremier, _ := sweepTestWorld(t)
	ctx := context.Background()

	engTable, err := svc.LastCompletedStandings(ctx, engPremier.ID)
	if err != nil {
		t.Fatalf("eng table: %v", err)
	}
	champX := engTable.Ranks[3] // 4th: out of the 1..1 band, entitled by +1

	champCup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID,
		Tier:    newInt(3),
		Name:    "Champions Cup",
		Qualification: []QualBandInput{
			{LeagueID: spaPremier.ID, From: 1, To: newInt(1)},
			{LeagueID: engChamp.ID, From: 1, To: newInt(1)},
		},
	})
	if err != nil {
		t.Fatalf("create champions cup: %v", err)
	}
	supportCup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID,
		Tier:    newInt(1),
		Name:    "Supporters Cup",
		Qualification: []QualBandInput{
			{LeagueID: engPremier.ID, From: 1, To: newInt(4)},
		},
	})
	if err != nil {
		t.Fatalf("create supporters cup: %v", err)
	}
	declareReigningChampion(t, pool, svc, worldID, champCup.ID, champX)

	champField, err := svc.ComputeCupField(ctx, champCup.ID)
	if err != nil {
		t.Fatalf("champions field: %v", err)
	}
	if champField.ClubCount != 3 {
		t.Fatalf("champions field = %d, want 3 (spa 1..1 + eng champ 1..1 + champion +1)", champField.ClubCount)
	}
	supportField, err := svc.ComputeCupField(ctx, supportCup.ID)
	if err != nil {
		t.Fatalf("supporters field: %v", err)
	}
	if supportField.ClubCount != 4 {
		t.Fatalf("supporters field = %d, want 4 (eng 1..4)", supportField.ClubCount)
	}

	// Starting the LOWER-tier cup must not poach X, who keeps the tier-3 seat.
	res, err := svc.StartRegionalCupCampaign(ctx, worldID, supportCup.ID)
	if err != nil {
		t.Fatalf("start supporters cup: %v", err)
	}
	if res.Season == nil || res.Season.Status != "in_progress" {
		t.Fatalf("supporters season = %+v, want in_progress", res.Season)
	}

	members := cupMembershipRows(t, pool, supportCup.ID)
	if len(members) != 3 {
		t.Fatalf("supporters members = %v, want 3 (X not drafted)", members)
	}
	if containsUUID(members, champX) {
		t.Fatalf("the double-qualified champion %s was drafted into the tier-1 cup", champX)
	}

	// The champion keeps its +1 seat in the higher-tier cup (still a live field
	// entitlement) — the sweep only stopped the lower cup from drafting it.
	if again, err := svc.ComputeCupField(ctx, champCup.ID); err != nil || again.ClubCount != 3 {
		t.Fatalf("champions field after sweep = %d, %v; want still 3", again.ClubCount, err)
	}

	headline, body := cascadeStory(t, pool, campaignEventID(t, pool, supportCup.ID))
	xName := clubName(t, pool, champX)
	if !strings.Contains(headline, xName) || !strings.Contains(headline, "Champions Cup") {
		t.Fatalf("headline = %q, want X keeps Champions Cup", headline)
	}
	if !strings.Contains(body, "Supporters Cup") || !strings.Contains(body, "stays open") {
		t.Fatalf("body = %q, want freed place stays open", body)
	}
}

// TestRegionalCupIntegrationSweepChoiceFlips drives the manager-side loop: a
// human manager records an opt-in for one of two equal-tier cups claiming the
// club, the choice (not the champion-defend default) decides the sweep at
// campaign start, the losing cup takes its next-best replacement (a privileged,
// cap-exempt cascade entrant), and both starts publish a cascade story.
func TestRegionalCupIntegrationSweepChoiceFlips(t *testing.T) {
	pool, svc, worldID, engPremier, engChamp, _, _ := sweepTestWorld(t)
	ctx := context.Background()

	engTable, err := svc.LastCompletedStandings(ctx, engPremier.ID)
	if err != nil {
		t.Fatalf("eng table: %v", err)
	}
	x := engTable.Ranks[0]  // league winner: entitled to any 1..n band
	r2 := engTable.Ranks[2] // rank 3: the cascade next-best for Cup A

	cupA, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, Tier: newInt(1), Name: "Cup A",
		Qualification: []QualBandInput{{LeagueID: engPremier.ID, From: 1, To: newInt(2)}},
	})
	if err != nil {
		t.Fatalf("create cup A: %v", err)
	}
	cupB, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, Tier: newInt(1), Name: "Cup B",
		Qualification: []QualBandInput{
			{LeagueID: engPremier.ID, From: 1, To: newInt(1)},
			{LeagueID: engChamp.ID, From: 1, To: newInt(1)},
		},
	})
	if err != nil {
		t.Fatalf("create cup B: %v", err)
	}

	// A human manager hands over club X (the seeded AI policy-bot stands down).
	userID := testdb.CreateUser(t, pool, "im09@example.com", "s3cret", []testdb.Join{{WorldID: worldID}})
	var managerID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM manager.managers WHERE user_id = $1`, userID).Scan(&managerID); err != nil {
		t.Fatalf("manager id: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE manager.managers SET current_club_id = NULL, status = 'unemployed' WHERE current_club_id = $1`, x); err != nil {
		t.Fatalf("stand down incumbent: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE manager.managers SET status = 'active', current_club_id = $2 WHERE id = $1`, managerID, x); err != nil {
		t.Fatalf("employ manager: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET current_manager_id = $2 WHERE id = $1`, x, managerID); err != nil {
		t.Fatalf("hand over club: %v", err)
	}

	// The manager opts X into Cup B (re-recorded to prove idempotence).
	if err := svc.RecordCupChoice(ctx, managerID, x, cupB.ID); err != nil {
		t.Fatalf("record choice B: %v", err)
	}
	if err := svc.RecordCupChoice(ctx, managerID, x, cupB.ID); err != nil {
		t.Fatalf("re-record choice B: %v", err)
	}

	// Start Cup B: the choice (not defend-default) sends X to B.
	if _, err := svc.StartRegionalCupCampaign(ctx, worldID, cupB.ID); err != nil {
		t.Fatalf("start cup B: %v", err)
	}
	bMembers := cupMembershipRows(t, pool, cupB.ID)
	if !containsUUID(bMembers, x) || len(bMembers) != 2 {
		t.Fatalf("cup B members = %v, want X + champ-league 1..1 club", bMembers)
	}

	// Cup A now lost X: its next-best, rank 3, is a cap-exempt cascade
	// replacement at campaign time.
	if _, err := svc.StartRegionalCupCampaign(ctx, worldID, cupA.ID); err != nil {
		t.Fatalf("start cup A: %v", err)
	}
	aMembers := cupMembershipRows(t, pool, cupA.ID)
	if containsUUID(aMembers, x) {
		t.Fatalf("cup A drafted %s against the recorded choice", x)
	}
	if !containsUUID(aMembers, r2) || len(aMembers) != 2 {
		t.Fatalf("cup A members = %v, want rank 2 + next-best %s", aMembers, r2)
	}
	if !planHasOrigin(t, pool, cupA.ID, r2, OriginCascadeReplacement) {
		t.Fatalf("cup A plan does not mark %s as cascade_replacement", r2)
	}

	// A choice for a cup the club is not projected for is rejected — probed
	// only now, because a single-entrant cup would abort the sweep's
	// minimum-field-size invariant during the campaign starts above.
	probeCup, err := svc.CreateRegionalCup(ctx, RegionalCupParams{
		WorldID: worldID, Tier: newInt(3), Name: "Probe Cup",
		Qualification: []QualBandInput{{LeagueID: engPremier.ID, From: 3, To: newInt(3)}},
	})
	if err != nil {
		t.Fatalf("create probe cup: %v", err)
	}
	if err := svc.RecordCupChoice(ctx, managerID, x, probeCup.ID); !errors.Is(err, ErrChoiceNotEligible) {
		t.Fatalf("reject unprojected choice err = %v, want ErrChoiceNotEligible", err)
	}

	// Both campaign starts published exactly one cascade story, and the Cup A
	// one names the next-best that replaced X.
	headline, body := cascadeStory(t, pool, campaignEventID(t, pool, cupA.ID))
	xName := clubName(t, pool, x)
	r2Name := clubName(t, pool, r2)
	if !strings.Contains(headline, xName) || !strings.Contains(headline, "Cup B") {
		t.Fatalf("headline = %q, want X keeps Cup B", headline)
	}
	if !strings.Contains(body, "Cup A") || !strings.Contains(body, r2Name) {
		t.Fatalf("body = %q, want %s joining Cup A as next-best", body, r2Name)
	}

	// The outlook readback shows the recorded choice on both conflicted cups.
	views, err := svc.ClubCupQualifications(ctx, worldID, x)
	if err != nil {
		t.Fatalf("cup qualifications: %v", err)
	}
	var aView, bView *CupQualificationView
	for i := range views {
		switch views[i].CupID {
		case cupA.ID:
			aView = &views[i]
		case cupB.ID:
			bView = &views[i]
		}
	}
	if aView == nil || bView == nil {
		t.Fatalf("outlook = %+v, want both Cup A and Cup B", views)
	}
	if aView.RecordedChoice != cupB.ID || bView.RecordedChoice != cupB.ID {
		t.Fatalf("recorded choice on A = %s, B = %s; want B on both", aView.RecordedChoice, bView.RecordedChoice)
	}
	if len(aView.Conflicts) != 1 || len(bView.Conflicts) != 1 {
		t.Fatalf("conflicts = A:%v B:%v, want exactly the mutual A↔B", aView.Conflicts, bView.Conflicts)
	}
	if !containsUUID([]uuid.UUID{r2}, aView.NextBest[0].ClubID) {
		t.Fatalf("cup A next-best = %+v, want rank 3 first", aView.NextBest)
	}
}
