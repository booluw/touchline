package matchsim

import (
	"fmt"
	"math"
)

// Simulate resolves a full match from the ordered inputs. It is pure: no I/O,
// no wall clock, no global state — the same Options always produce the same
// MatchResult (spec §3–§4).
func Simulate(opts Options) MatchResult {
	tuning := opts.Tuning
	if tuning.Version == "" {
		tuning = DefaultTuning()
	}
	r := newSplitMix64(opts.Seed)

	home := Team{ID: opts.Home.ID, ClubName: opts.Home.ClubName, Ability: clampAbility(opts.Home.Ability)}
	away := Team{ID: opts.Away.ID, ClubName: opts.Away.ClubName, Ability: clampAbility(opts.Away.Ability)}

	res := MatchResult{}
	seq := 0
	emit := func(minute int, typ, clubID, desc string) {
		seq++
		res.Events = append(res.Events, MatchEvent{
			Sequence: seq, Minute: minute, Type: typ, ClubID: clubID, Description: desc,
		})
	}

	ph := possessionShare(home.Ability, away.Ability, tuning.PossessionExponent)

	emit(1, EventKickoff, home.ID,
		fmt.Sprintf("Kick-off! %s get us under way at home.", home.ClubName))

	homeMinutes := 0
	goalWeight := func(att, def Team) float64 {
		m := math.Pow(att.Ability/def.Ability, tuning.GoalAbilityScale)
		m = math.Min(math.Max(m, tuning.GoalMultiplierMin), tuning.GoalMultiplierMax)
		return tuning.GoalWeightBase * m
	}

	for minute := 1; minute <= 90; minute++ {
		// Draw 1: possession.
		attacking, defending := &away, &home
		if r.nextFloat() < ph {
			attacking, defending = &home, &away
			homeMinutes++
		}

		// Draw 2: chance?
		if r.nextFloat() < tuning.ChancesPerMatchMin/90 {
			gw := goalWeight(*attacking, *defending)
			switch resolveChance(r, tuning, gw) {
			case outcomeGoal:
				if attacking.ID == home.ID {
					res.HomeGoals++
				} else {
					res.AwayGoals++
				}
				emit(minute, EventGoal, attacking.ID,
					fmt.Sprintf("GOAL for %s! {player} finishes after a flowing move.", attacking.ClubName))
			case outcomeOnTarget:
				// Draw 2b: surface as a feed event?
				if r.nextFloat() < tuning.ShotFeedFraction {
					emit(minute, EventChance, attacking.ID,
						fmt.Sprintf("Big chance for %s — {player} tests the keeper but can't convert.", attacking.ClubName))
				}
			case outcomeOffTarget, outcomeBlocked:
				// Not surfaced to keep the feed readable.
			case outcomeFoul:
				// Foul won by the attacker; cards come from the card draw.
			}
		}

		// Draw 3: card.
		if r.nextFloat() < tuning.YellowPerMatchMin/90 {
			emit(minute, EventYellowCard, defending.ID,
				fmt.Sprintf("Yellow card for %s after a late challenge.", defending.ClubName))
		}
		if r.nextFloat() < tuning.RedPerMatchMin/90 {
			emit(minute, EventRedCard, defending.ID,
				fmt.Sprintf("Straight red for a %s defender — they'll finish this with ten men.", defending.ClubName))
		}

		// Draw 4: substitutions at canonical windows.
		if contains(tuning.SubWindows, minute) {
			for _, team := range []Team{home, away} {
				if r.nextFloat() < 0.85 {
					emit(minute, EventSubstitution, team.ID,
						fmt.Sprintf("%s make a change: {sub} on for {player}.", team.ClubName))
				}
			}
		}

		if minute == 45 {
			emit(45, EventHalfTime, "",
				fmt.Sprintf("Half-time. %d–%d between %s and %s.", res.HomeGoals, res.AwayGoals, home.ClubName, away.ClubName))
		}
	}

	res.HomePossession = math.Round(float64(homeMinutes) / 90 * 100)
	emit(90, EventFullTime, "",
		fmt.Sprintf("Full-time! %d–%d. %s.", res.HomeGoals, res.AwayGoals, winnerName(res, home, away)))
	return res
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
