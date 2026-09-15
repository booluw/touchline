package player

import "math"

// All decision numbers in this file are PROPOSAL data until PM tuning sign-off
// (source of truth: docs/design/morale-numerics.md). Recalibration means
// editing constants here, never steering logic.
const (
	// FreshStartMorale is the morale a player gets the moment they join a new
	// club (transfer completion), regardless of their previous state. Playing
	// time resets with it — the whole-season share starts over at 0.
	FreshStartMorale = 0.85

	// Whole-season minute shares each agreed squad role expects, as a fraction
	// of the 90-minute slot (player minutes ÷ (90 × club matches played)).
	RoleExpectedShareKeyPlayer   = 0.75 // plays ~3 of every 4 matches
	RoleExpectedShareRotation    = 0.45 // roughly every other match
	RoleExpectedShareSquadPlayer = 0.20 // occasional minutes
	RoleExpectedShareDevelopment = 0.0  // no expectation — no pressure

	// Morale targets by satisfaction band (post-match pull targets).
	MoraleTargetSatisfied = 0.65
	MoraleTargetNeutral   = 0.50
	MoraleNeutralBaseline = 0.50
	MoraleTargetUnhappy   = 0.35

	// Personality influence steps, applied as step × (personality − 50) / 50
	// when grading a real shortfall.
	AmbitionMoraleStep = 0.08 // ambitious players punish a bench harder
	EgoMoraleStep      = 0.08 // star ego amplifies unhappiness when benched
	PatienceMoraleStep = 0.06 // patient players tolerate a quiet spell
	LoyaltyMoraleStep  = 0.06 // loyal players stay content longer

	// Post-match swing: morale moves alpha × (target − morale). Alpha is
	// scaled by emotional_volatility (bigger swings) and dampened by
	// professionalism (stable temperament).
	MoraleSwingAlpha          = 0.35 // alpha at a league-average temperament
	VolatilitySwingScale      = 1.5  // alpha multiplier at volatility = 100
	ProfessionalismSwingScale = 0.5  // alpha multiplier at professionalism = 100

	// Weekly recovery: morale regresses toward neutral at alpha × (0.5 −
	// morale). More experienced pros recover faster.
	WeeklyRecoveryAlpha = 0.10 // alpha at professionalism 50
	RecoveryAtPro0      = 0.5  // alpha multiplier at professionalism 0
	RecoveryAtPro100    = 2.0  // alpha multiplier at professionalism 100

	// Transfer-request thresholds and lifetimes.
	UnhappyMoraleThreshold   = 0.35 // morale at/below this can trigger a request
	TransferRequestDeepShare = 0.5  // share below 0.5 × expected counts as a real shortfall
	AutoListTTLWorldDays     = 21   // pending beyond this auto-list the player
	DenyCooldownDays         = 28   // after a denial the player stays quiet this long
	ReassureCooldownDays     = 28   // after a reassure, same window before re-requesting
	PromiseEvaluationWeeks   = 4    // after this, an unmet promise is judged broken
	DenyMoraleDrop           = 0.10 // the morale hit of having a request denied

	// Relationship sentiment deltas recorded on the player↔manager journal.
	SentimentTransferApproved = 15
	SentimentTransferDenied   = -25
	SentimentReassured        = 0
	SentimentPromiseKept      = 10
	SentimentPromiseBroken    = -30
)

// ExpectedShareForRole returns the whole-season minute share a squad role is
// entitled to. Unknown/empty roles degrade to the neutral squad_player share.
func ExpectedShareForRole(role string) float64 {
	switch role {
	case SquadRoleKeyPlayer:
		return RoleExpectedShareKeyPlayer
	case SquadRoleRotation:
		return RoleExpectedShareRotation
	case SquadRoleDevelopment:
		return RoleExpectedShareDevelopment
	default:
		return RoleExpectedShareSquadPlayer
	}
}

