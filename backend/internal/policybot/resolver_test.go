package policybot

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/touchline/backend/internal/transfer"
)

func defaultTransferPolicy() TransferPolicy {
	return TransferPolicy{
		SellFloorPct:   100,
		AcceptAbovePct: 120,
		AutoCounter:    true,
	}
}

func wellRatedAttrs() transfer.PlayerAttrs {
	var a transfer.PlayerAttrs
	a.PlayerID = uuid.New()
	a.Position = "ST"
	a.Attributes.Technical = 80
	a.Attributes.Physical = 80
	a.Attributes.Mental = 80
	a.Attributes.Tactical = 80
	a.Attributes.Goalkeeping = 40
	a.Attributes.Positional = 80
	a.Age = 26
	a.ContractEndDays = 1000
	a.MarketValue = math.MaxInt64
	return a
}

func bidWithFee(fee int64) transfer.Bid {
	return transfer.Bid{
		ID:                   uuid.New(),
		Fee:                  fee,
		WeeklyWage:           10000,
		ContractLengthMonths: 24,
		SigningBonus:         0,
	}
}

func TestResolveTransferAcceptAboveThreshold(t *testing.T) {
	attrs := wellRatedAttrs()
	val := transfer.Valuation(attrs)

	decision := ResolveTransfer(attrs, bidWithFee(int64(float64(val)*1.25)), defaultTransferPolicy(), nil)
	if decision.Action != transfer.RespondAccept {
		t.Fatalf("expected accept at 125%% of valuation, got %s (val=%d)", decision.Action, val)
	}
}

func TestResolveTransferRejectBelowFloor(t *testing.T) {
	attrs := wellRatedAttrs()
	val := transfer.Valuation(attrs)

	decision := ResolveTransfer(attrs, bidWithFee(int64(float64(val)*0.9)), defaultTransferPolicy(), nil)
	if decision.Action != transfer.RespondReject {
		t.Fatalf("expected reject at 90%% of valuation, got %s", decision.Action)
	}
}

func TestResolveTransferCounterInRange(t *testing.T) {
	attrs := wellRatedAttrs()
	val := transfer.Valuation(attrs)

	decision := ResolveTransfer(attrs, bidWithFee(int64(float64(val)*1.10)), defaultTransferPolicy(), nil)
	if decision.Action != transfer.RespondCounter {
		t.Fatalf("expected counter at 110%% of valuation, got %s", decision.Action)
	}
	if decision.Terms == nil || decision.Terms.Fee != int64(float64(val)*1.2) {
		t.Fatalf("expected counter at 120%% of valuation, got %+v", decision.Terms)
	}
}

func TestResolveTransferAutoCounterOff(t *testing.T) {
	attrs := wellRatedAttrs()
	val := transfer.Valuation(attrs)
	policy := defaultTransferPolicy()
	policy.AutoCounter = false

	decision := ResolveTransfer(attrs, bidWithFee(int64(float64(val)*1.10)), policy, nil)
	if decision.Action != transfer.RespondReject {
		t.Fatalf("expected reject when auto_counter is off, got %s", decision.Action)
	}
}

func TestResolveTransferAskingPriceRaisesCounter(t *testing.T) {
	attrs := wellRatedAttrs()
	val := transfer.Valuation(attrs)
	asking := int64(float64(val) * 1.35)

	decision := ResolveTransfer(attrs, bidWithFee(int64(float64(val)*1.10)), defaultTransferPolicy(), &asking)
	if decision.Action != transfer.RespondCounter {
		t.Fatalf("expected counter, got %s", decision.Action)
	}
	if decision.Terms == nil || decision.Terms.Fee != asking {
		t.Fatalf("expected counter raised to asking price %d, got %+v", asking, decision.Terms)
	}
}
