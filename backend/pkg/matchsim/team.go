// Package matchsim is the pure, seeded match engine. It has no database,
// network, or wall-clock dependency: identical (seed, teams, tuning) inputs
// produce byte-identical results. Persistence, live pacing, and result
// application live in internal/match (S04-02).
package matchsim

import "math"

// Team is one of the two match inputs. Ability is a single overall strength in
// [1,100], derived by the orchestration layer from squad player_attributes;
// the engine treats it as opaque input so determinism depends only on
// (seed, teams, tuning).
type Team struct {
	ID       string
	ClubName string
	Ability  float64
}

// Options is the full input set for a simulation.
type Options struct {
	Seed   int64
	Home   Team
	Away   Team
	Tuning Tuning
}

// MatchEvent is one ordered feed event. Type values are a subset of the
// match.match_events CHECK constraint so results persist verbatim.
type MatchEvent struct {
	Sequence    int    `json:"sequence"`
	Minute      int    `json:"minute"`
	Type        string `json:"type"`
	ClubID      string `json:"club_id,omitempty"`
	Description string `json:"description"`
}

// Well-known event types (subset of match.match_events.event_type).
const (
	EventKickoff      = "kickoff"
	EventGoal         = "goal"
	EventChance       = "chance_created"
	EventYellowCard   = "yellow_card"
	EventRedCard      = "red_card"
	EventSubstitution = "substitution"
	EventHalfTime     = "half_time"
	EventFullTime     = "full_time"
)

// MatchResult is the resolved outcome of a match.
type MatchResult struct {
	HomeGoals      int          `json:"home_goals"`
	AwayGoals      int          `json:"away_goals"`
	HomePossession float64      `json:"home_possession_pct"`
	Events         []MatchEvent `json:"events"`
}

// Outcome labels produced by a chance draw.
const (
	outcomeGoal      = "goal"
	outcomeOnTarget  = "on_target"
	outcomeOffTarget = "off_target"
	outcomeBlocked   = "blocked"
	outcomeFoul      = "foul"
)

// clampAbility keeps a team ability inside the documented domain.
func clampAbility(a float64) float64 {
	if a < 1 {
		return 1
	}
	if a > 100 {
		return 100
	}
	return a
}

// possessionShare returns the home side's possession probability for the
// tuning's possession exponent.
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
