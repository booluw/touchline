package player

import (
	"context"
	"math"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/explanation"
)

// AskingPricePresets are the approve-and-list price choices (IM65), as a
// multiple of market value. Labels only: the market does not model how price
// changes buyer interest.
var AskingPricePresets = map[string]float64{
	"quick_sale": 0.8,
	"valuation":  1.0,
	"hold_out":   1.25,
}

var askingPriceOrder = []struct{ key, label string }{
	{"quick_sale", "Quick sale"}, {"valuation", "Valuation"}, {"hold_out", "Hold out"},
}

// moraleTargetExplanation splits the morale target (0–100, relative to the
// 50 neutral baseline) into the exact terms moraleTarget adds (IM64). A
// remainder from clamping and rounding is its own factor, so deltas always sum
// to the score.
func moraleTargetExplanation(share, expected float64, p PlayerPersonality) *explanation.Explanation {
	pts := func(v float64) int { return int(math.Round(v * 100)) }
	score := pts(moraleTarget(share, expected, p)) - pts(MoraleNeutralBaseline)
	e := explanation.New("morale target", score)
	switch {
	case expected <= 0:
		e.Add("development role: no playing-time pressure", pts(MoraleTargetSatisfied-MoraleNeutralBaseline))
	case share >= expected:
		e.Add("playing time meets role", pts(MoraleTargetSatisfied-MoraleNeutralBaseline))
	case share >= expected/2:
		e.Add("playing time below role", pts(MoraleTargetNeutral-MoraleNeutralBaseline))
	default:
		e.Add("playing time far below role", pts(MoraleTargetUnhappy-MoraleNeutralBaseline))
		e.Add("patience", pts(PatienceMoraleStep*float64(p.Patience-50)/50))
		e.Add("loyalty", pts(LoyaltyMoraleStep*float64(p.Loyalty-50)/50))
		e.Add("ambition", -pts(AmbitionMoraleStep*float64(p.Ambition-50)/50))
		e.Add("ego", -pts(EgoMoraleStep*float64(p.Ego-50)/50))
	}
	sum := 0
	for _, f := range e.Factors {
		sum += f.Delta
	}
	if r := score - sum; r != 0 {
		e.Add("limits and rounding", r)
	}
	return e
}

// RequestPreview is the read-only consequence preview of answering an open
// transfer request (IM65). Every effect is a constant the matching action
// applies; nothing is simulated.
type RequestPreview struct {
	MarketValue int64              `json:"market_value"`
	Prices      []AskingPriceView  `json:"prices"`
	Options     []RequestOptionPrv `json:"options"`
}

type AskingPriceView struct {
	Preset      string  `json:"preset"`
	Label       string  `json:"label"`
	Multiplier  float64 `json:"multiplier"`
	AskingPrice int64   `json:"asking_price"`
}

// RequestOptionPrv is one answer: its immediate effects (morale points and
// relationship memory) and the follow-ups the engine will apply later.
type RequestOptionPrv struct {
	Action  string               `json:"action"`
	Effects []explanation.Factor `json:"effects"`
	Notes   []string             `json:"notes"`
}

func buildRequestPreview(morale float64, value int64) RequestPreview {
	out := RequestPreview{MarketValue: value}
	for _, p := range askingPriceOrder {
		m := AskingPricePresets[p.key]
		out.Prices = append(out.Prices, AskingPriceView{
			Preset: p.key, Label: p.label, Multiplier: m, AskingPrice: askingPrice(value, m),
		})
	}
	drop := int(math.Round(math.Min(DenyMoraleDrop, morale) * 100))
	out.Options = []RequestOptionPrv{
		{Action: "approve",
			Effects: []explanation.Factor{{Label: "relationship with you", Delta: SentimentTransferApproved}},
			Notes:   []string{"Listed open to offers at the chosen asking price."}},
		{Action: "reassure",
			Effects: []explanation.Factor{
				{Label: "relationship with you", Delta: SentimentReassured},
				{Label: "if the promise is kept", Delta: SentimentPromiseKept},
				{Label: "if the promise is broken", Delta: SentimentPromiseBroken},
			},
			Notes: []string{"Request paused for 28 days.", "Promise judged after 4 weeks against his role's playing time."}},
		{Action: "deny",
			Effects: []explanation.Factor{
				{Label: "morale", Delta: -drop},
				{Label: "relationship with you", Delta: SentimentTransferDenied},
			},
			Notes: []string{"He cannot ask again for 28 days."}},
	}
	return out
}

func askingPrice(value int64, multiplier float64) int64 {
	return int64(math.Round(float64(value) * multiplier))
}

// TransferRequestPreview returns the consequence preview for a player's open
// request at the caller's club.
func (s *Service) TransferRequestPreview(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*RequestPreview, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	pc, err := playerClubID(ctx, s.pool, playerID)
	if err != nil || pc != clubID {
		return nil, ErrPlayerNotInClub
	}
	req, err := openTransferRequest(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrRequestNotFound
	}
	vars, err := loadMoraleVars(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	value, err := marketValue(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	p := buildRequestPreview(vars.Current, value)
	return &p, nil
}

// ReassurePlayerRequest answers a player's open request with a playing-time
// promise (HTTP-facing; no requestID needed).
func (s *Service) ReassurePlayerRequest(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*TransferRequest, error) {
	req, err := openTransferRequest(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrRequestNotFound
	}
	return s.ReassurePlayer(ctx, worldID, managerID, req.ID)
}
