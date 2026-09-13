package playergen

import (
	"fmt"
	"math/rand"
)

// maxNameAttempts bounds regeneration when a collision registry is in use.
// On exhaustion CreatePlayer returns an error rather than emitting a duplicate.
const maxNameAttempts = 16

// PlayerFactory creates players with nationality-weighted names and basic
// attributes. It holds no database dependency: the caller supplies the loaded
// name generator, nationality pool, and a seeded rng (one per world/team).
type PlayerFactory struct {
	nameGen  PlayerNameGenerator
	natPool  *NationalityPool
	rng      *rand.Rand
	registry *NameRegistry
}

// NewPlayerFactory returns a factory using the given name generator, weighted
// nationality pool, and seeded random source.
func NewPlayerFactory(nameGen PlayerNameGenerator, natPool *NationalityPool, rng *rand.Rand) *PlayerFactory {
	return &PlayerFactory{
		nameGen: nameGen,
		natPool: natPool,
		rng:     rng,
	}
}

// WithRegistry enables collision-avoiding name uniqueness for factory output.
func (f *PlayerFactory) WithRegistry(r *NameRegistry) *PlayerFactory {
	f.registry = r
	return f
}

// CreatePlayer generates one player. For a fixed rng seed the output sequence
// is deterministic. With a registry set, names are regenerated until unique
// within the pool (bounded by maxNameAttempts).
func (f *PlayerFactory) CreatePlayer() (*GeneratedPlayer, error) {
	if f.natPool == nil {
		return nil, fmt.Errorf("playergen: nil nationality pool")
	}
	if f.nameGen == nil {
		return nil, fmt.Errorf("playergen: nil name generator")
	}
	if f.rng == nil {
		return nil, fmt.Errorf("playergen: nil random source")
	}

	code, err := f.natPool.WeightedRandom(f.rng)
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < maxNameAttempts; attempt++ {
		first, last, err := f.nameGen.GenerateFullName(f.rng, code)
		if err != nil {
			return nil, err
		}
		if f.registry == nil || f.registry.Reserve(code, first, last) {
			pos := ValidPositions[f.rng.Intn(len(ValidPositions))]
			age := minAge + f.rng.Intn(maxAge-minAge+1)
			ht, personality := generateTraitsAndPersonality(f.rng, age)
			return &GeneratedPlayer{
				FirstName:       first,
				LastName:        last,
				DisplayName:     displayName(first, last),
				NationalityCode: code,
				Age:             age,
				PrimaryPosition: pos,
				Attributes:      generateAttributes(f.rng, pos),
				HiddenTraits:    ht,
				Personality:     personality,
				EmotionalState:  neutralEmotionalState(f.rng),
			}, nil
		}
	}
	return nil, fmt.Errorf("playergen: could not generate a unique name for %q within %d attempts", code, maxNameAttempts)
}

// displayName is the shirt name. For first_last cultures the surname is used;
// single-name cultures (deferred) fall back to the given name.
func displayName(first, last string) string {
	if last != "" {
		return last
	}
	return first
}
