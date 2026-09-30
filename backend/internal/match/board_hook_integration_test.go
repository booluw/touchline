//go:build integration

package match

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	internalboard "github.com/touchline/backend/internal/board"
	internalsocial "github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/testdb"
)

// TestPlayFixtureWiresBoardHook proves the IM33 seam: finishing a fixture rates
// both managers, moves supporter sentiment to the stored value, and publishes
// exactly one fan-reaction story — for the human-managed club only — linked to
// the MATCH_PLAYED event.
func TestPlayFixtureWiresBoardHook(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	worldID, homeID, awayID := worldFor(t, pool, ctx, "match-board-world", "Harbour FC", "Riverside Albion")
	fixtureID := insertFixture(t, pool, ctx, uuid.Nil, worldID, homeID, awayID, time.Date(2030, 6, 1, 15, 0, 0, 0, time.UTC))
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers SET is_policy_bot = FALSE
		WHERE id = (SELECT current_manager_id FROM club.clubs WHERE id = $1)`, homeID); err != nil {
		t.Fatalf("promote home manager: %v", err)
	}

	svc := newMatchService(pool)
	svc.WithSocial(internalsocial.NewService(pool, nil))
	svc.WithBoard(internalboard.NewService(pool, nil, nil))
	if _, err := svc.PlayFixture(ctx, fixtureID); err != nil {
		t.Fatalf("play fixture: %v", err)
	}

	// One rating per club, and the club's sentiment is the stored sentiment_after.
	var ratings, mismatched int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       COUNT(*) FILTER (WHERE sg.current_sentiment IS DISTINCT FROM r.sentiment_after)::int
		FROM manager.match_ratings r
		LEFT JOIN club.supporter_groups sg ON sg.club_id = r.club_id
		WHERE r.fixture_id = $1`, fixtureID).Scan(&ratings, &mismatched); err != nil {
		t.Fatalf("load ratings: %v", err)
	}
	if ratings != 2 || mismatched != 0 {
		t.Errorf("ratings = %d (want 2), sentiment mismatches = %d (want 0)", ratings, mismatched)
	}

	// Exactly one fan story, about the human (home) club, tied to MATCH_PLAYED.
	var stories, linked int
	var headline string
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int, COALESCE(MAX(n.headline), ''),
		       COUNT(*) FILTER (WHERE e.event_type = 'MATCH_PLAYED')::int
		FROM world.news_stories n
		LEFT JOIN world.events e ON e.id = n.related_event_id
		WHERE n.world_id = $1 AND n.category = 'fan_reaction'`, worldID).Scan(&stories, &headline, &linked); err != nil {
		t.Fatalf("load stories: %v", err)
	}
	if stories != 1 || linked != 1 {
		t.Errorf("fan stories = %d, linked to MATCH_PLAYED = %d, want 1 and 1", stories, linked)
	}
	if len(headline) < 10 || headline[:10] != "Harbour FC" {
		t.Errorf("headline = %q, want the home club's story", headline)
	}
}
