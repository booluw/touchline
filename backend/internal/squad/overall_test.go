package squad

import "testing"

func TestPositionalOverallCapsAt99(t *testing.T) {
	maxed := AttributeSnapshot{Technical: 100, Physical: 100, Mental: 100, Tactical: 100, Goalkeeping: 100, Positional: 100}
	for _, pos := range ValidPositions {
		if got := PositionalOverall(pos, maxed); got != 99 {
			t.Fatalf("position %s overall = %d, want capped 99", pos, got)
		}
	}
}

func TestPositionalOverallWithinBand(t *testing.T) {
	mid := AttributeSnapshot{Technical: 50, Physical: 50, Mental: 50, Tactical: 50, Goalkeeping: 50, Positional: 50}
	for _, pos := range ValidPositions {
		got := PositionalOverall(pos, mid)
		if got < 1 || got > 99 {
			t.Fatalf("position %s overall = %d out of [1,99]", pos, got)
		}
	}
}

func TestPositionalOverallRespectsWeights(t *testing.T) {
	// A goalkeeper with goalkeeping-maxed but technical-zero attributes should
	// outrate one reversed (GK's recipe weights goalkeeping, not technical).
	good := PositionalOverall("GK", AttributeSnapshot{Goalkeeping: 95})
	bad := PositionalOverall("GK", AttributeSnapshot{Technical: 95})
	if good <= bad {
		t.Fatalf("GK goalkeeping-led overall %d should exceed technical-led %d", good, bad)
	}
}

func TestOverallDeltaSignedAndZeroNeutral(t *testing.T) {
	if got := OverallDelta("ST", AttributeSnapshot{}); got != 0 {
		t.Fatalf("zero delta snapshot should read 0, got %d", got)
	}
	// A +10 technical delta on the ST attack side is positive; a -20 physical
	// delta (pace etc.) reads clearly negative through both attack and defense.
	positive := OverallDelta("ST", AttributeSnapshot{Technical: 20})
	negative := OverallDelta("ST", AttributeSnapshot{Physical: -20})
	if positive <= 0 {
		t.Fatalf("positive delta expected, got %d", positive)
	}
	if negative >= 0 {
		t.Fatalf("negative delta expected, got %d", negative)
	}
}

func TestHeadlineKeysForPosition(t *testing.T) {
	for _, pos := range ValidPositions {
		keys := HeadlineKeysForPosition(pos)
		if len(keys) == 0 {
			t.Fatalf("position %s has no headline keys", pos)
		}
	}
	// Unknown position must fall back, never empty.
	if keys := HeadlineKeysForPosition("NOPE"); len(keys) == 0 {
		t.Fatal("unknown position must fall back to a block")
	}
}

func TestHeadlineKeysToDeltasCollapsesCategories(t *testing.T) {
	d := map[string]int{"passing": 2, "tackling": -1, "pace": 3, "morale": 5}
	got := HeadlineKeysToDeltas(d)
	if got.Technical != 1 {
		t.Fatalf("technical delta = %d, want 1 (passing 2 + tackling -1)", got.Technical)
	}
	if got.Physical != 3 {
		t.Fatalf("physical delta = %d, want 3", got.Physical)
	}
	// morale is not an attribute key and must be ignored.
	if got.Mental != 0 || got.Goalkeeping != 0 || got.Positional != 0 {
		t.Fatalf("unexpected deltas: %+v", got)
	}
}
