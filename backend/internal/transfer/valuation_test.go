package transfer

import (
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
)

func TestAgeFactorSpotValues(t *testing.T) {
	cases := []struct {
		age  int
		want float64
	}{
		{15, 0.80}, {17, 0.80},
		{18, 1.00}, {19, 0.97}, {23, 0.85}, // rising toward the 24-28 peak
		{24, 1.00}, {28, 1.00},
		{29, 0.95}, {33, 0.75}, {35, 0.65},
		{40, 0.50},
	}
	for _, c := range cases {
		if got := ageFactor(c.age); int(got*100) != int(c.want*100) {
			t.Errorf("ageFactor(%d) = %.3f, want %.3f", c.age, got, c.want)
		}
	}
}

func TestContractFactorBoundsAndShape(t *testing.T) {
	if got := contractFactor(30); got != 0.50 {
		t.Fatalf("early-expiry factor = %.3f, want 0.50", got)
	}
	if got := contractFactor(0); got != 0.50 {
		t.Fatalf("zero-days factor = %.3f, want 0.50", got)
	}
	if got := contractFactor(365 * 4); got != 1.10 {
		t.Fatalf("full-term factor = %.3f, want 1.10", got)
	}
	if got := contractFactor(365 * 8); got != 1.10 {
		t.Fatalf("over-term factor = %.3f, want capped 1.10", got)
	}
	if got := contractFactor(365 * 2); got != 0.80 {
		t.Fatalf("two-year factor = %.3f, want 0.80", got)
	}
}

func valuationAttrs(position string, attrs squad.AttributeSnapshot) PlayerAttrs {
	return PlayerAttrs{
		PlayerID:        uuid.New(),
		Position:        position,
		Age:             26,
		ContractEndDays: 365 * 4,
		Attributes:      attrs,
	}
}

func TestValuationIsDeterministicRoundedAndMonotonic(t *testing.T) {
	low := valuationAttrs("ST", squad.AttributeSnapshot{Technical: 70, Physical: 70, Mental: 70, Tactical: 70, Positional: 70})
	mid := valuationAttrs("ST", squad.AttributeSnapshot{Technical: 82, Physical: 82, Mental: 82, Tactical: 82, Positional: 82})
	high := valuationAttrs("ST", squad.AttributeSnapshot{Technical: 90, Physical: 90, Mental: 90, Tactical: 90, Positional: 90})

	if v1, v2 := Valuation(low), Valuation(low); v1 != v2 {
		t.Fatalf("valuation not deterministic: %d vs %d", v1, v2)
	}
	values := []int64{Valuation(low), Valuation(mid), Valuation(high)}
	for _, v := range values {
		if v%10000 != 0 {
			t.Fatalf("valuation %d not rounded to nearest 10k", v)
		}
		if v <= 0 {
			t.Fatalf("valuation %d must be positive for a rated squad player", v)
		}
	}
	if !(values[0] <= values[1] && values[1] <= values[2]) {
		t.Fatalf("valuation must be non-decreasing in attributes: %d, %d, %d", values[0], values[1], values[2])
	}
	if values[0] == values[2] {
		t.Fatal("valuation must actually respond to attribute deltas above the rounding step")
	}
}

func TestValuationKnownValue(t *testing.T) {
	// Neutral 50-rated ST, 26yo, four full years left: 1.10 contract factor.
	// 50^3 * 0.08 * 1.05 * 1.00 * 1.10 = 11,550 -> round to nearest 10k.
	got := Valuation(valuationAttrs("ST", squad.AttributeSnapshot{Technical: 50, Physical: 50, Mental: 50, Tactical: 50, Goalkeeping: 50, Positional: 50}))
	if got != 10000 {
		t.Fatalf("Valuation(ST, all 50) = %d, want 10000", got)
	}
}

func TestValuationPositionAndContractEffects(t *testing.T) {
	base := squad.AttributeSnapshot{Technical: 84, Physical: 84, Mental: 84, Tactical: 84, Goalkeeping: 90, Positional: 88}
	striker := valuationAttrs("ST", base)
	fullBack := valuationAttrs("LB", base)
	if !(Valuation(striker) > Valuation(fullBack)) {
		t.Fatal("star positions must carry a premium over cheap full-backs")
	}

	short := striker
	short.ContractEndDays = 20
	if !(Valuation(short) < Valuation(striker)) {
		t.Fatal("a running-out contract must reduce the valuation")
	}

	young := striker
	young.Age = 31
	if !(Valuation(young) < Valuation(striker)) {
		t.Fatal("decline-phase age must reduce the valuation")
	}
}

func TestRoundTo10k(t *testing.T) {
	if got := roundTo10k(11_550); got != 10_000 {
		t.Fatalf("roundTo10k(11550) = %d, want 10000", got)
	}
	if got := roundTo10k(15_000); got != 20_000 {
		t.Fatalf("roundTo10k(15000) = %d, want 20000", got)
	}
	if got := roundTo10k(1_000); got != 0 {
		t.Fatalf("roundTo10k(1000) = %d, want 0", got)
	}
}

func TestAiSellerDecision(t *testing.T) {
	const val = 1_000_000 // £10,000
	// no asking: AI holds out for 1.10x valuation.
	if d, target := aiSellerDecision(1_100_000, val, nil); d != RespondAccept || target != 1_100_000 {
		t.Fatalf("at/above target = %s/%d, want accept/1100000", d, target)
	}
	if d, target := aiSellerDecision(950_000, val, nil); d != RespondCounter || target != 1_100_000 {
		t.Fatalf("near target = %s/%d, want counter/1100000", d, target)
	}
	if d, _ := aiSellerDecision(700_000, val, nil); d != RespondReject {
		t.Fatalf("below floor = %s, want reject", d)
	}
	// an asking price above the natural target anchors the hold-out.
	asking := int64(2_000_000)
	if d, target := aiSellerDecision(2_000_000, val, &asking); d != RespondAccept || target != 2_000_000 {
		t.Fatalf("at asking = %s/%d, want accept/2000000", d, target)
	}
	if d, _ := aiSellerDecision(1_050_000, val, &asking); d != RespondCounter {
		t.Fatalf("below asking but above floor = %s, want counter", d)
	}
}

