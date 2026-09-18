package personality

import "fmt"

// Engine is the deterministic player-personality reaction engine (S09-01). It
// is PURE and SERVER-AUTHORITATIVE: given an Action + TraitSet it always returns
// the same ReactionResponse with no RNG and no database. Every reaction carries
// an Explanation object linking it back to the exact trait that drove it (the
// acceptance-criteria "clear Explanation objects").
type Engine struct{}

// NewEngine returns a stateless, concurrency-safe engine.
func NewEngine() *Engine { return &Engine{} }

// React runs the full S09-01 action catalogue against a trait set)Skip and
// returns every deterministic reaction plus any hidden-trait reveal that the
// interaction should surface (the gradual-reveal acceptance criteria).
//
// The Tuning matrix below covers the ENTIRE catalogue of management actions
// (user decision #1: all actions, not just the two exemplars):
//
//	missed promise, dropped from the starting XI, long-term bench, wage cut,
//	contract structure approved, transfer bid query, transfer bid blocked,
//	released from the squad, training load raised, motivational/critical team
//	talk.
func (e *Engine) React(action Action, t TraitSet) (ReactionResponse, error) {
	rr := ReactionResponse{Action: action}
	switch action {
	case ActionMissedPromise:
		rr.Reactions = []Reaction{e.reactMissedPromise(t)}
	case ActionDroppedFromStartingXI:
		rr.Reactions = []Reaction{e.reactDroppedFromXI(t, false)}
	case ActionBenchedLongTerm:
		rr.Reactions = []Reaction{e.reactDroppedFromXI(t, true)}
	case ActionWageCutOffered:
		rr.Reactions = []Reaction{e.reactWageCut(t)}
	case ActionContractApproved:
		rr.Reactions = []Reaction{e.reactContractStructure(t)}
	case ActionTransferBidQuery:
		rr.Reactions = []Reaction{e.reactTransferQuery(t)}
	case ActionTransferBidBlocked:
		rr.Reactions = []Reaction{e.reactTransferBlocked(t)}
	case ActionReleasedFromSquad:
		rr.Reactions = []Reaction{e.reactReleased(t)}
		rr.Revealed = e.maybeRevealPotential(t, "released_from_squad")
	case ActionTrainingLoadIncreased:
		rr.Reactions = []Reaction{e.reactTrainingLoad(t)}
	case ActionTeamTalkMotivational:
		rr.Reactions = []Reaction{e.reactTeamTalk(t, true)}
	case ActionTeamTalkCritical:
		rr.Reactions = []Reaction{e.reactTeamTalk(t, false)}
	default:
		// ActionResearchDenied is a reserved seam — the research room is a
		// documented "not built yet" backlog (OPENCODE), so it does not yet
		// resolve to a deterministic reaction set. Keep the engine total over
		// everything else.
		return ReactionResponse{}, fmt.Errorf("personality: unknown action %q (research-room actions are a reserved seam)", action)
	}
	return rr, nil
}

// explain builds the acceptance-criteria Explanation object for one trait.
func (e *Engine) explain(trait string, value int, text string) Explanation {
	return Explanation{Trait: trait, Value: value, Text: text}
}

// reactMissedPromise — acceptance-exemplar 1: a promise that was broken reads
// straight off volatility/ego before professionalism tries to paper over it.
func (e *Engine) reactMissedPromise(t TraitSet) Reaction {
	if t.Volatility >= 7 {
		return Reaction{Emotion: EmoAngry, Severity: 5,
			Explanation: e.explain("volatility", t.Volatility, "A volatile player reads a broken promise as a personal attack and escalates immediately.")}
	}
	if t.Ego >= 9 {
		return Reaction{Emotion: EmoBetrayed, Severity: 5,
			Explanation: e.explain("ego", t.Ego, "High ego sees a broken promise as disrespect, not negotiation — trust snaps.")}
	}
	if t.Loyalty >= 8 {
		return Reaction{Emotion: EmoFrustrated, Severity: 2,
			Explanation: e.explain("loyalty", t.Loyalty, "A loyal player is wounded but gives the manager the benefit of the doubt.")}
	}
	return Reaction{Emotion: EmoFrustrated, Severity: 2,
		Explanation: e.explain("professionalism", t.Professionalism, "A professional files the miss but keeps training.")}
}

