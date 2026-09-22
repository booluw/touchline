//go:build integration

package matchday

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/realtime"
)

// feedRecorder collects realtime envelopes synchronously (LocalBroker delivers
// publishes inline) for the S04-03 match_tick assertions.
type feedRecorder struct {
	mu  sync.Mutex
	evs []realtime.Event
}

func (r *feedRecorder) add(ev realtime.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func (r *feedRecorder) all() []realtime.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]realtime.Event{}, r.evs...)
}

func (r *feedRecorder) ticks(matchID uuid.UUID) []match.MatchTickPayload {
	var out []match.MatchTickPayload
	for _, ev := range r.all() {
		if ev.Type != realtime.EventMatchTick || ev.Payload == nil {
			continue
		}
		var tk match.MatchTickPayload
		if err := json.Unmarshal(ev.Payload, &tk); err != nil {
			continue
		}
		if tk.Match.ID == matchID {
			out = append(out, tk)
		}
	}
	return out
}

// TestRunnerPublishesMatchTickFeed kicks one matchday and runs it live with the
// realtime broker installed (S04-03): every paced minute and the finalization
// must emit an ordered match_tick envelope whose events are exactly the
// persisted feed, with a server-computed scoreline and the caller's world
// scoping the stream.
func TestRunnerPublishesMatchTickFeed(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE world.worlds SET current_tick = current_tick + 1, current_day = current_day + 1 WHERE id = $1`, worldID); err != nil {
		t.Fatalf("advance day: %v", err)
	}

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	broker := realtime.NewLocalBroker()
	t.Cleanup(func() { _ = broker.Close() })
	rec := &feedRecorder{}
	subCtx, subCancel := context.WithCancel(ctx)
	t.Cleanup(subCancel)
	go func() { _ = broker.Subscribe(subCtx, rec.add) }()
	<-broker.Ready()

	runner := NewRunner(pool, matches, compSvc).WithRealtime(broker)

	sum, err := runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("kickoff: %v", err)
	}
	if sum.Kicked != 4 {
		t.Fatalf("kicked = %d, want 4", sum.Kicked)
	}
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("run live: %v", err)
	}

	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT m.id FROM match.matches m
		JOIN match.fixtures f ON f.id = m.fixture_id
		WHERE f.world_id = $1
		ORDER BY f.scheduled_at, f.id LIMIT 1`, worldID).Scan(&matchID); err != nil {
		t.Fatalf("load match: %v", err)
	}

	// Every envelope in the stream is match_tick and world-scoped.
	for _, ev := range rec.all() {
		if ev.Type != realtime.EventMatchTick {
			t.Fatalf("unexpected envelope type %q", ev.Type)
		}
		if ev.WorldID == nil || *ev.WorldID != worldID {
			t.Fatalf("envelope world_id = %v, want %s", ev.WorldID, worldID)
		}
	}

	ticks := rec.ticks(matchID)
	if len(ticks) != 91 {
		t.Fatalf("match got %d match_tick envelopes, want 91 (90 paced minutes + completion)", len(ticks))
	}
	if first := ticks[0]; first.Minute != 1 || first.Status != match.MatchStatusInProgress {
		t.Fatalf("first tick = %+v, want minute 1 in_progress", first)
	}
	last := ticks[len(ticks)-1]
	if last.Status != match.MatchStatusCompleted || last.Minute != 90 {
		t.Fatalf("final tick = %+v, want completed 90", last)
	}

	// The concatenated tick events are exactly the persisted feed — the same
	// rows the quick-result REST path serves (S04-03 AC: one event list).
	persisted, err := matches.GetMatchEvents(ctx, matchID)
	if err != nil {
		t.Fatalf("load persisted feed: %v", err)
	}
	var fed []*match.MatchEventRow
	for _, tk := range ticks {
		fed = append(fed, tk.Events...)
	}
	if len(fed) != len(persisted) {
		t.Fatalf("tick feed has %d events, persisted has %d", len(fed), len(persisted))
	}
	for i, ev := range fed {
		want := persisted[i]
		if ev.ID != want.ID || ev.Sequence != want.Sequence || ev.Minute != want.Minute ||
			ev.Type != want.Type || ev.Match.ID != want.Match.ID {
			t.Fatalf("tick feed[%d] = %s seq %d m%d %s match %s, want %s seq %d m%d %s match %s",
				i, ev.ID, ev.Sequence, ev.Minute, ev.Type, ev.Match.ID,
				want.ID, want.Sequence, want.Minute, want.Type, want.Match.ID)
		}
	}

	// Persisted and tick rows carry identical nested club/player refs.
	for i, ev := range fed {
		want := persisted[i]
		if ev.Club != nil && (want.Club == nil || ev.Club.ID != want.Club.ID) {
			t.Fatalf("tick feed[%d] club mismatch: tick %+v, persisted %+v", i, ev.Club, want.Club)
		}
		if ev.Player != nil && (want.Player == nil || ev.Player.ID != want.Player.ID) {
			t.Fatalf("tick feed[%d] player mismatch: tick %+v, persisted %+v", i, ev.Player, want.Player)
		}
	}
	if ticks[0].Match.ID == uuid.Nil || ticks[0].Fixture.ID == uuid.Nil {
		t.Fatalf("tick carried no match/fixture ref: %+v", ticks[0])
	}
	if ticks[0].HomeClub.ID == uuid.Nil || ticks[0].HomeClub.Name == "" ||
		ticks[0].AwayClub.ID == uuid.Nil || ticks[0].AwayClub.Name == "" {
		t.Fatalf("tick carried no home/away club refs: %+v", ticks[0])
	}

	// The final tick carries the authoritative server scoreline (client never
	// computes the outcome).
	home, away, err := matches.ScoreLine(ctx, matchID)
	if err != nil {
		t.Fatalf("scoreline: %v", err)
	}
	if last.HomeScore != home || last.AwayScore != away {
		t.Fatalf("final tick score = %d-%d, want %d-%d", last.HomeScore, last.AwayScore, home, away)
	}
}