// SquadRoleFromExpectation maps the free-text playing_time_expectation
// preference to an agreed squad role (used to fill NULL contracts.squad_role
// on the weekly pass; "" is returned when nothing matches).
func SquadRoleFromExpectation(v string) string {
	switch {
	case containsFold(v, "key player"), containsFold(v, "first team"),
		containsFold(v, "regular starter"), containsFold(v, "star player"):
		return SquadRoleKeyPlayer
	case containsFold(v, "rotation"), containsFold(v, "impact"):
		return SquadRoleRotation
	case containsFold(v, "development"), containsFold(v, "youth"), containsFold(v, "prospect"):
		return SquadRoleDevelopment
	case containsFold(v, "squad"), containsFold(v, "backup"), containsFold(v, "depth"):
		return SquadRoleSquadPlayer
	}
	return ""
}

// moraleTarget picks the pull target for a player after a match, given their
// whole-season share vs their role's expected share.
func moraleTarget(share, expected float64, p PlayerPersonality) float64 {
	if expected <= 0 {
		// Development role: no expectation, no pressure — stays satisfied.
		return MoraleTargetSatisfied
	}
	ratio := share / expected
	if ratio >= 1 {
		return MoraleTargetSatisfied
	}
	if ratio >= 0.5 {
		return MoraleTargetNeutral
	}
	unhappy := MoraleTargetUnhappy +
		PatienceMoraleStep*float64(p.Patience-50)/50 +
		LoyaltyMoraleStep*float64(p.Loyalty-50)/50 -
		AmbitionMoraleStep*float64(p.Ambition-50)/50 -
		EgoMoraleStep*float64(p.Ego-50)/50
	return round4(clamp01(unhappy, 0.05, MoraleTargetNeutral-0.05))
}

// swingAlpha scales the post-match update by volatility (more movement) and
// dampens it with professionalism (stable temperament).
func swingAlpha(p PlayerPersonality) float64 {
	vol := 1.0 + (VolatilitySwingScale-1.0)*float64(clampInt(p.EmotionalVolatility, 0, 100))/100
	pro := 1.0 - (1.0-ProfessionalismSwingScale)*float64(clampInt(p.Professionalism, 0, 100))/100
	return MoraleSwingAlpha * vol * pro
}

// UpdateMorale pulls a player's morale toward their satisfaction target after
// a match. Bounded to [0,1].
func UpdateMorale(current, share, expected float64, p PlayerPersonality) float64 {
	target := moraleTarget(share, expected, p)
	return round4(current + swingAlpha(p)*(target-current))
}

// WeeklyRecovery regresses morale toward neutral at a professionalism-
// weighted rate. Bounded to [0,1].
func WeeklyRecovery(current float64, professionalism int) float64 {
	pro := clampInt(professionalism, 0, 100)
	f := RecoveryAtPro0 + (RecoveryAtPro100-RecoveryAtPro0)*float64(pro)/100
	alpha := WeeklyRecoveryAlpha * f
	return round4(current + alpha*(MoraleNeutralBaseline-current))
}

// PlayingTimeRatio converts aggregated minutes and the club's played-match
// count into the whole-season share (player minutes ÷ 90 × matches), capped
// at 1. A club with no recorded matches yields 0.
func PlayingTimeRatio(minutes, matches int) float64 {
	if matches <= 0 || minutes <= 0 {
		return 0
	}
	return clamp01(float64(minutes)/(90*float64(matches)), 0, 1)
}

// Satisfied reports whether a player's whole-season share meets their role's
// expectation.
func Satisfied(share, expected float64) bool {
	return expected <= 0 || share >= expected
}

// DeepShortfall reports whether the share sits clearly below the role's
// expectation (below half), the state that can turn into a transfer request.
func DeepShortfall(share, expected float64) bool {
	return expected > 0 && share < TransferRequestDeepShare*expected
}

// SatisfiedTargetFor returns the satisfied-pull target a player would have if
// their share met their role exactly (detail-view helper, personality-neutral).
func SatisfiedTargetFor(expected float64) float64 {
	return moraleTarget(expected, expected, PlayerPersonality{})
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func clamp01(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------- tiny case-insensitive plain-text helpers ----------

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	h := len(s) - len(sub)
	if h < 0 {
		return -1
	}
	for i := 0; i <= h; i++ {
		if equalsFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalsFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := foldByte(a[i]), foldByte(b[i])
		if ca != cb {
			return false
		}
	}
	return true
}

func foldByte(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
