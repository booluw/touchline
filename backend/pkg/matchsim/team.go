// Package matchsim is the pure, seeded match engine. It has no database,
// network, or wall-clock dependency: identical (seed, teams, tuning, inputs)
// produce byte-identical results. Persistence, live pacing, and result
// application live in internal/match (S04-02).
//
// Spec: matchsim_addendum_v1.5.md (cumulative v1.1→v1.5; proposal until PM
// tuning sign-off, balanced style = identity vs the v1.4 approved block).
package matchsim

import "math"

// Team is one of the two match inputs. Attack and Defense are position-weighted
// ratings in [1,100] derived upstream (internal/squad, internal/form); the
// factor multiplier fields are the pre-computed Form × Morale × Motivation
// contributions. MatchDayVariance is NOT part of the input — it is drawn inside
// the engine from the seeded stream (spec §2.3, draw order Part 3).
type Team struct {
	ID       string
	ClubName string

	// Attack/Defense are the two-sided ratings (spec §2.1, replacing the old
	// single Ability). Attack feeds goal scaling when this side attacks,
	// Defense when it defends.
	Attack  int
	Defense int

	// Multiplicative modifiers computed upstream: Form (EWMA, ±15%), Squad
	// Morale (±10%), Club Ambition/Motivation (spec §2.2/§2.4/§2.5). Any
	// value <= 0 defaults to 1.0 (neutral) so an uncomputed factor never
	// zeroes the team out.
	FormFactor       float64
	MoraleFactor     float64
	MotivationFactor float64

	// Aggression [1,100] and RivalryIntensity 0-100 scale card probability
	// (spec §2.2/Concern 13); the orchestration layer sets RivalryIntensity
	// from club.rivalries (see docs/design/derby-rivalry-determination.md).
	Aggression       int
	RivalryIntensity int

	// PenaltyConversionRate is the designated taker's conversion rate in
	// [0,1], defaulting to the tuning baseline (0.78) when unset/invalid.
	// Upstream clamps takers to [0.65, 0.88] (PM Part 7 §3).
	PenaltyConversionRate float64

	// Tactics is the club's Simple-Mode style (S05-01, addendum v1.5). It
	// re-weights existing draws (possession, chances, conversion, cards,
	// stamina); the default "balanced" is the identity block, so a tactics-less
	// caller replays the v1.4 behavior exactly. Missing/unknown keys normalize
	// to balanced at simulate time.
	Tactics Tactics

	// Fitness is the XI's matchday conditioning in [0,1] (mean
	// player.player_condition.fitness). It seeds the engine's per-side stamina
	// tank; values below 1 make the side measurably late-game worse through the
	// post-75 fatigue penalty. <=0 means "not computed" and plays a fresh squad.
	Fitness float64

	// Lineups is the optional player-level cast for attribution (v1.6): the XI,
	// bench and penalty taker the engine resolves goal/assist/chance/card/
	// substitution tokens to, and the per-player match ratings it derives. When
	// nil, events carry no player ids and no per-player ratings are produced —
	// legacy and bare-fixture callers replay the exact v1.5 feed.
	Lineups *PlayerLineups
}

// Tactics is the per-team tactical setup consumed by the engine (v1.5). Style
// must be one of the S05-01 keys (StyleBalanced etc.); anything else falls back
// through Tuning.DefaultStyle to the identity block.
type Tactics struct {
	Style string
}

// PlayerRef is one lineup member the attribution pass can cast a token to. ID
// is the club's player uuid (string) resolved upstream (internal/squad); Weight
// is the member's attribute weight feeding the seeded weighted picks.
type PlayerRef struct {
	ID       string  `json:"id"`
	Position string  `json:"position,omitempty"`
	Weight   float64 `json:"weight,omitempty"`
}

// PlayerLineups is the player-level cast input for one side (v1.6). When a side
// provides it, the engine resolves every castable event token to a player and
// produces per-player match ratings; Taker names the designated penalty taker
// (falls back to a weighted XI pick when empty).
type PlayerLineups struct {
	XI    []PlayerRef `json:"xi"`
	Bench []PlayerRef `json:"bench,omitempty"`
	Taker string      `json:"taker_id,omitempty"`
}

// LiveInput is one ordered, minute-tagged manager input (spec §2.7, OPD-21).
// When present at a substitution window it replaces the random sub draw.
// tactic_change inputs (Detail {"style": "<key>"}) actively switch the side's
// style from their minute onward (addendum v1.5); they are consumed inside the
// engine and never surface in the feed.
type LiveInput struct {
	Minute int
	ClubID string
	Kind   string // "substitution" | "tactic_change"
	Detail map[string]any
}

