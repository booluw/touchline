package matchsim

import (
	"fmt"
	"math"
)

// Simulate resolves a full match from the ordered inputs. It is pure: no I/O,
// no wall clock, no global state — the same Options always produce the same
// MatchResult (spec §3–§4, draw order below).
//
// Canonical draw order (replay contract, EngineVersion "1.6-proposal"): the
// v1.5 style/stamina levers re-weight existing draws and ratings only — they
// never add RNG consumption — so the order below is unchanged from v1.4 and a
// balanced-vs-balanced, fully-fit match is byte-identical to the v1.4 golden
// digest. The "balanced" style is the identity block. v1.6 (attribution,
// addendum) is a strictly post-pass: after full time, each side's lineup casts
// its events to players FROM ITS OWN attribution stream (never the canonical
// one), so the v1.5 feed and digest are untouched when no lineups are present.
//
//	Pre-match:
//	  1-2. variance — one triangular roll per team (home, then away) from
//	                  [VarianceLow, VarianceHigh] centred at 1.0 (§2.3);
//	                  each side seeds its stamina tank from Team.Fitness and
//	                  resolves its style block (addendum v1.5).
//
//	For each minute m in 1..90:
//	  0. tactics   — a manager tactic_change LiveInput (if any) switches that
//	                  side's style for the rest of the match (no RNG).
//	                  Then each side spends (1/90) × StaminaDecay of its tank
//	                  and, from the fatigue window on, refreshes its effective
//	                  ratings against the stamina deficit (§v1.5).
//	  1. possess      — home/away attacker by tuned possession share of the
//	                    combined (Attack+Defense)/2 ratings, scaled by
//	                    Form×Morale×Motivation×variance (home × HomeAdvantage),
//	                    shifted by both styles' PossessionShift.
//	  2. chance?      — attacker creates a chance (ChancesPerMatchMin/90 ×
//	                    style.ChanceVolume)?
//	     a. outcome   — cumulative chance table, goal bucket scaled by
//	                    (att.Attack / def.Defense)^GoalAbilityScale ×
//	                    att.style.GoalConversion × def.style.ConcededConversion.
//	     b. feed?     — on-target shots surface as chance_created (part).
//	     c. foul      — in-box? → penalty decision (referee, §2.6) then
//	                    conversion draw vs PenaltyConversionRate (§2.6+Part 6).
//	  3. foul draw    — defensive foul for the non-possession side, scaled by
//	                    Aggression × RivalryIntensity (no independent card draw).
//	  4. card/injury  — on any foul of the minute: card level (yellow/red,
//	                    thresholds × fouling side's style.CardRate) with a
//	                    borderline-card referee resolution, then an injury draw
//	                    (serious → forced substitution).
//	  5. sub          — at SubWindows only: consume a manager LiveInput
//	                    substitution if present, else the auto-sub draw.
//
// After a red card or substitution the affected side's effective Attack/Defense
// is recomputed for all remaining minutes (PM Part 5 §4). Half/full time are
// synthesized at minutes 45/90 after the minute's draws.
func Simulate(opts Options) MatchResult {
	tuning := opts.Tuning
	if tuning.Version == "" {
		tuning = DefaultTuning()
	}
	r := newSplitMix64(opts.Seed)

	home := normalizeTeam(opts.Home)
	away := normalizeTeam(opts.Away)
	liveInputs := indexLiveInputs(opts.LiveInputs)

	res := MatchResult{}
	seq := 0
	emit := func(minute int, typ, clubID, desc, detail string) {
		seq++
		res.Events = append(res.Events, MatchEvent{
			Sequence: seq, Minute: minute, Type: typ, ClubID: clubID,
			Description: desc, Detail: detail,
		})
	}

	// Pre-match: one variance roll per team (draws 1-2), home first.
	homeVar := triangular(r, tuning.VarianceLow, tuning.VarianceHigh)
	awayVar := triangular(r, tuning.VarianceLow, tuning.VarianceHigh)

	hs := &side{isHome: true}
	as := &side{}
	hs.reset(home, homeVar, tuning)
	as.reset(away, awayVar, tuning)

	emit(1, EventKickoff, home.ID,
		fmt.Sprintf("Kick-off! %s get us under way at home.", home.ClubName), "")

	homeMinutes := 0

	for minute := 1; minute <= 90; minute++ {
		// A live tactic_change input applies the side's new style for the
		// remainder of the match from this minute (v1.5; no RNG consumed).
		if in := liveInputs.tacticAt(minute, hs.team.ID); in != nil {
			hs.style = tuning.styleSpec(in.TacticStyle())
		}
		if in := liveInputs.tacticAt(minute, as.team.ID); in != nil {
			as.style = tuning.styleSpec(in.TacticStyle())
		}

		// Draw 1: possession. Both sides' styles shift the tuned share
		// (possession/gegenpress up, low-block/direct down), then the delta is
		// read back off the ordinary rng draw.
		hs.drain(tuning, minute)
		as.drain(tuning, minute)

		ph := possessionShare(hs.combined(), as.combined(), tuning.PossessionExponent)
		ph = clampShare(ph + hs.style.PossessionShift - as.style.PossessionShift)
		var att, def *side
		if r.nextFloat() < ph {
			att, def = hs, as
			homeMinutes++
		} else {
			att, def = as, hs
		}

		foulThisMinute := false

		// Draw 2: does the attacker create a chance? The side's style scales
		// its arrival rate (gegenpress +25%, low-block −30%, …).
		if r.nextFloat() < tuning.ChancesPerMatchMin/90*att.style.ChanceVolume {
			gw := goalWeight(att.a, def.d, tuning) * att.style.GoalConversion * def.style.ConcededConversion
			switch resolveChance(r, tuning, gw) {
			case outcomeGoal:
				if att.isHome {
					res.HomeGoals++
				} else {
					res.AwayGoals++
				}
				emit(minute, EventGoal, att.team.ID,
					fmt.Sprintf("GOAL for %s! {player} finishes after a flowing move.", att.team.ClubName), "")
				if r.nextFloat() < tuning.AssistFraction {
					emit(minute, EventAssist, att.team.ID,
						fmt.Sprintf("Assist from {assist} for %s — lovely service for {player}.", att.team.ClubName), "")
				}
			case outcomeOnTarget:
				if r.nextFloat() < tuning.ShotFeedFraction {
					emit(minute, EventChance, att.team.ID,
						fmt.Sprintf("Big chance for %s — {player} tests the keeper but can't convert.", att.team.ClubName), "")
				}
			case outcomeFoul:
				// Attacker wins a (defensive) foul, entering the foul
				// consequences of draw 4. In-box fouls additionally go to the
				// referee as a marginal penalty decision (draw 2c).
				foulThisMinute = true
				if r.nextFloat() < tuning.PenaltyInBoxFraction {
					if refereeFlip(r, tuning, att.isHome) {
						// Waved away: reuse chance_created rather than invent
						// a new event type (spec §2.6).
						emit(minute, EventChance, att.team.ID,
							fmt.Sprintf("%s appeal for a penalty is waved away.", att.team.ClubName),
							"The referee has waved away appeals for a penalty there, allowing play to continue.")
					} else {
						emit(minute, EventPenaltyAwarded, att.team.ID,
							fmt.Sprintf("PENALTY to %s!", att.team.ClubName),
							"A marginal in-box call given by the referee.")
						conv := penaltyConversionRate(att.team, tuning)
						if r.nextFloat() < conv {
							if att.isHome {
								res.HomeGoals++
							} else {
								res.AwayGoals++
							}
							emit(minute, EventPenaltyScored, att.team.ID,
								fmt.Sprintf("GOAL! {player} converts the penalty for %s.", att.team.ClubName), "")
						} else {
							emit(minute, EventPenaltyMissed, att.team.ID,
								fmt.Sprintf("{player} misses the penalty for %s — a huge let-off!", att.team.ClubName), "")
						}
					}
				}
			case outcomeOffTarget, outcomeBlocked:
				// Not surfaced to keep the feed readable.
			}
		}

		// Draw 3: defensive foul for the non-possession side, scaled by
		// aggression/rivalry. Cards are NOT an independent per-minute draw —
		// they couple to fouls (draw 4).
		foulP := tuning.FoulBasePerMatchMin / 90 * cardScale(def.team, tuning)
		if r.nextFloat() < foulP {
			foulThisMinute = true
		}

		// Draw 4: foul consequences (card/injury) for any foul this minute.
		if foulThisMinute {
			resolveFoul(r, tuning, def, minute, emit)
		}

		// Draw 5: substitutions only at the canonical windows. A manager
		// LiveInput substitution for this minute replaces the random draw.
		if contains(tuning.SubWindows, minute) {
			for _, sd := range []*side{hs, as} {
				if in := liveInputs.substitutionAt(minute, sd.team.ID); in != nil {
					sd.applySubBoost(tuning)
					emit(minute, EventSubstitution, sd.team.ID,
						fmt.Sprintf("%s make a change from the bench: {sub} on for {player}.", sd.team.ClubName), "")
					continue
				}
				if r.nextFloat() < tuning.SubAutoFraction {
					sd.applySubBoost(tuning)
					emit(minute, EventSubstitution, sd.team.ID,
						fmt.Sprintf("%s make a change: {sub} on for {player}.", sd.team.ClubName), "")
				}
			}
		}

		if minute == 45 {
			emit(45, EventHalfTime, "",
				fmt.Sprintf("Half-time. %d–%d between %s and %s.", res.HomeGoals, res.AwayGoals, home.ClubName, away.ClubName), "")
		}
	}

	res.HomePossession = math.Round(float64(homeMinutes) / 90 * 100)
	emit(90, EventFullTime, "",
		fmt.Sprintf("Full-time! %d–%d. %s.", res.HomeGoals, res.AwayGoals, winnerName(res, home, away)), "")

	// Attribution pass (v1.6, addendum): link castable events to players and
	// derive per-side ratings. Strictly post-canonical draws, so the v1.5 draw
	// order above is never perturbed; skipped entirely for lineup-less callers.
	attributeMatch(opts, tuning, liveInputs, &res)
	return res
}

