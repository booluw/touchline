package playergen

import (
	"fmt"
	"math/rand"
	"sort"
)

// Nationality describes one nationality/name culture in the global player pool.
type Nationality struct {
	Code   string
	Name   string
	Weight float64 // relative weight; must be > 0 for the nationality to appear
}

// NationalityPool defines weighted nationality distribution for player
// generation. Weights mirror ref.nationalities.generation_weight and are
// sourced from the curated data files, so tuning is a data change, not code.
type NationalityPool struct {
	entries []Nationality
}

// NewNationalityPool returns an empty pool.
func NewNationalityPool() *NationalityPool {
	return &NationalityPool{}
}

// Add appends a nationality. Entries are kept in insertion order.
func (p *NationalityPool) Add(n Nationality) {
	p.entries = append(p.entries, n)
}

// Entries returns a copy of the pool entries.
func (p *NationalityPool) Entries() []Nationality {
	out := make([]Nationality, len(p.entries))
	copy(out, p.entries)
	return out
}

// TotalWeight returns the sum of positive weights.
func (p *NationalityPool) TotalWeight() float64 {
	var total float64
	for _, n := range p.entries {
		if n.Weight > 0 {
			total += n.Weight
		}
	}
	return total
}

// WeightedRandom selects a nationality code proportional to its weight.
// Non-positive weights are never selected. It errors on an empty/zero-weight pool.
func (p *NationalityPool) WeightedRandom(rng *rand.Rand) (string, error) {
	total := p.TotalWeight()
	if total <= 0 {
		return "", fmt.Errorf("playergen: nationality pool has no positive weights")
	}
	target := rng.Float64() * total
	var cumulative float64
	for _, n := range p.entries {
		if n.Weight <= 0 {
			continue
		}
		cumulative += n.Weight
		if target < cumulative {
			return n.Code, nil
		}
	}
	// Floating-point tail: fall back to the last positive-weight entry.
	for i := len(p.entries) - 1; i >= 0; i-- {
		if p.entries[i].Weight > 0 {
			return p.entries[i].Code, nil
		}
	}
	return "", fmt.Errorf("playergen: nationality pool has no positive weights")
}

// Codes returns the codes of all entries with positive weight, sorted.
func (p *NationalityPool) Codes() []string {
	codes := make([]string, 0, len(p.entries))
	for _, n := range p.entries {
		if n.Weight > 0 {
			codes = append(codes, n.Code)
		}
	}
	sort.Strings(codes)
	return codes
}
