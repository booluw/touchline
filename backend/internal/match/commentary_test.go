package match

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/matchsim"
)

func eventRow(t *testing.T, typ, commentary string, primary, related string) *MatchEventRow {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"commentary": commentary})
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	row := &MatchEventRow{Type: typ, Detail: raw}
	if primary != "" {
		row.Player = &apiref.PlayerRef{ID: uuid.New(), Name: primary}
	}
	if related != "" {
		row.RelatedPlayer = &apiref.PlayerRef{ID: uuid.New(), Name: related}
	}
	return row
}

func commentaryText(t *testing.T, row *MatchEventRow) string {
	t.Helper()
	var text map[string]string
	if err := json.Unmarshal(row.Detail, &text); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	return text["commentary"]
}

// TestCommentaryResolvesNames pins the role → name mapping per event type: the
// engine's attribution pass orders player_id/related_player_id by what the
// event is, and the placeholder must follow that order or the sentence names the
// wrong man.
func TestCommentaryResolvesNames(t *testing.T) {
	cases := []struct {
		name       string
		typ        string
		commentary string
		primary    string
		related    string
		want       string
	}{
		{
			name:       "goal names the scorer",
			typ:        matchsim.EventGoal,
			commentary: "GOAL for Harbour City FC! {player} finishes after a flowing move.",
			primary:    "Ada Bright",
			want:       "GOAL for Harbour City FC! Ada Bright finishes after a flowing move.",
		},
		{
			name:       "assist names the assister and the scorer",
			typ:        matchsim.EventAssist,
			commentary: "Assist from {assist} for Harbour City FC — lovely service for {player}.",
			primary:    "Kit Marek",
			related:    "Ada Bright",
			want:       "Assist from Kit Marek for Harbour City FC — lovely service for Ada Bright.",
		},
		{
			name:       "substitution names the player on and the player off",
			typ:        matchsim.EventSubstitution,
			commentary: "Harbour City FC make a change: {sub} on for {player}.",
			primary:    "Rob Tiller",
			related:    "Sam Ord",
			want:       "Harbour City FC make a change: Rob Tiller on for Sam Ord.",
		},
		{
			name:       "unresolved primary falls back to neutral wording",
			typ:        matchsim.EventChance,
			commentary: "Big chance for Harbour City FC — {player} tests the keeper but can't convert.",
			want:       "Big chance for Harbour City FC — the player tests the keeper but can't convert.",
		},
		{
			name:       "unresolved related falls back to neutral wording",
			typ:        matchsim.EventSubstitution,
			commentary: "Harbour City FC forced into a change: {sub} on for the injured {player}.",
			primary:    "Rob Tiller",
			want:       "Harbour City FC forced into a change: Rob Tiller on for the injured the player.",
		},
		{
			name:       "structural event keeps its own text",
			typ:        matchsim.EventFullTime,
			commentary: "Full-time: 2-1 to Harbour City FC.",
			want:       "Full-time: 2-1 to Harbour City FC.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := eventRow(t, tc.typ, tc.commentary, tc.primary, tc.related)
			resolveCommentary([]*MatchEventRow{row})
			if got := commentaryText(t, row); got != tc.want {
				t.Fatalf("commentary\n got: %s\nwant: %s", got, tc.want)
			}
			if strings.ContainsRune(commentaryText(t, row), '{') {
				t.Fatalf("a raw placeholder leaked: %s", commentaryText(t, row))
			}
		})
	}
}

// TestCommentaryLeavesForeignDetailAlone proves the rewrite is scoped: a detail
// that is not the engine's commentary object is passed through byte for byte,
// so a future event shape can never be mangled by this pass.
func TestCommentaryLeavesForeignDetailAlone(t *testing.T) {
	raw := json.RawMessage(`["not","an","object"]`)
	row := &MatchEventRow{Type: matchsim.EventGoal, Detail: raw}
	resolveCommentary([]*MatchEventRow{row})
	if string(row.Detail) != string(raw) {
		t.Fatalf("detail = %s, want it untouched", row.Detail)
	}

	empty := &MatchEventRow{Type: matchsim.EventGoal}
	resolveCommentary([]*MatchEventRow{empty})
	if empty.Detail != nil {
		t.Fatalf("nil detail became %s", empty.Detail)
	}
}

// TestCommentaryIdempotent proves re-resolving an already-rendered row is a
// no-op: a live tick renders the rows it just persisted, and the REST read
// renders them again.
func TestCommentaryIdempotent(t *testing.T) {
	row := eventRow(t, matchsim.EventGoal, "GOAL! {player} converts the penalty for Harbour City FC.", "Ada Bright", "")
	resolveCommentary([]*MatchEventRow{row})
	once := string(row.Detail)
	resolveCommentary([]*MatchEventRow{row})
	if string(row.Detail) != once {
		t.Fatalf("re-resolving changed the detail:\n once: %s\n twice: %s", once, row.Detail)
	}
}

// TestCommentaryCoversEngineTemplates renders every {…} template the engine can
// emit, so a new template cannot ship with a raw placeholder in the feed.
func TestCommentaryCoversEngineTemplates(t *testing.T) {
	rows := []*MatchEventRow{
		eventRow(t, matchsim.EventGoal, "GOAL for Harbour City FC! {player} finishes after a flowing move.", "Ada Bright", ""),
		eventRow(t, matchsim.EventAssist, "Assist from {assist} for Harbour City FC — lovely service for {player}.", "Kit Marek", "Ada Bright"),
		eventRow(t, matchsim.EventChance, "Big chance for Harbour City FC — {player} tests the keeper but can't convert.", "Nina Quill", ""),
		eventRow(t, matchsim.EventPenaltyScored, "GOAL! {player} converts the penalty for Harbour City FC.", "Kit Marek", ""),
		eventRow(t, matchsim.EventPenaltyMissed, "{player} misses the penalty for Harbour City FC — a huge let-off!", "Kit Marek", ""),
		eventRow(t, matchsim.EventSubstitution, "Harbour City FC make a change from the bench: {sub} on for {player}.", "Rob Tiller", "Sam Ord"),
		eventRow(t, matchsim.EventSubstitution, "Harbour City FC make a change: {sub} on for {player}.", "Rob Tiller", "Sam Ord"),
		eventRow(t, matchsim.EventInjury, "{player} comes off injured for Harbour City FC.", "Nina Quill", ""),
		eventRow(t, matchsim.EventSubstitution, "Harbour City FC forced into a change: {sub} on for the injured {player}.", "Rob Tiller", "Nina Quill"),
	}
	resolveCommentary(rows)
	for _, row := range rows {
		if text := commentaryText(t, row); strings.ContainsRune(text, '{') {
			t.Fatalf("%s: a raw placeholder leaked: %s", row.Type, text)
		}
	}
}