// side holds a team's live effective Attack/Defense, its active style, and its
// stamina tank. Effective ratings are maintained as base (after red-card and
// substitution effects) × a fatigue effectiveness (the post-75 penalty, v1.5),
// refreshed each minute from tuning.
type side struct {
	team    Team
	isHome  bool
	a, d    float64 // current effective, = base × eff
	baseA   float64 // after red/sub effects, before fatigue
	baseD   float64
	eff     float64 // fatigue effectiveness in [1-FatiguePenaltyMax, 1]
	style   StyleSpec
	stamina float64
}

// reset computes the kickoff effective ratings: Attack/Defense ×
// Form×Morale×Motivation × match-day variance; the HOME side also carries the
// HomeAdvantageFactor multiplier (Concern 1 — applied to ratings feeding both
// possession and goal scaling). The style block and the stamina tank seed from
// the team's tactics and conditioning (addendum v1.5).
func (s *side) reset(t Team, variance float64, tuning Tuning) {
	s.team = t
	f := neutralFactor(t.FormFactor) * neutralFactor(t.MoraleFactor) * neutralFactor(t.MotivationFactor)
	s.a = float64(t.Attack) * f * variance
	s.d = float64(t.Defense) * f * variance
	if s.isHome {
		s.a *= tuning.HomeAdvantageFactor
		s.d *= tuning.HomeAdvantageFactor
	}
	s.baseA, s.baseD = s.a, s.d
	s.eff = 1
	s.style = tuning.styleSpec(t.Tactics.Style)
	s.stamina = clampFitness(t.Fitness)
}