func TestAiBuyerDecision(t *testing.T) {
	const val = 1_000_000
	if d, target := aiBuyerDecision(950_000, val); d != RespondAccept || target != 950_000 {
		t.Fatalf("fair price = %s/%d, want accept/950000", d, target)
	}
	if d, _ := aiBuyerDecision(1_000_000, val); d != RespondCounter {
		t.Fatalf("above ceiling but close = %s, want counter", d)
	}
	if d, _ := aiBuyerDecision(2_000_000, val); d != RespondReject {
		t.Fatalf("seller overreach = %s, want reject", d)
	}
}

func TestAiBidFeeBoundsAndDeterminism(t *testing.T) {
	const val = 1_000_000
	const seed int64 = 424242

	fee := aiBidFee(seed, val, 0)
	if fee < 750_000 || fee > 950_000 {
		t.Fatalf("fee %d outside [0.75, 0.95]x valuation", fee)
	}
	if fee%10000 != 0 {
		t.Fatalf("fee %d not rounded to 10k", fee)
	}
	if again := aiBidFee(seed, val, 0); again != fee {
		t.Fatalf("fee not deterministic: %d vs %d", fee, again)
	}

	// An asking price under the natural ceiling caps the bid.
	asking := int64(800_000)
	if capped := aiBidFee(seed, val, asking); capped > 800_000 {
		t.Fatalf("fee %d above asking price", capped)
	}
	// An asking price under the floor collapses the band to the asking price.
	lowAsking := int64(600_000)
	if got := aiBidFee(seed, val, lowAsking); got != 600_000 {
		t.Fatalf("collapsed band fee = %d, want 600000", got)
	}
}

func TestAiBidSeedStablePerListingTick(t *testing.T) {
	id := uuid.MustParse("a1b2c3d4-0000-0000-0000-000000000000")
	if s1, s2 := aiBidSeed(id, 7), aiBidSeed(id, 7); s1 != s2 {
		t.Fatalf("seed not stable: %d vs %d", s1, s2)
	}
	if s := aiBidSeed(id, 8); s == aiBidSeed(id, 7) {
		t.Fatal("seed must change across world ticks")
	}
	if s := aiBidSeed(uuid.New(), 7); s == aiBidSeed(id, 7) {
		t.Fatal("seed must change across listings")
	}
}

func TestMul(t *testing.T) {
	if got := mul(100_000, 1.10); got != 110_000 {
		t.Fatalf("mul(100000, 1.10) = %d, want 110000", got)
	}
	if got := mul(750_000, 0.90); got != 675_000 {
		t.Fatalf("mul(750000, 0.90) = %d, want 675000", got)
	}
}

func TestValidateTerms(t *testing.T) {
	good := Terms{Fee: 1_000_000, WeeklyWage: 10_000, ContractLengthMonths: 24}
	for name, mutate := range map[string]func(*Terms){
		"zero fee":          func(t *Terms) { t.Fee = 0 },
		"zero wage":         func(t *Terms) { t.WeeklyWage = 0 },
		"short contract":    func(t *Terms) { t.ContractLengthMonths = 0 },
		"overlong contract": func(t *Terms) { t.ContractLengthMonths = 121 },
		"negative bonus":    func(t *Terms) { t.SigningBonus = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			terms := good
			mutate(&terms)
			if err := validateTerms(terms); err != ErrInvalidTerms {
				t.Fatalf("%s: err = %v, want ErrInvalidTerms", name, err)
			}
		})
	}
	sellOn := good
	sellOn.SellOnPercentage = new(int)
	*sellOn.SellOnPercentage = 130
	if err := validateTerms(sellOn); err != ErrInvalidTerms {
		t.Fatalf("sell-on over 100%%: err = %v, want ErrInvalidTerms", err)
	}
	valid := good
	valid.SellOnPercentage = new(int)
	*valid.SellOnPercentage = 20
	if err := validateTerms(valid); err != nil {
		t.Fatalf("valid terms rejected: %v", err)
	}
}

func TestNegotiationOpenAndListingTypes(t *testing.T) {
	bid := &Bid{Status: BidStatusPending}
	if !bid.NegotiationOpen() {
		t.Fatal("pending bid must be negotiable")
	}
	bid.Status = BidStatusCountered
	if !bid.NegotiationOpen() {
		t.Fatal("countered bid must be negotiable")
	}
	for _, s := range []string{BidStatusAccepted, BidStatusRejected, BidStatusWithdrawn, BidStatusExpired} {
		bid.Status = s
		if bid.NegotiationOpen() {
			t.Fatalf("%s bid must be closed", s)
		}
	}
	for lt, ok := range map[string]bool{
		ListingOpenToOffers: true, ListingActivelyShopped: true,
		ListingLoanAvailable: true, "nonsense": false,
	} {
		if ReasonableListingTypes[lt] != ok {
			t.Fatalf("ReasonableListingTypes[%q] = %v, want %v", lt, !ok, ok)
		}
	}
}

func TestBidTTLConstant(t *testing.T) {
	if BidTTLWorldDays != 3 {
		t.Fatalf("BidTTLWorldDays = %d, want 3 (OPD-05 resolution)", BidTTLWorldDays)
	}
}
