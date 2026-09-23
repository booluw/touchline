// Package squad is the orchestration layer's per-matchday aggregation package
// (matchsim addendum v1.4 Part 8, approved). It is deliberately pure — it reads
// no databases and emits no events: it turns persisted player/club data
// (passed in as plain structs by internal/match) into the scalar modifiers the
// pure engine consumes.
//
// Responsibilities (all negotiated in the addenda):
//
//   - ComputeSquadMorale        — one [0.90, 1.10] multiplier from each
//     starter's sentiment, weighted by leadership × starting status (v1.2 §2.4).
//   - ComputeMotivation         — one [0.85, 1.20] multiplier from
//     competitiveness + fixture stakes + rivalry floor + the approved 10%
//     giant-killing roll (v1.2 §2.5, Part 5 §2).
//   - ComputePlayerPerformanceFactor — per-key-player [0.85, 1.15] multiplier
//     from consistency (variance width), temperament/pressure (high-stakes
//     only), and the player's own unweighted sentiment (v1.3 §2.8, Part 7 §1/§3).
//   - SelectKeyPlayers          — the top 3–5 attribute-weight contributors
//     (GK/Captain/ST/AM tie-break) ∪ any starter with |sentiment| >= 20
//     (Part 7 §1, Part 8).
//   - BuildSquadRatings         — aggregates the XI into one Attack/Defense
//     pair via position-weighted attribute categories, applying the per-player
//     performance factors (v1.2 §2.1; weights are proposal data, PM open item 4).
//   - ScoutingTags / LineupWarning — the PM-mandated trait labels and pre-match
//     warnings (Part 7 §2), produced as the shared Explanation pattern.
//   - TakerPenaltyConversionRate — the designated taker's [0.65, 0.88] rate
//     around the approved 78% baseline (Part 7 §3).
//
// Determinism: each stochastic routine draws from a local, seeded splitmix64
// stream (package rng), identified by (seed, player/club salt). These draws
// are orchestration-level — NOT part of pkg/matchsim's canonical replay
// contract — but a saved world replays them identically because the caller
// derives `seed` deterministically (world seed → fixture seed → per-club seed).
package squad