// render recomputes a/d from base × the current fatigue effectiveness.
func (s *side) render(eff float64) {
	s.eff = eff
	s.a = s.baseA * eff
	s.d = s.baseD * eff
}

// drain spends this minute's stamina and — from the tuning's fatigue window —
// refreshes the effectiveness from the side's tank lag: a side holding its
// healthy norm (Fitness 1, balanced style) has deficit 0 all match, so the
// v1.4 path is byte-identical; slower conditioning and faster-decay styles burn
// the tank and pay late.
func (s *side) drain(tuning Tuning, minute int) {
	s.stamina -= (1.0 / 90.0) * s.style.StaminaDecay
	if minute < tuning.FatigueStartMinute {
		return
	}
	deficit := float64(90-minute)/90 - s.stamina
	if deficit < 0 {
		deficit = 0
	}
	eff := 1 - clampPenalty(deficit*tuning.FatiguePenaltyScale, tuning.FatiguePenaltyMax)
	if eff != s.eff {
		s.render(eff)
	}
}

func (s *side) combined() float64 {
	return (s.a + s.d) / 2
}

func (s *side) applySubBoost(tuning Tuning) {
	boost := 1 + tuning.SubStaminaBoost
	s.baseA *= boost
	s.baseD *= boost
	s.stamina = tuning.SubFitness
	s.render(s.eff)
}

