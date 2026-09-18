// Package personality models each player's full behavioural profile on top of
// the player.player_personality / player.player_hidden_traits tables (migration
// 0006) and drives three deterministic, world-scoped behaviours:
//
//   - reaction: how a player responds to management/management-adjacent actions;
//   - reveal:    how hidden traits gradually surface through observation;
//   - influence: how traits bias training efficacy and recovery discipline.
//
// The engine is PURE (no RNG, no world state): given an Action and a TraitSet it
// returns deterministic Reaction objects, each carrying an Explanation that ties
// the reaction back to the underlying trait. Persistence/absence and the DB
// round-trip live behind the Store seam so unit tests run race-clean with no
// database, exactly as the sprint-9 gate battery demands.
package personality

import "fmt"

// TraitSet is the full behavioural vector for one player, formed from the
// visible personality columns (0006 player.player_personality) plus the hidden
// trait columns (0006 player.player_hidden_traits). All values are 1..10 except
// the hidden boolean potential_ceiling_locked flag.
type TraitSet struct {
	PlayerID string

	// Visible personality (player.player_personality).
	Professionalism int // 1..10 — drive + work-rate + willingness to train hard.
	Ambition        int // 1..10 — hunger to start, win, and move to bigger clubs.
	Loyalty         int // 1..10 — attachment to club; how strongly wage/mistreated offers sting.
	Ego             int // 1..10 — self-image; how targets respond to being benched.
	Sociability     int // 1..10 — how the squad treats someone who turns up difficult.
	Adaptability    int // 1..10 — how quickly a player accepts a new role/shape/league.
	Patience        int // 1..10 — how long they endure bad news before reacting.
	Leadership      int // 1..10 — presence in the dressing room.
	Volatility      int // 1..10 — emotional_volatility; how fast a slight escalates.

	// Hidden traits (player.player_hidden_traits) — only revealed over time.
	Potential            int  // 1..10 — career ceiling (raw talent at intake).
	PotentialLocked      bool // potential_ceiling_locked — becomes true after aging proves the ceiling.
	Consistency          int  // 1..10 — how repeatable performance is from game to game.
	InjurySusceptibility int  // 1..10 — hidden injury risk.
	PressureHandling     int  // 1..10 — hidden composure in high-stakes moments.
	LearningSpeed        int  // 1..10 — hidden rate of skill acquisition in training.
}

// Action is every management/management-adjacent behaviour the reaction engine
// responds to. S09-01 covers the full catalogue (not just two exemplars), so the
// acceptance-criteria reactions — missed promises, dropped from the XI, wage/
// contract offers, transfer/signing outcomes, team talks, squad dynamics — all
// resolve here.
type Action string

const (
	ActionMissedPromise         Action = "missed_promise" // management promised playing time / wage / release and broke it.
	ActionDroppedFromStartingXI Action = "dropped_from_starting_xi"
	ActionBenchedLongTerm       Action = "benched_long_term"
	ActionWageCutOffered        Action = "wage_cut_offered"            // below expectations / below market for the profile.
	ActionContractApproved      Action = "contract_structure_approved" // loyal accept wage structures (criterion).
	ActionTransferBidQuery      Action = "transfer_bid_query"
	ActionTransferBidBlocked    Action = "transfer_bid_blocked"    // club keeps him despite interest.
	ActionReleasedFromSquad     Action = "released_from_squad"     // free-agent release (A08 seam).
	ActionTrainingLoadIncreased Action = "training_load_increased" // professionalism+ambition absorb intensity.
	ActionTeamTalkMotivational  Action = "team_talk_motivational"
	ActionTeamTalkCritical      Action = "team_talk_critical"
	ActionResearchDenied        Action = "research_room_denied" // (reserved — see OPENCODE "not built yet": research room backlog)
)

// EmotionalState is the observable mood a reaction lands the player in; it maps
// onto the player.player_emotional_states cause vocabulary (migration 0006).
type EmotionalState string

const (
	EmoContent    EmotionalState = "content"
	EmoHappy      EmotionalState = "happy"
	EmoMotivated  EmotionalState = "motivated"
	EmoFrustrated EmotionalState = "frustrated"
	EmoAngry      EmotionalState = "angry"
	EmoBetrayed   EmotionalState = "betrayed"
	EmoConfident  EmotionalState = "confident"
	EmoAmbition   EmotionalState = "ambitious"
	EmoVolatile   EmotionalState = "volatile"
)

// Explanation links a single player reaction back to the trait that caused it
// (the acceptance-criteria "clear Explanation objects linking reactions to
// underlying personality traits").
type Explanation struct {
	// Trait is the column/trait that drove this reaction.
	Trait string `json:"trait"`
	// Value is the 1..10 trait value that triggered the branch.
	Value int `json:"value"`
	// Text is a server-authoritative, human-readable explanation for it.
	Text string `json:"text"`
}

// Reaction is one deterministic response to an Action.
type Reaction struct {
	Emotion EmotionalState `json:"emotion"`
	// Severity is how strongly the mood shifts; managers see it in Explanation.
	Severity    int         `json:"severity"`
	Explanation Explanation `json:"explanation"`
}

// ReactionResponse aggregates every reaction a trait set produces for an action,
// plus the newest hidden-trait revelation (if any) surfaced by the interaction.
type ReactionResponse struct {
	Action    Action       `json:"action"`
	Reactions []Reaction   `json:"reactions"`
	Revealed  *TraitReveal `json:"revealed,omitempty"`
}

// TraitReveal is a single hidden-trait exposure: which trait surfaced, from what
// interaction source, with an observation-driven confidence.
type TraitReveal struct {
	PlayerID   string `json:"player_id"`
	Trait      string `json:"trait"`
	Source     string `json:"source"`     // e.g. "missed_promise_interaction", "scouting_report", "training_observation"
	Confidence int    `json:"confidence"` // 1..100, grows with more exposure
	Text       string `json:"text"`
}

func (x *TraitSet) validateTrait(v int) error {
	if v < 1 || v > 10 {
		return fmt.Errorf("personality: trait value %d out of range 1..10", v)
	}
	return nil
}