import (
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Tuning blocks — versioned data, mirroring pkg/matchsim's Tuning pattern so a
// recalibration is a data change, never a code change. "approved" values are
// PM-signed (Part 5/7); everything unmarked is a documented PROPOSAL awaiting
// tuning sign-off.
// ---------------------------------------------------------------------------

// SquadTuning holds the morale/motivation/performance mechanics' proposed
// scale parameters.
type SquadTuning struct {
	// MoraleSpan is the full [0.90, 1.10] swing available to squad morale
	// (approved band; full swing only when sentiment is unanimous/extreme).
	MoraleSpan float64

	// MotivBandLow/MotivBandHigh bound ComputeMotivation's output (approved).
	MotivBandLow  float64
	MotivBandHigh float64
	// AmbitionDragLow scales the ambient drag at ambition 0 (dead rubbers).
	AmbitionDragLow float64
	// HighStakesSpan is the maximum high-stakes lift: motivation =
	// 1.0 + span*(0.5+0.5*ambition), so ambition 0 still floors the base at
	// 1.0+span/2 and ambition 100 tops it at 1.0+span.
	HighStakesSpan float64

	// DeriveFloorSpan scales the rivalry intensity → motivation floor: at
	// intensity 100 the floor is 1.0 + DeriveFloorSpan.
	DeriveFloorSpan float64

	// GiantKillingChance is the approved 10% upset baseline; GrantUnderdogMax
	// is the band top the flip jumps to; AmbitionForUpsetMax gates the roll to
	// genuinely low-ambition clubs; ReputationGapMin defines "significantly
	// higher reputation".
	GiantKillingChance  float64
	GrantUnderdogMax    float64
	AmbitionForUpsetMax float64
	ReputationGapMin    int

	// PerfBandLow/PerfBandHigh bound ComputePlayerPerformanceFactor (approved).
	PerfBandLow  float64
	PerfBandHigh float64
	// ConsistencyMinWidth/ConsistencyMaxWidth bound the personal variance band
	// width across the consistency range [100 → 0].
	ConsistencyMinWidth float64
	ConsistencyMaxWidth float64
	// StakesPressureSpan/StakesTemperamentSpan bound the high-stakes
	// temperament/pressure divergence; SentimentSpan scales the always-on
	// sentiment nudge.
	StakesPressureSpan    float64
	StakesTemperamentSpan float64
	SentimentSpan         float64
}

// ProposedTuning is the v1.2/v1.3 tuning used until PM recalibrates. values
// are proposals (see package doc).
var ProposedTuning = SquadTuning{
	MoraleSpan:            0.10,
	MotivBandLow:          0.85,
	MotivBandHigh:         1.20,
	AmbitionDragLow:       0.85,
	HighStakesSpan:        0.06,
	DeriveFloorSpan:       0.08,
	GiantKillingChance:    0.10, // APPROVED (Part 5 §2)
	GrantUnderdogMax:      1.20,
	AmbitionForUpsetMax:   0.7,
	ReputationGapMin:      15,
	PerfBandLow:           0.85,
	PerfBandHigh:          1.15,
	ConsistencyMinWidth:   0.02,
	ConsistencyMaxWidth:   0.32,
	StakesPressureSpan:    0.06,
	StakesTemperamentSpan: 0.04,
	SentimentSpan:         0.10,
}

// SevereEmotionalThreshold implements PM Part 7 §1: any starter whose current
// sentiment magnitude meets or exceeds this is a key player regardless of
// position or attribute weight.
const SevereEmotionalThreshold = 20

// ---------------------------------------------------------------------------
// Fixture context (shared by ComputeMotivation and ComputePlayerPerformanceFactor)
// ---------------------------------------------------------------------------

// FixtureContext is the stakes summary of one fixture, assembled by
// internal/match at matchday aggregation (derby classification per
// docs/design/derby-rivalry-determination.md). High-stakes status is what lets
// temperament/pressure_handling matter at all (v1.3 §2.8).
type FixtureContext struct {
	IsDerby        bool // rivalry(H,A) exists with intensity >= threshold
	DerbyIntensity int  // [0,100]; meaningful only when IsDerby
	IsSixPointer   bool // relegation/promotion six-pointer
	IsCupTie       bool // knockout tie
	IsDeadRubber   bool // nothing riding on it
	LeagueTier     int  // fixture's league tier (reputation signal, 1 = elite)
	GoldenGoal     bool // knockout format: a level regulation score is decided
}

// IsHighStakes reports whether a fixture carries material stakes — the switch
// that activates temperament/pressure_handling divergence and the motivation
// high-stakes base.
func (fc FixtureContext) IsHighStakes() bool {
	return fc.IsDerby || fc.IsSixPointer || fc.IsCupTie
}

// ---------------------------------------------------------------------------
// Squad morale (v1.2 §2.4)
// ---------------------------------------------------------------------------

// SquadMorale is the team-wide morale multiplier ([0.90, 1.10]).
type SquadMorale struct {
	Rating float64
}

// PlayerMoraleInput is one player's contribution to squad morale.
type PlayerMoraleInput struct {
	PlayerID         uuid.UUID
	Leadership       int // player.player_personality.leadership, [1,100]
	IsLikelyStarter  bool
	CurrentSentiment int // signed: magnitude ≈ emotional intensity, sign = valence
}

// ComputeSquadMorale aggregates sentiment by leadership × starter weight into
// one multiplier. A high-leadership happy player lifts the group more than a
// quiet one; an angry captain drags it down harder. Starters weigh ~2× fringe.
func ComputeSquadMorale(squad []PlayerMoraleInput, t SquadTuning) SquadMorale {
	var weighted, wSum float64
	for _, p := range squad {
		w := 0.5 + float64(clampScore(p.Leadership))/200
		if p.IsLikelyStarter {
			w *= 2
		}
		sent := float64(clampSentiment(p.CurrentSentiment)) / 100
		weighted += w * sent
		wSum += w
	}
	if wSum == 0 {
		return SquadMorale{Rating: 1.0}
	}
	rating := 1.0 + weighted/wSum*t.MoraleSpan
	return SquadMorale{Rating: clamp(rating, 1-t.MoraleSpan, 1+t.MoraleSpan)}
}

// ---------------------------------------------------------------------------
// Motivation / ambition (v1.2 §2.5, Part 5 §2)
// ---------------------------------------------------------------------------

// ClubDNAInput is the competitiveness slice of club DNA the engine reacts to.
type ClubDNAInput struct {
	ClubID              uuid.UUID
	CompetitiveAmbition int // club.club_dna.competitive_ambition, [0,100]
}

// MotivationModifier is a single matchday motivation multiplier.
type MotivationModifier struct {
	ClubID uuid.UUID
	Factor float64
}

// ComputeMotivation resolves a club's motivation for one fixture: an ambient
// drag for low-ambition clubs in low-stakes games, a lift in high-stakes
// games, a rivalry floor, and — for low-ambition underdogs meeting a
// significantly more reputable opponent — the approved 10% giant-killing roll.
// The roll is deterministic: seed is the per-club fixture seed; the club ID
// salts the draw so shared seeds still produce independent rolls.
func ComputeMotivation(dna ClubDNAInput, fc FixtureContext, opponentReputationGap int, seed int64, t SquadTuning) MotivationModifier {
	ambition := clamp(float64(dna.CompetitiveAmbition)/100, 0, 1)

	var f float64
	switch {
	case fc.IsHighStakes():
		// Ambition-moderated lift: an ambitious club rises on the big stages,
		// a coasting one only partially.
		f = 1.0 + t.HighStakesSpan*(0.5+0.5*ambition)
	case fc.IsDeadRubber:
		// Ambient drag: a club that doesn't prioritise winning goes through
		// the motions against nothing-stacking fixtures.
		f = t.AmbitionDragLow + (1-t.AmbitionDragLow)*ambition
	default:
		f = 1.0
	}

	if fc.IsDerby {
		// The rivalry floor guarantees a derby never flatlines, and it beats
		// the ambition-moderated base whenever the intensity is high enough —
		// a genuinely intense rivalry outweighs even low managerial ambition.
		floor := 1.0 + float64(clampScore(fc.DerbyIntensity))*t.DeriveFloorSpan/100
		if f < floor {
			f = floor
		}
	}

	if !fc.IsDeadRubber && ambition < t.AmbitionForUpsetMax &&
		opponentReputationGap >= t.ReputationGapMin &&
		newRNG(seed, hashClub(dna.ClubID)).nextFloat() < t.GiantKillingChance {
		f = t.GrantUnderdogMax
	}

	return MotivationModifier{ClubID: dna.ClubID, Factor: clamp(f, t.MotivBandLow, t.MotivBandHigh)}
}

// ---------------------------------------------------------------------------
// Individual player performance (v1.3 §2.8, Part 7)
// ---------------------------------------------------------------------------

// PlayerHiddenTraitsSnapshot is the engine-relevant slice of
// player.player_hidden_traits.
type PlayerHiddenTraitsSnapshot struct {
	Consistency      int
	Professionalism  int
	Adaptability     int
	Temperament      int
	PressureHandling int
	LearningSpeed    int
}

// SquadMember is the unit the aggregation layer reasons about: the persisted
// player joined with their attribute weight and context flags.
type SquadMember struct {
	PlayerID         uuid.UUID
	Position         string // player.players.primary_position (see ValidPositions)
	IsCaptain        bool
	AttributeWeight  float64 // share of the XI's aggregated Attack/Defense
	Leadership       int
	Hidden           PlayerHiddenTraitsSnapshot
	Attributes       AttributeSnapshot
	CurrentSentiment int // signed
}

// PlayerPerformanceInput is one key player worth varying individually.
type PlayerPerformanceInput struct {
	PlayerID         uuid.UUID
	Consistency      int
	Temperament      int
	PressureHandling int
	Professionalism  int
	CurrentSentiment int
	IsKeyPlayer      bool
}

// PlayerPerformanceFactor is the per-player multiplier ([0.85, 1.15]) applied
// only to that player's own contribution when the XI is aggregated.
type PlayerPerformanceFactor struct {
	PlayerID uuid.UUID
	Factor   float64
}

// ComputePlayerPerformanceFactor determines one player's matchday factor:
//
//  1. a consistency-width chance draw — high consistency keeps Factor near
//     1.0, low consistency swings it wider (seeded, deterministic);
//  2. temperament/pressure_handling divergence ONLY in high-stakes fixtures
//     (a poor-temperament player is not punished for something that hasn't
//     happened in a routine game);
//  3. the player's own sentiment, deliberately unweighted by leadership
//     (that weighting is ComputeSquadMorale's job).
func ComputePlayerPerformanceFactor(in PlayerPerformanceInput, fc FixtureContext, seed int64, t SquadTuning) PlayerPerformanceFactor {
	f := 1.0

	width := t.ConsistencyMinWidth +
		(1-float64(clampScore(in.Consistency))/100)*(t.ConsistencyMaxWidth-t.ConsistencyMinWidth)
	u := newRNG(seed, hashPlayer(in.PlayerID)).nextFloat()
	f += (2*u - 1) * width

	if fc.IsHighStakes() {
		pressure := (float64(clampScore(in.PressureHandling)) - 50) / 50 * t.StakesPressureSpan
		temper := (float64(clampScore(in.Temperament)) - 50) / 50 * t.StakesTemperamentSpan
		f += pressure + temper
	}

	f += float64(clampSentiment(in.CurrentSentiment)) / 100 * t.SentimentSpan

	return PlayerPerformanceFactor{
		PlayerID: in.PlayerID,
		Factor:   clamp(f, t.PerfBandLow, t.PerfBandHigh),
	}
}

// ---------------------------------------------------------------------------
// Key-player selection (Part 7 §1, Part 8)
// ---------------------------------------------------------------------------

// positionPriority anchors the SelectKeyPlayers tie-break: GK and skipper and
// the attacking anchors win ties over rotation options.
var positionPriority = map[string]int{
	"GK": 4, "ST": 3, "AM": 2, "LW": 2, "RW": 2,
	"CM": 1, "DM": 1, "LM": 1, "RM": 1, "CB": 1, "LB": 1, "RB": 1, "": 0,
}

// MinKeyPlayers / MaxKeyPlayers bound the ability-based selection band
// (Part 7 §1: 3–5). Anchors are always taken; the emotional union is NOT
// capped by MaxKeyPlayers because §1 makes every emotionally-severe starter a
// key player regardless of rank.
const (
	MinKeyPlayers = 3
	MaxKeyPlayers = 5
)

// SelectKeyPlayers implements PM Part 7 §1 exactly: the top MinKeyPlayers by
// attribute weight, then every anchor of the quartet (goalkeeper, captain,
// primary striker, playmaker) that is not already in that set promotes into
// it — so the ability-led scan is the primary driver and the anchor roles
// take priority — capped at MaxKeyPlayers. Finally the selection is unioned
// with any starter whose |CurrentSentiment| >= SevereEmotionalThreshold,
// which is NOT capped by MaxKeyPlayers: §1 makes every emotionally-severe
// starter a key player regardless of rank.
func SelectKeyPlayers(xi []SquadMember) []PlayerPerformanceInput {
	ranked := rankByAttributeWeight(xi)

	topN := MinKeyPlayers
	if len(ranked) < topN {
		topN = len(ranked)
	}
	selected := append([]SquadMember{}, ranked[:topN]...)

	for _, a := range []*SquadMember{gkOf(xi), captainOf(xi), bestByWeightInPos(xi, "ST"), bestByWeightInPos(xi, "AM")} {
		if len(selected) >= MaxKeyPlayers {
			break
		}
		if a == nil || contains(selected, a.PlayerID) {
			continue
		}
		selected = append(selected, *a)
	}

	for i := range xi {
		m := &xi[i]
		if abs(m.CurrentSentiment) >= SevereEmotionalThreshold && !contains(selected, m.PlayerID) {
			selected = append(selected, *m)
		}
	}

	out := make([]PlayerPerformanceInput, 0, len(selected))
	for _, m := range selected {
		out = append(out, toPerformanceInput(m))
	}
	return out
}

// gkOf returns the XI's best-bodied goalkeeper anchor.
func gkOf(xi []SquadMember) *SquadMember { return bestByWeightInPos(xi, "GK") }

// captainOf returns the captain anchor — highest leadership, tie-broken by
// professionalism (PickCaptain is the persisted-captain stand-in).
func captainOf(xi []SquadMember) *SquadMember {
	return PickCaptain(xi)
}

// bestByWeightInPos returns the sturdiest member of one position by attribute
// weight, ties broken by anchor priority. Returns nil when the position is
// absent from the XI (an AM-less formation simply has no playmaker anchor).
func bestByWeightInPos(xi []SquadMember, pos string) *SquadMember {
	var best *SquadMember
	for i := range xi {
		m := &xi[i]
		if m.Position != pos {
			continue
		}
		if best == nil || m.AttributeWeight > best.AttributeWeight ||
			(m.AttributeWeight == best.AttributeWeight && priorityScore(*m) > priorityScore(*best)) {
			best = m
		}
	}
	return best
}

func toPerformanceInput(m SquadMember) PlayerPerformanceInput {
	return PlayerPerformanceInput{
		PlayerID:         m.PlayerID,
		Consistency:      m.Hidden.Consistency,
		Temperament:      m.Hidden.Temperament,
		PressureHandling: m.Hidden.PressureHandling,
		Professionalism:  m.Hidden.Professionalism,
		CurrentSentiment: m.CurrentSentiment,
		IsKeyPlayer:      true,
	}
}

// rankByAttributeWeight orders the XI by descending attribute weight, breaking
// ties by anchor priority (GK/Captain/ST/AM). A captain outranks equal-weight
// teammates; the goalkeeper anchors the defence.
func rankByAttributeWeight(xi []SquadMember) []SquadMember {
	ranked := append([]SquadMember{}, xi...)
	// insertion sort: squads are XI-sized, keep it dependency-free
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && fewer(ranked[j-1], ranked[j]); j-- {
			ranked[j-1], ranked[j] = ranked[j], ranked[j-1]
		}
	}
	return ranked
}

