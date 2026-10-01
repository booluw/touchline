//go:build integration

package match

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/matchsim"
	"github.com/touchline/backend/pkg/pitchsim"
)

func enableVisual(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO world.world_config (world_id, config_key, config_value)
		VALUES ($1, 'match.visual_engine', '"2d"')
		ON CONFLICT (world_id, config_key) DO UPDATE SET config_value = EXCLUDED.config_value`, worldID); err != nil {
		t.Fatalf("enable visual engine: %v", err)
	}
}

// feedCounts returns the matchsim rows, the pitchsim rows, and the rows with no
// offset for a match.
func feedCounts(t *testing.T, pool *pgxpool.Pool, matchID uuid.UUID) (sim, pitch, noOffset int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FILTER (WHERE source = 'matchsim')::int,
		       COUNT(*) FILTER (WHERE source = 'pitchsim')::int,
		       COUNT(*) FILTER (WHERE offset_millis IS NULL)::int
		FROM match.match_events WHERE match_id = $1`, matchID).Scan(&sim, &pitch, &noOffset); err != nil {
		t.Fatalf("feed counts: %v", err)
	}
	return
}

// trackExtras counts the pitchsim cues in a regenerated track.
func trackExtras(v *TrackView) int {
	n := 0
	for _, m := range v.Minutes {
		for _, c := range m.Cues {
			if c.Sequence > pitchsim.ExtraSequenceBase {
				n++
			}
		}
	}
	return n
}

// TestQuickPlayPitchsim: the world switch decides whether a quick-played match
// gets the positional engine. Off: the feed is matchsim's alone and there is no
// track. On: the match keeps a track, every event has a place in its minute,
// the extra events are stored, and the score is still matchsim's.
func TestQuickPlayPitchsim(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()
	worldID, homeID, awayID := worldFor(t, pool, ctx, "pitch-quick", "Harbour FC", "Riverside Albion")
	svc := newMatchService(pool)

	off := insertFixture(t, pool, ctx, uuid.Nil, worldID, homeID, awayID, time.Date(2030, 6, 1, 15, 0, 0, 0, time.UTC))
	r, err := svc.PlayFixture(ctx, off)
	if err != nil {
		t.Fatalf("play (off): %v", err)
	}
	if _, pitch, noOffset := feedCounts(t, pool, r.Match.ID); pitch != 0 || noOffset == 0 {
		t.Errorf("switch off: pitchsim rows = %d, rows without offset = %d; want 0 and all", pitch, noOffset)
	}
	if _, err := svc.GetTrack(ctx, r.Match.ID, 1, 0); !errors.Is(err, ErrNoTrack) {
		t.Errorf("switch off: GetTrack err = %v, want ErrNoTrack", err)
	}

	enableVisual(t, pool, worldID)
	on := insertFixture(t, pool, ctx, uuid.Nil, worldID, homeID, awayID, time.Date(2030, 6, 8, 15, 0, 0, 0, time.UTC))
	r, err = svc.PlayFixture(ctx, on)
	if err != nil {
		t.Fatalf("play (on): %v", err)
	}
	sim, pitch, noOffset := feedCounts(t, pool, r.Match.ID)
	if sim == 0 || pitch == 0 || noOffset != 0 {
		t.Fatalf("switch on: matchsim = %d, pitchsim = %d, without offset = %d", sim, pitch, noOffset)
	}
	home, away, err := svc.ScoreLine(ctx, r.Match.ID)
	if err != nil || home != r.Match.HomeGoals || away != r.Match.AwayGoals {
		t.Errorf("score from feed = %d-%d (err %v), engine = %d-%d", home, away, err, r.Match.HomeGoals, r.Match.AwayGoals)
	}

	view, err := svc.GetTrack(ctx, r.Match.ID, 1, 0)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if len(view.Minutes) != view.LastMinute || view.LastMinute < 90 {
		t.Fatalf("track minutes = %d, last minute = %d", len(view.Minutes), view.LastMinute)
	}
	// The regenerated track and the stored feed are the same engine run.
	if got := trackExtras(view); got != pitch {
		t.Errorf("track has %d extra events, feed stored %d", got, pitch)
	}
	if len(view.Players) < 22 {
		t.Errorf("resolved %d player names, want at least 22", len(view.Players))
	}
	part, err := svc.GetTrack(ctx, r.Match.ID, 10, 12)
	if err != nil || len(part.Minutes) != 3 || part.Minutes[0].Minute != 10 {
		t.Errorf("range 10..12: %d minutes (err %v)", len(part.Minutes), err)
	}
	fm, err := svc.GetFixtureMatch(ctx, on)
	if err != nil || fm.Match == nil || !fm.Match.Visual {
		t.Errorf("fixture view must report the simulation (err %v)", err)
	}
	feed, err := svc.GetMatchEvents(ctx, r.Match.ID)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	for i := 1; i < len(feed); i++ {
		a, b := feed[i-1], feed[i]
		if a.Minute > b.Minute || (a.Minute == b.Minute && *a.Offset > *b.Offset) {
			t.Fatalf("feed out of order at %d: %d'+%d then %d'+%d", i, a.Minute, *a.Offset, b.Minute, *b.Offset)
		}
	}
}

// TestLivePitchsim: a live match with the switch on produces one minute of
// track per paced minute, leaves matchsim's feed exactly as an instant Simulate
// gives it, and stores the same extra events a later regeneration produces.
func TestLivePitchsim(t *testing.T) {
	pool, worldID, _, _, _, _ := liveWorld(t)
	enableVisual(t, pool, worldID)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)
	ctx := context.Background()

	for sess.NextMinute() <= 90 {
		m := sess.NextMinute()
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace %d: %v", m, err)
		}
		if tr := sess.Track(); len(tr) != 1 || tr[0].Minute != m {
			t.Fatalf("minute %d: track has %d minute(s)", m, len(tr))
		}
	}
	if _, err := svc.Finalize(ctx, sess); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	expected := matchsim.Simulate(matchsim.Options{
		Seed: sess.Seed, Home: sess.Home, Away: sess.Away, Tuning: matchsim.DefaultTuning(),
	})
	rows, err := pool.Query(ctx, `
		SELECT sequence, minute, event_type FROM match.match_events
		WHERE match_id = $1 AND source = 'matchsim' ORDER BY sequence`, sess.MatchID)
	if err != nil {
		t.Fatalf("load feed: %v", err)
	}
	defer rows.Close()
	i := 0
	for ; rows.Next(); i++ {
		var seq, minute int
		var typ string
		if err := rows.Scan(&seq, &minute, &typ); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if i >= len(expected.Events) {
			t.Fatalf("feed has more matchsim rows than the engine produced (%d)", len(expected.Events))
		}
		if e := expected.Events[i]; e.Sequence != seq || e.Minute != minute || e.Type != typ {
			t.Fatalf("matchsim row %d = (%d,%d,%s), engine = (%d,%d,%s)", i, seq, minute, typ, e.Sequence, e.Minute, e.Type)
		}
	}
	if i != len(expected.Events) {
		t.Fatalf("feed has %d matchsim rows, engine produced %d", i, len(expected.Events))
	}

	_, pitch, noOffset := feedCounts(t, pool, sess.MatchID)
	view, err := svc.GetTrack(ctx, sess.MatchID, 1, 0)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if got := trackExtras(view); pitch == 0 || noOffset != 0 || got != pitch {
		t.Errorf("pitchsim rows = %d, without offset = %d, regenerated extras = %d", pitch, noOffset, got)
	}
}
