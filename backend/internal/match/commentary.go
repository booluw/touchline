package match

import (
	"encoding/json"
	"strings"

	"github.com/touchline/backend/pkg/matchsim"
)

// The engine writes commentary as a template so the same string can be
// re-simulated deterministically: it names a *role* ("{player}", "{assist}",
// "{sub}") and leaves the attribution pass to say who filled it. The persisted
// event row carries the same two people in two columns — `player_id` (the
// event's subject) and `related_player_id` (its counterpart) — so the names are
// resolved at read time, after `resolveEventRefs` has decorated the rows. Nothing
// is rewritten in the database: the engine's canonical text stays the record, the
// wire shows names.
//
// The role → column mapping is per event type, because the engine's attribution
// pass orders the two ids by what the event *is*, not alphabetically:
//
//	goal/chance/penalty/card  → player_id is the subject         → {player}
//	assist                     → player_id is the assister        → {assist}, {player} = scorer
//	substitution               → player_id is the bench player on  → {sub}, {player} = replaced
//
// Any other event type falls back to {player} = player_id, which is the subject
// in every remaining case.
type tokenSource struct {
	token   string
	related bool // true → the token names the related player, not the primary
}

// neutral fallbacks for a token whose person the attribution pass could not
// resolve (a side without lineups, a deleted player). Neutral wording keeps the
// sentence readable; a raw "{player}" in the UI never leaks.
const (
	fallbackPlayer = "the player"
	fallbackAssist = "a teammate"
	fallbackSub    = "a substitute"
)

var commentaryTokens = map[string][]tokenSource{
	matchsim.EventAssist:       {{token: "{assist}"}, {token: "{player}", related: true}},
	matchsim.EventSubstitution: {{token: "{sub}"}, {token: "{player}", related: true}},
}

func fallbackFor(token string) string {
	switch token {
	case "{assist}":
		return fallbackAssist
	case "{sub}":
		return fallbackSub
	default:
		return fallbackPlayer
	}
}

// resolveCommentary rewrites each row's commentary/detail text with the names
// resolveEventRefs just attached. Rows whose text holds no placeholder, or whose
// detail is not the engine's {commentary, detail} object, are left untouched.
func resolveCommentary(rows []*MatchEventRow) {
	for _, e := range rows {
		e.Detail = renderCommentary(e)
	}
}

// renderCommentary substitutes an event's commentary placeholders with the
// resolved player names. Pure: it reads the row and returns new JSON, so the
// same call is safe on a freshly persisted row (live tick) and on a row read
// back from Postgres (REST).
func renderCommentary(e *MatchEventRow) json.RawMessage {
	if len(e.Detail) == 0 {
		return e.Detail
	}
	var text map[string]string
	if err := json.Unmarshal(e.Detail, &text); err != nil {
		return e.Detail // not our object shape; leave the caller's bytes alone
	}
	changed := false
	for key, value := range text {
		if !strings.ContainsRune(value, '{') {
			continue
		}
		names := commentaryNames(e)
		for token, name := range names {
			if name == "" {
				name = fallbackFor(token)
			}
			if replaced := strings.ReplaceAll(value, token, name); replaced != value {
				value = replaced
				changed = true
			}
		}
		text[key] = value
	}
	if !changed {
		return e.Detail
	}
	out, err := json.Marshal(text)
	if err != nil {
		return e.Detail
	}
	return out
}

// commentaryNames is the placeholder → name table for one event type.
func commentaryNames(e *MatchEventRow) map[string]string {
	primary, related := "", ""
	if e.Player != nil {
		primary = e.Player.Name
	}
	if e.RelatedPlayer != nil {
		related = e.RelatedPlayer.Name
	}
	names := make(map[string]string, 3)
	if sources, ok := commentaryTokens[e.Type]; ok {
		for _, src := range sources {
			if src.related {
				names[src.token] = related
			} else {
				names[src.token] = primary
			}
		}
		return names
	}
	names["{player}"] = primary
	names["{assist}"] = related
	return names
}
