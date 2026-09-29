package match

import (
	"context"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/matchsim"
)

// ---------------------------------------------------------------------------
// Team assembly
// ---------------------------------------------------------------------------

// teamPlan is everything PlayFixture needs after building one side: the XI for
// casting, the team handed to the engine, and the pre-match state for form.
type teamPlan struct {
	club      squad.ClubRow
	dna       squad.ClubDNAInput
	xi        []squad.SquadMember
	bench     []squad.SquadMember
	taker     *squad.SquadMember
	warnings  []squad.LineupWarning
	team      matchsim.Team
	formState form.FormState
	isHome    bool
	worldTick int64
}

// buildTeam assembles one side: XI selection, the aggregation pipeline
// (morale → motivation → key players → performance factors → ratings → taker),
// and the engine Team. oppRep is the opponent's persisted reputation (drives
// the giant-killing gate in ComputeMotivation).
func (s *Service) buildTeam(ctx context.Context, f *Fixture, club squad.ClubRow, oppRep int, tick int64, fc squad.FixtureContext, seed int64, t matchsim.Tuning) (*teamPlan, error) {
	p := &teamPlan{club: club, isHome: club.ID == f.HomeClub.ID, worldTick: tick}

	dna, err := s.squad.LoadClubDNA(ctx, club.ID)
	if err != nil {
		return nil, err
	}
	p.dna = dna

	players, err := s.squad.LoadSquad(ctx, club.ID, f.ScheduledAt)
	if err != nil {
		return nil, err
	}

	var xi []squad.SquadMember
	tactics, err := s.squad.LoadTactics(ctx, club.ID)
	if err != nil {
		return nil, err
	}
	style, order := squad.ResolveTactics(tactics)
	if club.IsAIControlled {
		xi = squad.SelectStartersForAI(players, seed, club.ID, order)
	} else {
		lineup, err := s.squad.LoadLineup(ctx, club.ID)
		if err != nil {
			return nil, err
		}
		xi, err = squad.SelectStartersWithLineup(players, lineup, order)
		if err != nil {
			return nil, err
		}
	}
	p.xi = xi
	p.bench = benchFrom(players, xi)
	p.taker = chooseTaker(xi, penaltiesOf(players))

	squTuning := squad.ProposedTuning
	morale := squad.ComputeSquadMorale(moraleInputs(xi), squTuning)
	mot := squad.ComputeMotivation(dna, fc, oppRep-club.Reputation, seed, squTuning)

	// Condition folds every XI member's sharpness/fatigue into their matchday
	// contribution (S05-01); key players layer the performance factor on top.
	conds, err := s.squad.LoadConditions(ctx, squad.PlayerIDs(xi))
	if err != nil {
		return nil, err
	}
	factorMap := make(map[uuid.UUID]float64, len(xi))
	for _, m := range xi {
		factorMap[m.PlayerID] = squad.ConditionFactor(conds[m.PlayerID])
	}
	for _, in := range squad.SelectKeyPlayers(xi) {
		pf := squad.ComputePlayerPerformanceFactor(in, fc, seed, squTuning)
		factorMap[in.PlayerID] *= pf.Factor
		if w, ok := squad.LineupWarningFor(in, fc, pf, seed, squTuning, 0); ok {
			p.warnings = append(p.warnings, w)
		}
	}
	// The style's recipe skew reaches the ratings (possession = technical/
	// mental-led, gegenpress = physical-press, low-block = defensive-tactical,
	// direct = physical-attack); balanced uses the default recipe.
	weights := squad.WeightsForStyle(style, squad.DefaultPositionWeights)
	att, def := squad.BuildSquadRatings(xi, weights, factorMap)

	takerRate := 0.0
	if p.taker != nil {
		takerRate = squad.TakerPenaltyConversionRate(p.taker.Hidden, p.taker.CurrentSentiment)
	}

	fs, ok, err := s.form.Get(ctx, club.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		fs = form.Neutral(club.ID, tick)
	}
	p.formState = fs

	p.team = matchsim.Team{
		ID:                    club.ID.String(),
		ClubName:              club.Name,
		Attack:                att,
		Defense:               def,
		FormFactor:            fs.CurrentRating,
		MoraleFactor:          morale.Rating,
		MotivationFactor:      mot.Factor,
		Aggression:            50, // generated clubs have no DNA aggression column (engine normalises 0 the same)
		RivalryIntensity:      fc.DerbyIntensity,
		PenaltyConversionRate: takerRate,
		Tactics:               matchsim.Tactics{Style: style},
		Fitness:               squad.XIFitness(conds, xi),
	}
	return p, nil
}

// sortBench / helpers are in selection helpers below.

// benchFrom collects the top-5 available non-starters by attribute weight as
// the substitution pool the caster draws on (highest weight comes on first).
func benchFrom(players []squad.LoadedPlayer, xi []squad.SquadMember) []squad.SquadMember {
	inXI := make(map[uuid.UUID]bool, len(xi))
	for _, m := range xi {
		inXI[m.PlayerID] = true
	}
	var bench []squad.LoadedPlayer
	for _, p := range players {
		if p.Available && !inXI[p.PlayerID] {
			bench = append(bench, p)
		}
	}
	for i := 1; i < len(bench); i++ {
		for j := i; j > 0 && squad.MemberWeight(bench[j-1]) < squad.MemberWeight(bench[j]); j-- {
			bench[j-1], bench[j] = bench[j], bench[j-1]
		}
	}
	if len(bench) > 5 {
		bench = bench[:5]
	}
	out := make([]squad.SquadMember, 0, len(bench))
	for _, p := range bench {
		out = append(out, squad.ToSquadMember(p))
	}
	return out
}

// moraleInputs turns the XI into ComputeSquadMorale's weighted inputs.
func moraleInputs(xi []squad.SquadMember) []squad.PlayerMoraleInput {
	out := make([]squad.PlayerMoraleInput, 0, len(xi))
	for _, m := range xi {
		out = append(out, squad.PlayerMoraleInput{
			PlayerID:         m.PlayerID,
			Leadership:       m.Leadership,
			IsLikelyStarter:  true,
			CurrentSentiment: m.CurrentSentiment,
		})
	}
	return out
}

func penaltiesOf(players []squad.LoadedPlayer) map[uuid.UUID]int {
	out := make(map[uuid.UUID]int, len(players))
	for _, p := range players {
		out[p.PlayerID] = p.Penalties
	}
	return out
}

// chooseTaker designates the penalty taker: the highest penalties attribute in
// the XI, falling back to the captain, and finally nil (engine baseline 78%).
func chooseTaker(xi []squad.SquadMember, pen map[uuid.UUID]int) *squad.SquadMember {
	if len(xi) == 0 {
		return nil
	}
	best := xi[0]
	for _, m := range xi {
		if pen[m.PlayerID] > pen[best.PlayerID] {
			best = m
		}
	}
	if pen[best.PlayerID] <= 0 {
		if c := squad.PickCaptain(xi); c != nil {
			return c
		}
	}
	return &best
}