// playedShorthanded applies the (approved) red-card degradation for the rest
// of the match: Defense −15%, Attack −25%.
func (s *side) playedShorthanded(tuning Tuning) {
	s.baseA *= 1 - tuning.RedAttackDegrade
	s.baseD *= 1 - tuning.RedDefenseDegrade
	s.render(s.eff)
}

// resolveFoul runs the foul consequences of draw 4: a card coupled to the
// foul (with a borderline-card referee resolution) and a serious-injury draw
// that forces an immediate substitution. The fouling side's style scales the
// card thresholds (tactical-foul coefficients, v1.5).
func resolveFoul(r *splitMix64, tuning Tuning, def *side, minute int, emit emitFn) {
	level := resolveCard(r, tuning, def)
	switch level {
	case cardRed:
		def.playedShorthanded(tuning)
		emit(minute, EventRedCard, def.team.ID,
			fmt.Sprintf("Straight red for a %s player — they'll finish this with ten men.", def.team.ClubName), "")
	case cardYellow:
		emit(minute, EventYellowCard, def.team.ID,
			fmt.Sprintf("Yellow card for %s after a late challenge.", def.team.ClubName), "")
	case cardNone:
		// nothing surfaced; the foul is game management
	}

	if r.nextFloat() < tuning.InjuryOnFoulFraction {
		def.applySubBoost(tuning)
		emit(minute, EventInjury, def.team.ID,
			fmt.Sprintf("{player} comes off injured for %s.", def.team.ClubName), "")
		emit(minute, EventSubstitution, def.team.ID,
			fmt.Sprintf("%s forced into a change: {sub} on for the injured {player}.", def.team.ClubName), "")
	}
}

// Card levels produced by a foul-coupled card draw.
const (
	cardNone = iota
	cardYellow
	cardRed
)

// resolveCard draws a card level for a foul and resolves borderline calls via
// the referee. Aggression × RivalryIntensity scale the base rates (Concern 13);
// the fouling side's style CardRate further scales the thresholds (v1.5).
func resolveCard(r *splitMix64, tuning Tuning, def *side) int {
	scale := cardScale(def.team, tuning) * def.style.CardRate
	redThr := tuning.FoulRedBase * scale
	yellowThr := tuning.FoulYellowBase * scale

	u := r.nextFloat()
	level := cardNone
	switch {
	case u < redThr:
		level = cardRed
	case u < redThr+yellowThr:
		level = cardYellow
	}

	// A draw near a decision boundary is a marginal call for the referee.
	marginal := math.Abs(u-redThr) <= tuning.CardRefereeMargin ||
		math.Abs(u-(redThr+yellowThr)) <= tuning.CardRefereeMargin
	if marginal && refereeFlip(r, tuning, !def.isHome) {
		switch level {
		case cardRed:
			level = cardYellow
		case cardYellow:
			level = cardNone
		default:
			level = cardYellow
		}
	}
	return level
}

// cardScale couples card rates to a side's Aggression and RivalryIntensity.
func cardScale(t Team, tuning Tuning) float64 {
	s := (0.5 + float64(clampAggression(t.Aggression))*tuning.AggressionCardScale) *
		(1 + float64(clampRivalry(t.RivalryIntensity))*tuning.RivalryCardScale)
	if s < 0.5 {
		return 0.5
	}
	if s > 3.0 {
		return 3.0
	}
	return s
}

