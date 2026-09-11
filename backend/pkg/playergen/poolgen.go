package playergen

import (
	"fmt"
	"math/rand"
	"sort"
)

// namePool holds the first/last name lists for one nationality code.
type namePool struct {
	firstNames []string
	lastNames  []string
}

// PoolGenerator serves name lists from in-memory pools. Pools are loaded from
// the curated data files (LoadNameData) or, later, from ref.name_pool on the
// database side of the team-creation flow. This package is DB-free by design.
type PoolGenerator struct {
	pools map[string]*namePool
}

// NewPoolGenerator returns an empty generator.
func NewPoolGenerator() *PoolGenerator {
	return &PoolGenerator{pools: make(map[string]*namePool)}
}

// AddPool registers the first/last name lists for a nationality code. The
// lists are deduplicated; empty lists are rejected so generation can never
// fall back to "Unknown".
func (g *PoolGenerator) AddPool(code string, first, last []string) error {
	if code == "" {
		return fmt.Errorf("playergen: AddPool requires a non-empty nationality code")
	}
	if len(first) == 0 {
		return fmt.Errorf("playergen: AddPool(%q): first_names list is empty", code)
	}
	if len(last) == 0 {
		return fmt.Errorf("playergen: AddPool(%q): last_names list is empty", code)
	}
	g.pools[code] = &namePool{
		firstNames: dedupe(first),
		lastNames:  dedupe(last),
	}
	return nil
}

// Codes returns the registered nationality codes in sorted order, so iteration
// is deterministic.
func (g *PoolGenerator) Codes() []string {
	codes := make([]string, 0, len(g.pools))
	for code := range g.pools {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// NameList returns the first-name list (first=true) or last-name list
// (first=false) for a nationality code. It is nil for unknown codes. The
// returned slice must not be mutated.
func (g *PoolGenerator) NameList(code string, first bool) []string {
	pool, ok := g.pools[code]
	if !ok {
		return nil
	}
	if first {
		return pool.firstNames
	}
	return pool.lastNames
}

// GenerateFirstName returns a random given name for the nationality.
func (g *PoolGenerator) GenerateFirstName(rng *rand.Rand, code string) (string, error) {
	pool, ok := g.pools[code]
	if !ok || len(pool.firstNames) == 0 {
		return "", fmt.Errorf("playergen: no first-name pool for nationality %q", code)
	}
	return pool.firstNames[rng.Intn(len(pool.firstNames))], nil
}

// GenerateLastName returns a random surname for the nationality.
func (g *PoolGenerator) GenerateLastName(rng *rand.Rand, code string) (string, error) {
	pool, ok := g.pools[code]
	if !ok || len(pool.lastNames) == 0 {
		return "", fmt.Errorf("playergen: no last-name pool for nationality %q", code)
	}
	return pool.lastNames[rng.Intn(len(pool.lastNames))], nil
}

// GenerateFullName returns a random (first, last) pair for the nationality.
func (g *PoolGenerator) GenerateFullName(rng *rand.Rand, code string) (string, string, error) {
	first, err := g.GenerateFirstName(rng, code)
	if err != nil {
		return "", "", err
	}
	last, err := g.GenerateLastName(rng, code)
	if err != nil {
		return "", "", err
	}
	return first, last, nil
}

func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