// fewer reports whether a ranks strictly below b (a is "fewer" = worse).
func fewer(a, b SquadMember) bool {
	if a.AttributeWeight != b.AttributeWeight {
		return a.AttributeWeight < b.AttributeWeight
	}
	return priorityScore(a) < priorityScore(b)
}

func priorityScore(m SquadMember) int {
	s := positionPriority[m.Position]
	if m.IsCaptain {
		s += 10
	}
	return s
}

// PickCaptain nominates the starter with the highest leadership as captain —
// a deterministic stand-in until a persisted captain concept exists
// (leadership is the schema's leadership signal). Ties fall to professionalism.
func PickCaptain(xi []SquadMember) *SquadMember {
	var best *SquadMember
	for i := range xi {
		m := &xi[i]
		if best == nil ||
			m.Leadership > best.Leadership ||
			(m.Leadership == best.Leadership && m.Hidden.Professionalism > best.Hidden.Professionalism) {
			best = m
		}
	}
	return best
}

func contains(xi []SquadMember, id uuid.UUID) bool {
	for _, m := range xi {
		if m.PlayerID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampScore(v int) int {
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}

// clampSentiment keeps signed sentiment inside [-100, 100].
func clampSentiment(v int) int {
	if v < -100 {
		return -100
	}
	if v > 100 {
		return 100
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
