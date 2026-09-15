package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// fetchPersona reads the stored board persona (and DNA for the profile cache).
func (s *Service) fetchPersona(ctx context.Context, q querier, clubID uuid.UUID) (Persona, int, int, error) {
	var persona Persona
	var ambition, patience *int
	err := q.QueryRow(ctx, `
		SELECT b.personality_type, d.competitive_ambition, d.patience
		FROM club.boards b
		LEFT JOIN club.club_dna d ON d.club_id = b.club_id
		WHERE b.club_id = $1`, clubID).Scan(&persona, &ambition, &patience)
	if err != nil {
		return PersonaPatientOwner, 50, 50, nil
	}
	a, p := 50, 50
	if ambition != nil {
		a = *ambition
	}
	if patience != nil {
		p = *patience
	}
	return persona, a, p, nil
}

// reviewProgress converts played fixtures into a 0-1 season progress used to
// scale mandate evaluation.
func reviewProgress(played, total int) float64 {
	if total <= 0 {
		return 1
	}
	p := float64(played) / float64(total)
	if p > 1 {
		return 1
	}
	return p
}

// evaluateAndRecord grades the club's mandates, computes the seven factor
// scores and their weighted total, snapshots the result, and emits the weekly
// review plus per-mandate resolution events. Must run inside the review tx.
func (s *Service) evaluateAndRecord(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, in reviewInputs, finish, points int, tick int64) (FactorScores, *explanation.Explanation, error) {
	persona, _, _, err := s.fetchPersona(ctx, tx, in.clubID)
	if err != nil {
		return FactorScores{}, nil, err
	}

	met, broken := 0, 0
	metStrategic, brokenStrategic, metFinancial, brokenFinancial := 0, 0, 0, 0
	progress := reviewProgress(in.played, in.totalMatches)

	for i := range in.mandates {
		m := &in.mandates[i]
		if m.Status != MandatePending && m.Status != MandateAgreed {
			continue
		}
		var mMet, mBroken bool
		switch m.TargetType {
		case TargetLeagueFinish:
			if in.hasLeague {
				mMet, mBroken = evaluateFinish(*in.position, finish, progress, in.seasonComplete)
			}
		case TargetPointsTarget:
			if in.hasLeague {
				mMet, mBroken = evaluatePoints(*in.points, points, progress, in.seasonComplete)
			}
		case TargetWageStructure:
			mMet, mBroken = evaluateWageStructure(in.committedAnnual, in.wageBudget, parsePP(m))
		case TargetOperatingBank:
			mMet, mBroken = evaluateOperatingBalance(in.operatingProfit, in.wageBudget)
		}
		if !mMet && !mBroken {
			continue
		}
		status := MandateMet
		eventType := EventMandateMet
		if mBroken {
			status = MandateBroken
			eventType = EventMandateBroken
		}
		if err := s.store.resolveMandate(ctx, tx, m.ID, status); err != nil {
			return FactorScores{}, nil, err
		}
		if mMet {
			met++
			switch m.Category {
			case CatStrategic:
				metStrategic++
			case CatFinancial:
				metFinancial++
			}
		} else {
			broken++
			switch m.Category {
			case CatStrategic:
				brokenStrategic++
			case CatFinancial:
				brokenFinancial++
			}
		}
		mandateExp := explanation.New("board_mandate", 0).
			Add(fmt.Sprintf("%s mandate %s", m.TargetType, status), 0)
		if err := s.recordBoardEvent(ctx, tx, worldID, tick, "system", uuid.Nil, eventType, mandateExp); err != nil {
			return FactorScores{}, nil, err
		}
	}

	scores := FactorScores{
		Performance:        s.perf(in, finish),
		Expectations:       expectationsScore(met, broken),
		Financial:          financialScore(in.operatingProfit, in.wageBudget, in.committedAnnual),
		BoardRelationship:  relationshipScore(met, broken, in.patience),
		ClubDNAAlignment:   dnaAlignmentScore(metStrategic, metFinancial, brokenStrategic, brokenFinancial),
		SupporterSentiment: supporterScore(in.sentiment),
		Alternatives:       alternativesScore(in.reputation),
	}
	total, deltas := totalScore(persona, scores)
	scores.Total = total

	exp := explanation.New("board_confidence", total).
		Add("league performance", deltas[0]).
		Add("promise fulfillment", deltas[1]).
		Add("financial health", deltas[2]).
		Add("board relationship", deltas[3]).
		Add("club dna alignment", deltas[4]).
		Add("supporter sentiment", deltas[5]).
		Add("replacement pressure", deltas[6])

	if err := s.writeReviewSnapshot(ctx, tx, in, scores, exp, tick); err != nil {
		return scores, nil, err
	}
	if err := s.recordBoardEvent(ctx, tx, worldID, tick, "system", uuid.Nil, EventBoardReviewed, exp); err != nil {
		return scores, nil, err
	}
	return scores, exp, nil
}

// writeReviewSnapshot persists the snapshot row and its explanation.
func (s *Service) writeReviewSnapshot(ctx context.Context, tx pgx.Tx, in reviewInputs, scores FactorScores, exp *explanation.Explanation, tick int64) error {
	rawExp, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal review explanation: %w", err)
	}
	snap := Snapshot{ManagerID: in.managerID, ClubID: in.clubID, WorldTick: tick, Scores: scores, Explanation: rawExp}
	if err := s.store.writeSnapshot(ctx, tx, snap); err != nil {
		return err
	}
	return nil
}

// recordBoardEvent emits one board-scoped event with its structured
// explanation through the outbox (falls back to record-only when bus is nil).
func (s *Service) recordBoardEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, tick int64,
	actorType string, actorID uuid.UUID, eventType string, exp *explanation.Explanation) error {

	rawExp, err := json.Marshal(exp)
	if err != nil {
		return fmt.Errorf("marshal event explanation: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"subject": exp.Subject, "score": exp.Score})
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	e := eventbus.Event{
		WorldID:     worldID,
		WorldTick:   tick,
		EventType:   eventType,
		Explanation: rawExp,
		Payload:     payload,
	}
	if actorType != "" {
		at := actorType
		e.ActorType = &at
		if actorID != uuid.Nil {
			e.ActorID = &actorID
		}
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// emitNegotiation records the manager-initiated mandate change.
func (s *Service) emitNegotiation(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, m *Mandate, exp *explanation.Explanation) error {
	tick, err := s.store.currentTick(ctx, tx, worldID)
	if err != nil {
		return err
	}
	return s.recordBoardEvent(ctx, tx, worldID, tick, "manager", m.ManagerID, EventMandateNegotiated, exp)
}

// parsePP reads the tolerance percentage of a wage_structure mandate.
func parsePP(m *Mandate) int {
	if v, ok := parseInt(m.TargetValue); ok {
		return v
	}
	return 0
}

// perf computes the performance factor (neutral 50 when no league standings).
func (s *Service) perf(in reviewInputs, finish int) int {
	if !in.hasLeague {
		return 50
	}
	return performanceScore(in.position, finish)
}
