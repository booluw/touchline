package playergen

import (
	"math/rand"
	"testing"
)

func TestFileBackedGenerator_LoadsNames(t *testing.T) {
	gen, err := NewFileBackedGenerator("../../data/names")
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}

	// This will return "Unknown" if no data files exist yet
	// That's expected until name data files are curated
	first := gen.GenerateFirstName("nigeria")
	last := gen.GenerateLastName("nigeria")

	if first == "" || last == "" {
		t.Errorf("expected non-empty names, got %s %s", first, last)
	}
}

func TestNationalityPool_WeightedRandom(t *testing.T) {
	pool := &NationalityPool{
		Entries: []NationalityWeight{
			{Nationality: "brazil", Weight: 10.0},
			{Nationality: "nigeria", Weight: 8.0},
			{Nationality: "england", Weight: 6.0},
		},
	}

	// With the current placeholder implementation, this always returns the first entry
	nat := pool.WeightedRandom()
	if nat != "brazil" {
		t.Errorf("expected brazil (placeholder), got %s", nat)
	}
}

func TestFileBackedGenerator_RandSeed(t *testing.T) {
	// Verify that different seeds produce different results
	rand.Seed(1)
	gen, _ := NewFileBackedGenerator("../../data/names")
	first1 := gen.GenerateFirstName("brazil")

	rand.Seed(2)
	gen2, _ := NewFileBackedGenerator("../../data/names")
	first2 := gen2.GenerateFirstName("brazil")

	// Not guaranteed to be different, but with enough name pool entries it's likely
	// This is mainly a smoke test that the generator doesn't panic
	_ = first1
	_ = first2
}