// refereeFlip returns true when a marginal call is decided "the other way".
// Noise dominates; a directional nudge from RefereeBiasSource counts only when
// the default outcome favours the home club (home_crowd) or the bias source is
// enabled (spec §2.6, PM Part 5 §1).
func refereeFlip(r *splitMix64, tuning Tuning, homeFavors bool) bool {
	p := tuning.RefereeNoiseFactor
	if tuning.RefereeBiasSource == RefereeSourceHomeCrowd {
		if homeFavors {
			p -= tuning.RefereeBiasFactor
		} else {
			p += tuning.RefereeBiasFactor
		}
	}
	if p <= 0 {
		return false
	}
	if p >= 1 {
		return true
	}
	return r.nextFloat() < p
}

// penaltyConversionRate returns a team's designated-taker conversion rate,
// falling back to the 78% baseline when no taker is identifiable (§2.8).
func penaltyConversionRate(t Team, tuning Tuning) float64 {
	if t.PenaltyConversionRate > 0 && t.PenaltyConversionRate <= 1 {
		return t.PenaltyConversionRate
	}
	return tuning.PenaltyConversionBase
}

// goalWeight resolves the ability-scaled goal multiplier for one chance.
func goalWeight(attA, defD float64, tuning Tuning) float64 {
	m := math.Pow(attA/defD, tuning.GoalAbilityScale)
	m = math.Min(math.Max(m, tuning.GoalMultiplierMin), tuning.GoalMultiplierMax)
	return tuning.GoalWeightBase * m
}

// GoalWeight exposes the ability-scaled goal multiplier to orchestration-layer
// callers so the form expectation is computed with the engine's own math — one
// formula, no drift (internal/match mirrors the home-advantage scaling on the
// ratings before calling, exactly as side.reset does).
func GoalWeight(attA, defD float64, tuning Tuning) float64 {
	return goalWeight(attA, defD, tuning)
}

// resolveChance walks the cumulative chance table with an ability-scaled goal
// weight and returns the outcome.
func resolveChance(r *splitMix64, tuning Tuning, goalWeight float64) string {
	w := tuning.OutcomeWeights
	goalW := math.Max(w.Goal*goalWeight, 0.05)
	total := goalW + w.OnTarget + w.OffTarget + w.Blocked + w.Foul
	u := r.nextFloat() * total

	switch {
	case u < goalW:
		return outcomeGoal
	}
	u -= goalW
	switch {
	case u < w.OnTarget:
		return outcomeOnTarget
	}
	u -= w.OnTarget
	switch {
	case u < w.OffTarget:
		return outcomeOffTarget
	}
	u -= w.OffTarget
	switch {
	case u < w.Blocked:
		return outcomeBlocked
	}
	return outcomeFoul
}

// triangular samples the bounded, mode-centred-at-1.0 distribution used for the
// pre-match variance roll (§2.3).
func triangular(r *splitMix64, low, high float64) float64 {
	mid := (low + high) / 2
	cA := mid - low
	bA := high - low
	p := cA / bA
	u := r.nextFloat()
	if u < p {
		return low + math.Sqrt(u*cA*bA)
	}
	return high - math.Sqrt((1-u)*(high-mid)*bA)
}

// normalizeTeam clamps a Team's inputs into their documented domains.
func normalizeTeam(t Team) Team {
	t.Attack = clampRating(t.Attack)
	t.Defense = clampRating(t.Defense)
	if t.Aggression == 0 {
		t.Aggression = 50
	}
	return t
}

func clampAggression(a int) int {
	if a < 0 {
		return 0
	}
	if a > 100 {
		return 100
	}
	return a
}

func clampRivalry(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func winnerName(res MatchResult, home, away Team) string {
	switch {
	case res.HomeGoals > res.AwayGoals:
		return fmt.Sprintf("%s take the three points", home.ClubName)
	case res.AwayGoals > res.HomeGoals:
		return fmt.Sprintf("%s win it away from home", away.ClubName)
	default:
		return "the points are shared"
	}
}
