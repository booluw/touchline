// Package policybot is the S06-05 absence-delegation engine. When a manager is
// away (explicitly, or auto-away after three missed club fixtures) their club
// is managed by a per-world unemployment bot: on each deadline the bot invokes
// the SAME command handlers a human would call (tactics/lineup, training plan,
// transfer-bid response) so a manager on holiday never leaves a silent gap.
//
// Delegation is policy-driven but defaults always act: a manager with no saved
// policy gets the assistant defaults (strongest XI honouring the saved lineup,
// DNA-weighted training plan, never-sell-below-market transfer rule), so
// "no policy" never means "no automation".
package policybot

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Decision types stored in manager.policies.policy_type.
const (
	TypeSquad    = "squad"    // matchday lineup + tactics
	TypeTransfer = "transfer" // open-bid responses on the manager's own listings
	TypeTraining = "training" // weekly training-plan selection
)

// ValidPolicyTypes is the accepted policy_type vocabulary.
var ValidPolicyTypes = map[string]bool{
	TypeSquad: true, TypeTransfer: true, TypeTraining: true,
}

// Sentinel errors surfaced by the service.
var (
	ErrInvalidPolicyType = errors.New("policy type must be squad, transfer or training")
	ErrInvalidPolicyJSON = errors.New("policy params are not valid JSON")
	ErrManagerNotFound   = errors.New("manager not found")
)

// Actor is the invoking principal (manager | policy bot), mirroring the
// command services' Actor shape so the bot can hand its identity down.
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

func (a Actor) actorTypeAndID() (string, *uuid.UUID) {
	t := "manager"
	if a.IsPolicyBot {
		t = "policy_bot"
	}
	return t, &a.ManagerID
}

// Event types emitted by this package.
const (
	EventAbsenceSet     = "ABSENCE_SET"
	EventAbsenceCleared = "ABSENCE_CLEARED"
	EventPolicySaved    = "POLICY_SAVED"
	EventPolicyDeleted  = "POLICY_DELETED"
)

// Policy is one saved delegation rule (manager.policies row), with its raw
// params JSONB plus the typed view the resolver consumes.
type Policy struct {
	WorldID   uuid.UUID       `json:"world_id"`
	ManagerID uuid.UUID       `json:"manager_id"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params"`
	Enabled   bool            `json:"enabled"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Default policies. act on Failure: when no saved row exists the resolver
// falls back to these documented assistant defaults (decision #3: policies
// always act as assistant defaults — no silent gap).

// Squad lineup rules.
const (
	RuleBestEleven  = "best_eleven"  // honour the saved XI, deterministic gap-fill
	RuleBestFitness = "best_fitness" // freshest-legs XI by player condition
	RuleRotate      = "rotate"       // rest the incumbent XI, field a reserve-heavy XI
)

// Tactics delegation directives.
const (
	TacticsKeep = "keep" // keep the manager's saved style/formation (default)
)

// SquadPolicy is the typed manager.policies params for TypeSquad.
type SquadPolicy struct {
	Rule    string `json:"rule"`
	Tactics string `json:"tactics"`
}

// DefaultSquadPolicy is the assistant default (decision #1: strongest XI that
// honours the saved lineup, no tactics change).
var DefaultSquadPolicy = SquadPolicy{Rule: RuleBestEleven, Tactics: TacticsKeep}

// TransferPolicy is the typed params for TypeTransfer.
type TransferPolicy struct {
	SellFloorPct   int64 `json:"sell_floor_pct"`   // % of valuation below which a bid is rejected
	AcceptAbovePct int64 `json:"accept_above_pct"` // % of valuation at/above which a bid is accepted
	AutoCounter    bool  `json:"auto_counter"`     // counter rather than reject in the negotiation band
}

// DefaultTransferPolicy is the assistant default: never go below the market
// valuation, accept 1.2x+, and haggle (rather than flatly reject) in between.
var DefaultTransferPolicy = TransferPolicy{
	SellFloorPct: 100, AcceptAbovePct: 120, AutoCounter: true,
}

// TrainingPolicy is the typed params for TypeTraining. Archetype picks one of
// the five training archetypes, "dna" (or empty) defers to the club-DNA
// weighted default.
type TrainingPolicy struct {
	Archetype string `json:"archetype"` // "technical"|"physical"|"defensive"|"attacking"|"recovery"|"dna"|""
}

// DefaultTrainingPolicy acts from the club DNA.
var DefaultTrainingPolicy = TrainingPolicy{Archetype: "dna"}

// AbsenceState is the manager's away-mode status (manager.managers columns).
type AbsenceState struct {
	AwaySince         *time.Time `json:"away_since"`
	AwayAuto          bool       `json:"away_auto"`
	ConsecutiveMissed int        `json:"consecutive_missed"`
	LastActivityAt    *time.Time `json:"last_activity_at"`
}

// IsAway reports whether delegation is currently active (explicit or auto).
func (a AbsenceState) IsAway() bool { return a.AwaySince != nil }

// MissedFixtureThreshold is how many unattended club fixtures trigger auto-away
// (decision #1: auto-activates after three missed consecutive club fixtures).
const MissedFixtureThreshold = 3

// HeartbeatInterval throttles authenticated-request activity writes: the
// manager is only marked "attended" at most once per interval.
const HeartbeatInterval = time.Hour