// Options is the full input set for a simulation.
type Options struct {
	Seed int64
	Home Team
	Away Team
	// Tuning is the versioned tuning block; empty ⇒ DefaultTuning().
	Tuning Tuning
	// LiveInputs are ordered, minute-tagged manager inputs; empty for
	// non-live/quick-result matches. Replay contract: seed + LiveInputs ⇒
	// identical outcome.
	LiveInputs []LiveInput
}

// MatchEvent is one ordered feed event. Type values are the exact subset of
// the match.match_events CHECK constraint emitted by this engine (spec §3,
// PM Part 5 §5). Detail carries the optional commentary/referee explanation
// line (persisted as match_events.detail). PlayerID/RelatedPlayerID are the
// resolved player links from the v1.6 attribution pass (empty when the side
// provides no Lineups).
type MatchEvent struct {
	Sequence    int    `json:"sequence"`
	Minute      int    `json:"minute"`
	Type        string `json:"type"`
	ClubID      string `json:"club_id,omitempty"`
	Description string `json:"description"`
	Detail      string `json:"detail,omitempty"`

	PlayerID        string `json:"player_id,omitempty"`
	RelatedPlayerID string `json:"related_player_id,omitempty"`
}

// Well-known event types — the approved subset of match_events.event_type.
const (
	EventKickoff        = "kickoff"
	EventGoal           = "goal"
	EventAssist         = "assist"
	EventChance         = "chance_created"
	EventYellowCard     = "yellow_card"
	EventRedCard        = "red_card"
	EventSubstitution   = "substitution"
	EventInjury         = "injury"
	EventPenaltyAwarded = "penalty_awarded"
	EventPenaltyScored  = "penalty_scored"
	EventPenaltyMissed  = "penalty_missed"
	EventHalfTime       = "half_time"
	EventFullTime       = "full_time"
)

// MatchResult is the resolved outcome of a match.
type MatchResult struct {
	HomeGoals      int          `json:"home_goals"`
	AwayGoals      int          `json:"away_goals"`
	HomePossession float64      `json:"home_possession_pct"`
	Events         []MatchEvent `json:"events"`

	// HomePlayerRatings/AwayPlayerRatings are the per-player match ratings the
	// v1.6 attribution pass derives when the sides carry Lineups. Empty for
	// legacy callers. Goals/Assists/etc. are the attribution tallies; Minutes
	// mirrors the appearance derivation (90 minus sub-off for starters plus
	// post-sub minutes for bench players, clamped to [0,90]).
	HomePlayerRatings []PlayerRating `json:"home_player_ratings,omitempty"`
	AwayPlayerRatings []PlayerRating `json:"away_player_ratings,omitempty"`
}

// PlayerRating is one player's v1.6 match attribution: the seeded-cast
// tallies plus the derived 1..10 match rating.
type PlayerRating struct {
	PlayerID        string `json:"player_id"`
	Minutes         int    `json:"minutes"`
	Goals           int    `json:"goals"`
	Assists         int    `json:"assists"`
	Chances         int    `json:"chances"`
	YellowCards     int    `json:"yellow_cards"`
	RedCards        int    `json:"red_cards"`
	PenaltiesScored int    `json:"penalties_scored"`
	PenaltiesMissed int    `json:"penalties_missed"`
	Rating          int    `json:"rating"`
}

// Outcome labels produced by a chance draw.
const (
	outcomeGoal      = "goal"
	outcomeOnTarget  = "on_target"
	outcomeOffTarget = "off_target"
	outcomeBlocked   = "blocked"
	outcomeFoul      = "foul"
)

// clampRating keeps a raw Attack/Defense rating inside the documented domain.
func clampRating(a int) int {
	if a < 1 {
		return 1
	}
	if a > 100 {
		return 100
	}
	return a
}

// neutralFactor normalises an upstream multiplier: <= 0 means "not computed",
// so the team plays at neutral.
func neutralFactor(f float64) float64 {
	if f <= 0 {
		return 1
	}
	return f
}

// possessionShare returns the home side's possession probability for the
// tuning's possession exponent, given combined (Attack+Defense)/2 inputs.
func possessionShare(home, away, exponent float64) float64 {
	if exponent <= 0 {
		return 0.5
	}
	h := math.Pow(home, exponent)
	a := math.Pow(away, exponent)
	if h+a == 0 {
		return 0.5
	}
	return h / (h + a)
}
