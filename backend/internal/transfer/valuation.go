package transfer

import (
	"math"
	"math/rand"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
)

// Valuation and AI policy numbers. Everything here is PROPOSAL data (the
// OPD-03/OPD-05 companion): the formula shape is agreed in
// docs/design/transfer-numerics.md but every constant is game-parameterized —
// recalibration is a data change, never code (same convention as
// squad.DefaultPositionWeights).
const (
	// BidTTLWorldDays is how many days an unanswered bid survives before the
	// daily sweep expires it (OPD-05 resolution).
	BidTTLWorldDays = 3

	// AI market behaviour (transfer-numerics.md §2).
	aiSellAcceptMultiple = 1.10 // sell when fee >= max(asking, valuation*1.10)
	aiSellCounterFloor   = 0.90 // below this an AI seller rejects outright
	aiBuyMaxMultiple     = 0.95 // an AI buyer will pay at most this fraction of valuation
	aiBuyMinMultiple     = 0.75 // and never lowballs below this
	aiInterestClubs      = 2    // top-K AI buyers invited to bid per listing
	aiBuyFundsMargin     = 1.25 // AI buyers must have cash >= 1.25 * fee
	aiMaxRounds          = 3    // after this many counter rounds AI rejects
)

// PositionMarketMultiplier scales a rating-derived value by position (star
// positions carry scarcity; full-backs are cheap). Proposal data.
var PositionMarketMultiplier = map[string]float64{
	"GK": 0.90, "CB": 1.00, "LB": 0.85, "RB": 0.85,
	"DM": 1.00, "CM": 1.00, "AM": 1.05, "LM": 0.90, "RM": 0.90,
	"ST": 1.05, "LW": 1.00, "RW": 1.00,
}

// PlayerAttrs is the valuation input snapshot for one player.
type PlayerAttrs struct {
	PlayerID        uuid.UUID
	Position        string
	Age             int
	MarketValue     int64
	ContractEndDays int   // days remaining on the active contract
	Wage            int64 // weekly wage of the active contract (AI deal terms)
	Attributes      squad.AttributeSnapshot
}

// ageFactor implements the value curve: players peak 24-28 and decline
// smoothly after 30; promising youngsters command a modest premium.
func ageFactor(age int) float64 {
	switch {
	case age <= 17:
		return 0.80
	case age <= 23:
		return 0.85 + 0.03*float64(23-age) // 0.85..1.00 rising toward peak
	case age <= 28:
		return 1.00
	case age <= 35:
		return 1.00 - 0.05*float64(age-28)
	default:
		return 0.65 - 0.03*float64(age-35)
	}
}

// contractFactor rewards length of control: a player with four years left is
// worth more than one running into an expiring deal.
func contractFactor(daysLeft int) float64 {
	if daysLeft <= 45 {
		return 0.50 // into the last month honest-security territory
	}
	f := 0.50 + clamp01(float64(daysLeft)/float64(365*4))*0.60 // 0.5 .. 1.1
	if f > 1.10 {
		f = 1.10
	}
	return f
}

// overallRating collapses the six attribute categories into one [1,100]
// number using the agreed per-position weighting (the same recipe matchsim
// consumes via squad.BuildSquadRatings on a single-member XI).
func overallRating(p PlayerAttrs) int {
	member := squad.SquadMember{PlayerID: p.PlayerID, Position: p.Position, Attributes: p.Attributes}
	att, def := squad.BuildSquadRatings([]squad.SquadMember{member}, squad.DefaultPositionWeights, nil)
	return (att + def) / 2
}

// Valuation computes the deterministic market value of a player in pence
// (finance.cash units). Rounding is to the nearest 10k so identical situations
// always agree and AI anchoring is stable.
func Valuation(p PlayerAttrs) int64 {
	ovr := float64(overallRating(p))
	mult := PositionMarketMultiplier[p.Position]
	if mult == 0 {
		mult = 0.9
	}
	v := ovr * ovr * ovr * 0.08 * mult * ageFactor(p.Age) * contractFactor(p.ContractEndDays)
	return roundTo10k(v)
}

func roundTo10k(v float64) int64 {
	return int64(math.Round(v/10000.0)) * 10000
}

// mul applies a float policy multiplier to an integer (pence) value. The
// multiplier is proposal data; conversions are explicit so a recalibration
// never changes the rounding.
func mul(v int64, m float64) int64 {
	return int64(float64(v) * m)
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// aiSellerDecision returns accept/counter/reject for a bid received by an AI
// club over one of its players. target is what the AI will hold out for: the
// asking price when set, else valuation with a premium.
func aiSellerDecision(fee, valuation int64, asking *int64) (decision string, target int64) {
	target = mul(valuation, aiSellAcceptMultiple)
	if asking != nil && *asking > target {
		target = *asking
	}
	switch {
	case fee >= target:
		return RespondAccept, target
	case fee >= mul(valuation, aiSellCounterFloor):
		return RespondCounter, target
	default:
		return RespondReject, target
	}
}

// aiBuyerDecision returns the AI's reaction (as a buying club) to a counter
// proposal: accept when the price is fair to the AI, counter back at its
// ceiling when close, reject when the seller is asking too much.
func aiBuyerDecision(fee, valuation int64) (decision string, target int64) {
	target = mul(valuation, aiBuyMaxMultiple)
	switch {
	case fee <= target:
		return RespondAccept, target
	case fee <= mul(valuation, aiBuyMaxMultiple+0.15):
		return RespondCounter, target
	default:
		return RespondReject, target
	}
}

// aiBidFee samples the fee an interested AI buyer offers for a listed player,
// seeded deterministically (listing id + world tick) so replays are stable.
// The fee lands in [0.75, 0.95] of valuation, never above the asking price.
func aiBidFee(seed int64, valuation, askingPrice int64) int64 {
	rng := rand.New(rand.NewSource(seed))
	hi := mul(valuation, aiBuyMaxMultiple)
	lo := mul(valuation, aiBuyMinMultiple)
	if askingPrice > 0 && askingPrice < hi {
		hi = askingPrice
	}
	if hi <= lo {
		return roundTo10k(float64(hi))
	}
	span := float64(hi - lo)
	fee := lo + int64(rng.Float64()*span)
	return roundTo10k(float64(fee))
}

// aiBidSeed derives a stable RNG seed for AI buyer interest on one listing in
// a given world tick.
func aiBidSeed(listingID uuid.UUID, worldTick int64) int64 {
	h := fnv64(listingID[:])
	return h ^ (int64(7919) * worldTick)
}

func fnv64(b []byte) int64 {
	var h uint64 = 14695981039346656037
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return int64(h)
}
