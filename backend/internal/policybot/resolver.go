// Resolver turns saved policies (or assistant defaults) into the concrete
// command inputs that club-level cores consume. It is pure logic over loaded
// data and never touches the database — the Service orchestrates the load
// and calls through here.
package policybot

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/transfer"
)

// Resolver holds data-access helpers used by the policy resolution layer.
type Resolver struct {
	squad *squad.Store
}

// NewResolver builds the resolver.
func NewResolver(squad *squad.Store) *Resolver {
	return &Resolver{squad: squad}
}

// SquadResult is the resolved XI and optional tactics override that the bot
// writes for a club. Slots are in formation-slot order (0..10) ready for the
// tactics command layer. When SetTactics is true the caller must also call
// SetTacticsForClub.
type SquadResult struct {
	Slots            []tactics.LineupInput
	TacticsStyle     string
	TacticsFormation string
	SetTactics       bool
}

// ResolveSquad loads the club's current squad and turns the squad policy into
// the concrete lineup + optional tactics change. The policybot uses this to
// populate (or update) the lineup before kickoff.
func (r *Resolver) ResolveSquad(ctx context.Context, clubID uuid.UUID, policy SquadPolicy) (SquadResult, error) {
	var res SquadResult

	tacticsRow, err := r.squad.LoadTactics(ctx, clubID)
	if err != nil {
		return res, err
	}
	_, order := squad.ResolveTactics(tacticsRow)

	players, err := r.squad.LoadSquad(ctx, clubID, time.Now())
	if err != nil {
		return res, err
	}
	if len(players) == 0 {
		return res, nil
	}

	playerIDs := make([]uuid.UUID, 0, len(players))
	for _, p := range players {
		playerIDs = append(playerIDs, p.PlayerID)
	}
	conds, err := r.squad.LoadConditions(ctx, playerIDs)
	if err != nil {
		return res, err
	}

	lineup, err := r.squad.LoadLineup(ctx, clubID)
	if err != nil {
		return res, err
	}

	var xi []squad.SquadMember
	switch policy.Rule {
	case RuleRotate:
		xi, err = squad.SelectStartersRotated(players, conds, order)
	case RuleBestFitness:
		xi, err = squad.SelectStartersByFitness(players, conds, order)
	default: // RuleBestEleven
		xi, err = squad.SelectStartersWithLineup(players, lineup, order)
	}
	if err != nil {
		return res, err
	}
	slots := make([]tactics.LineupInput, 0, 11)
	for i, m := range xi {
		slots = append(slots, tactics.LineupInput{Slot: i, PlayerID: m.PlayerID})
	}
	res.Slots = slots

	tacticsStyle := policy.Tactics
	if tacticsStyle == "" || tacticsStyle == TacticsKeep {
		return res, nil
	}
	res.SetTactics = true
	res.TacticsStyle = tacticsStyle
	allowed := squad.AllowedFormations(tacticsStyle)
	if len(allowed) > 0 {
		res.TacticsFormation = allowed[0]
	}
	return res, nil
}

// TrainingArchetype resolves the concrete archetype that training.SubmitPlan
// expects. The assistant default (empty or "dna") maps to club DNA via the
// competitive_ambition dimension (docs/design policybot-numerics.md).
func (r *Resolver) TrainingArchetype(ctx context.Context, clubID uuid.UUID, policy TrainingPolicy) (string, error) {
	arch := policy.Archetype
	if arch != "" && arch != "dna" {
		return arch, nil
	}
	dna, err := r.squad.LoadClubDNA(ctx, clubID)
	if err != nil {
		return "", err
	}
	return dnaArchetype(dna.CompetitiveAmbition), nil
}

// dnaArchetype maps competitive_ambition to a training archetype.
func dnaArchetype(ambition int) string {
	switch {
	case ambition >= 80:
		return "attacking"
	case ambition >= 60:
		return "physical"
	case ambition >= 40:
		return "technical"
	case ambition >= 20:
		return "defensive"
	default:
		return "recovery"
	}
}

// TransferDecision is the sell-side negotiation action the bot takes.
type TransferDecision struct {
	Action string // transfer.RespondAccept | RespondReject | RespondCounter
	Terms  *transfer.Terms
}

// ResolveTransfer evaluates a pending bid against the transfer policy. Only
// seller-side responses are automated; the policy maps fee-vs-valuation
// to accept / reject / counter.
func ResolveTransfer(playerAttrs transfer.PlayerAttrs, bid transfer.Bid, policy TransferPolicy, askingPrice *int64) TransferDecision {
	val := transfer.Valuation(playerAttrs)
	floor := int64(float64(val) * float64(policy.SellFloorPct) / 100.0)
	acceptAbove := int64(float64(val) * float64(policy.AcceptAbovePct) / 100.0)

	if bid.Fee >= acceptAbove {
		return TransferDecision{Action: transfer.RespondAccept}
	}
	if bid.Fee < floor {
		return TransferDecision{Action: transfer.RespondReject}
	}
	if !policy.AutoCounter {
		return TransferDecision{Action: transfer.RespondReject}
	}
	terms := bidCurrentTerms(bid)
	target := counterFee(acceptAbove, askingPrice)
	if target <= bid.Fee {
		return TransferDecision{Action: transfer.RespondAccept}
	}
	terms.Fee = target
	return TransferDecision{Action: transfer.RespondCounter, Terms: &terms}
}

// counterFee sets the counter offer. A bid inside [sellFloor, acceptAbove)
// is countered at the accept threshold (never below it); an explicit asking
// price only ever raises the counter, it never lowers it.
func counterFee(acceptAbove int64, asking *int64) int64 {
	target := acceptAbove
	if asking != nil && *asking > target {
		target = *asking
	}
	return target
}

func bidCurrentTerms(b transfer.Bid) transfer.Terms {
	return transfer.Terms{
		WeeklyWage:           b.WeeklyWage,
		ContractLengthMonths: b.ContractLengthMonths,
		SigningBonus:         b.SigningBonus,
		ReleaseClause:        b.ReleaseClause,
	}
}