// reactDroppedFromXI — benching touches ego/volatility and, for the driven,
// ambition. longTerm = repeated bench (stronger reaction).
func (e *Engine) reactDroppedFromXI(t TraitSet, longTerm bool) Reaction {
	sev := 2
	if longTerm {
		sev = 4
	}
	if t.Ego >= 8 {
		return Reaction{Emotion: EmoAngry, Severity: sev,
			Explanation: e.explain("ego", t.Ego, "High ego treats a demotion from the XI as a slight to the self-image.")}
	}
	if t.Volatility >= 6 {
		return Reaction{Emotion: EmoFrustrated, Severity: sev,
			Explanation: e.explain("volatility", t.Volatility, "A volatile player cannot sit on a bench quietly — the mood curdles.")}
	}
	if t.Ambition >= 7 {
		return Reaction{Emotion: EmoMotivated, Severity: 2,
			Explanation: e.explain("ambition", t.Ambition, "High ambition turns a dropped spell into a hunger to force the way back in.")}
	}
	if t.Patience >= 7 {
		return Reaction{Emotion: EmoContent, Severity: 1,
			Explanation: e.explain("patience", t.Patience, "A patient player accepts the tactical decision and waits for the window to reopen.")}
	}
	return Reaction{Emotion: EmoFrustrated, Severity: 1,
		Explanation: e.explain("professionalism", t.Professionalism, "A professional swallows the decision and trains on.")}
}

// reactWageCut — loyalty absorbs a pay reduction; ambition resents it as the
// club signalling no hunger.
func (e *Engine) reactWageCut(t TraitSet) Reaction {
	if t.Loyalty >= 8 {
		return Reaction{Emotion: EmoContent, Severity: 2,
			Explanation: e.explain("loyalty", t.Loyalty, "High loyalty accepts a wage cut as the price of staying part of the project.")}
	}
	if t.Ambition >= 8 {
		return Reaction{Emotion: EmoAngry, Severity: 4,
			Explanation: e.explain("ambition", t.Ambition, "A highly ambitious player reads the cut as a ceiling being lowered and reacts badly.")}
	}
	if t.Professionalism >= 7 {
		return Reaction{Emotion: EmoFrustrated, Severity: 1,
			Explanation: e.explain("professionalism", t.Professionalism, "A professional grumbles but understands the books.")}
	}
	return Reaction{Emotion: EmoAngry, Severity: 2,
		Explanation: e.explain("ego", t.Ego, "Low trust in the club's finances makes the cut feel like an insult.")}
}

// reactContractStructure — a contract structure that matches a loyal/ambitious
// or professional profile is accepted warmly (acceptance exemplar).
func (e *Engine) reactContractStructure(t TraitSet) Reaction {
	if t.Loyalty >= 7 {
		return Reaction{Emotion: EmoHappy, Severity: 1,
			Explanation: e.explain("loyalty", t.Loyalty, "A player with high loyalty welcomes a contract that puts the club's ambition first.")}
	}
	if t.Ambition >= 7 {
		return Reaction{Emotion: EmoMotivated, Severity: 1,
			Explanation: e.explain("ambition", t.Ambition, "An ambitious player signs readily when the structure rewards performance growth.")}
	}
	if t.Professionalism >= 7 {
		return Reaction{Emotion: EmoContent, Severity: 1,
			Explanation: e.explain("professionalism", t.Professionalism, "A professional approves the structure on its merits.")}
	}
	return Reaction{Emotion: EmoContent, Severity: 1,
		Explanation: e.explain("patience", t.Patience, "An easy-going player accepts the paperwork without drama.")}
}

// reactTransferQuery — ambitious players lean toward a move; loyal ones stay.
func (e *Engine) reactTransferQuery(t TraitSet) Reaction {
	if t.Loyalty >= 8 {
		return Reaction{Emotion: EmoContent, Severity: 1,
			Explanation: e.explain("loyalty", t.Loyalty, "A loyal player hears the bid and says the project keeps them grounded.")}
	}
	if t.Ambition >= 8 {
		return Reaction{Emotion: EmoMotivated, Severity: 1,
			Explanation: e.explain("ambition", t.Ambition, "High ambition sees the query as a step toward a bigger stage.")}
	}
	if t.Adaptability >= 7 {
		return Reaction{Emotion: EmoHappy, Severity: 1,
			Explanation: e.explain("adaptability", t.Adaptability, "An adaptable player is comfortable weighing a new club and new league.")}
	}
	return Reaction{Emotion: EmoContent, Severity: 1,
		Explanation: e.explain("professionalism", t.Professionalism, "A professional keeps an open mind and lets the club decide.")}
}

// reactTransferBlocked — blocking a move frustrates ambition; loyalty accepts.
func (e *Engine) reactTransferBlocked(t TraitSet) Reaction {
	if t.Ambition >= 8 {
		return Reaction{Emotion: EmoAngry, Severity: 4,
			Explanation: e.explain("ambition", t.Ambition, "Blocking a move a high-ambition player wanted reads as the club capping their career.")}
	}
	if t.Volatility >= 7 {
		return Reaction{Emotion: EmoAngry, Severity: 3,
			Explanation: e.explain("volatility", t.Volatility, "A volatile player resents the veto and lets it be known loudly.")}
	}
	if t.Loyalty >= 7 {
		return Reaction{Emotion: EmoContent, Severity: 1,
			Explanation: e.explain("loyalty", t.Loyalty, "A loyal player accepts the block and refocuses on the season.")}
	}
	return Reaction{Emotion: EmoFrustrated, Severity: 1,
		Explanation: e.explain("patience", t.Patience, "The player accepts the block in the short term.")}
}

// reactReleased — a release is the strongest ego/adaptability pressure point and
// the most likely place a hidden-trait reveal leaks out (see maybeRevealPotential).
func (e *Engine) reactReleased(t TraitSet) Reaction {
	if t.Ego >= 8 {
		return Reaction{Emotion: EmoAngry, Severity: 5,
			Explanation: e.explain("ego", t.Ego, "High ego treats a release as the ultimate slight — self-image can't absorb it.")}
	}
	if t.Adaptability >= 8 {
		return Reaction{Emotion: EmoMotivated, Severity: 1,
			Explanation: e.explain("adaptability", t.Adaptability, "High adaptability reframes a release as a fresh market and stays hungry.")}
	}
	if t.Professionalism >= 7 {
		return Reaction{Emotion: EmoContent, Severity: 2,
			Explanation: e.explain("professionalism", t.Professionalism, "A professional takes the release on the chin and keeps the career moving.")}
	}
	return Reaction{Emotion: EmoFrustrated, Severity: 3,
		Explanation: e.explain("volatility", t.Volatility, "Without a stabilising trait the release lands as open frustration.")}
}

// reactTrainingLoad — professionalism and ambition absorb an increased load
// (the training-influence seam's data source; see influence.go).
func (e *Engine) reactTrainingLoad(t TraitSet) Reaction {
	if t.Professionalism >= 7 {
		return Reaction{Emotion: EmoMotivated, Severity: 1,
			Explanation: e.explain("professionalism", t.Professionalism, "A professional treats the added load as growth, not punishment.")}
	}
	if t.Ambition >= 8 {
		return Reaction{Emotion: EmoMotivated, Severity: 1,
			Explanation: e.explain("ambition", t.Ambition, "An ambitious player is hungry for the extra work.")}
	}
	if t.Volatility >= 6 {
		return Reaction{Emotion: EmoFrustrated, Severity: 2,
			Explanation: e.explain("volatility", t.Volatility, "A volatile player reads the extra load as being singled out.")}
	}
	return Reaction{Emotion: EmoContent, Severity: 1,
		Explanation: e.explain("patience", t.Patience, "An easy-going player takes the extra load in stride.")}
}

// reactTeamTalk — motivational talks land on ambition/sociability; critical
// talks land on volatility or translate into fuel for the professional.
func (e *Engine) reactTeamTalk(t TraitSet, motivational bool) Reaction {
	if motivational {
		if t.Ambition >= 7 {
			return Reaction{Emotion: EmoMotivated, Severity: 2,
				Explanation: e.explain("ambition", t.Ambition, "A motivational talk feeds straight into a high ambition.")}
		}
		if t.Sociability >= 7 {
			return Reaction{Emotion: EmoContent, Severity: 1,
				Explanation: e.explain("sociability", t.Sociability, "A sociable player responds well to the group being lifted.")}
		}
		return Reaction{Emotion: EmoContent, Severity: 1,
			Explanation: e.explain("professionalism", t.Professionalism, "A professional takes the motivation on board without drama.")}
	}
	if t.Volatility >= 9 {
		return Reaction{Emotion: EmoAngry, Severity: 3,
			Explanation: e.explain("volatility", t.Volatility, "A volatile player crosses the line when criticised in public.")}
	}
	if t.Ambition >= 8 {
		return Reaction{Emotion: EmoMotivated, Severity: 2,
			Explanation: e.explain("ambition", t.Ambition, "Hard criticism lands as fuel on a hungry ambition.")}
	}
	return Reaction{Emotion: EmoFrustrated, Severity: 1,
		Explanation: e.explain("professionalism", t.Professionalism, "A professional digests criticism and refocuses.")}
}

// maybeRevealPotential is the gradual hidden-trait reveal: the first time a
// release (or other high-pressure interaction) interacts with an UNLOCKED
// potential, the engine surfaces a TraitReveal so the manager learns the
// hidden trait's existence last — the acceptance criteria require reveals to
// be gradual and evidence-based, never a nuked dump.
func (e *Engine) maybeRevealPotential(t TraitSet, source string) *TraitReveal {
	if !t.PotentialLocked && t.Potential >= 9 {
		return &TraitReveal{
			PlayerID:   t.PlayerID,
			Trait:      "potential",
			Source:     source,
			Confidence: 40,
			Text:       "Through this interaction a scout whispered the player's ceiling is big — a hidden trait surfaces with low confidence.",
		}
	}
	return nil
}
