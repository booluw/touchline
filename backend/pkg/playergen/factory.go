package playergen

import (
	"fmt"
	"math/rand"
)

// maxNameAttempts bounds regeneration when a collision registry is in use.
// On exhaustion CreatePlayer returns an error rather than emitting a duplicate.
const maxNameAttempts = 16

// CreatePlayerOptions controls the per-call player generation.
// Zero-value produces the same output as CreatePlayer().
type CreatePlayerOptions struct {
	// MinAge / MaxAge override the default [17,33] age span.
	// Clamped to [13,38] at the factory level.
	MinAge int
	MaxAge int
	// QualityOffset shifts all category means by the given integer (-20..+20).
	// 0 = baseline. A positive value produces higher-quality players (academy
	// graduates, elite talent); a negative value produces lower-quality ones.
	QualityOffset int
	// Nationality, when non-empty, forces this nationality code instead of
	// drawing from the weighted nationality pool. The caller must provide a
	// code that exists in ref.nationalities.
	Nationality string
	// Origin propagates to the GeneratedPlayer. The calling layer is
	// responsible for persisting this alongside the player.
	Origin string
	// AcademyProduct marks the player as a club-academy youth intake.
	AcademyProduct bool
}

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

// Rng exposes the factory's underlying random source so sibling generation
// logic (e.g. pool age distribution) can extend the same deterministic stream:
// one seeded rng drives the whole world's output, and a given seed still
// reproduces identical results.
func (f *PlayerFactory) Rng() *rand.Rand { return f.rng }

// CreatePlayer generates one player using the default options (age 17–33,
// no quality offset, weighted nationality). For a fixed rng seed the output
// sequence is deterministic. With a registry set, names are regenerated
// until unique within the pool (bounded by maxNameAttempts).
func (f *PlayerFactory) CreatePlayer() (*GeneratedPlayer, error) {
	return f.CreatePlayerWithOptions(CreatePlayerOptions{})
}

// CreatePlayerWithOptions generates one player with caller-supplied options.
// All fields have sensible defaults; the zero value produces the same output
// as the no-arg CreatePlayer.
func (f *PlayerFactory) CreatePlayerWithOptions(opts CreatePlayerOptions) (*GeneratedPlayer, error) {
	if f.natPool == nil {
		return nil, fmt.Errorf("playergen: nil nationality pool")
	}
	if f.nameGen == nil {
		return nil, fmt.Errorf("playergen: nil name generator")
	}
	if f.rng == nil {
		return nil, fmt.Errorf("playergen: nil random source")
	}

	// Resolve age range, clamped to [13,38]. An invalid or out-of-range pair
	// falls back to the default [17,33] span.
	lo, hi := opts.MinAge, opts.MaxAge
	if lo < 13 || hi < lo || hi > 38 || hi == 0 {
		lo, hi = minAge, maxAge
	}
	qualityOffset := opts.QualityOffset
	if qualityOffset < -20 {
		qualityOffset = -20
	}
	if qualityOffset > 20 {
		qualityOffset = 20
	}

	// Resolve nationality: forced code or weighted draw.
	code := opts.Nationality
	if code == "" {
		var err error
		code, err = f.natPool.WeightedRandom(f.rng)
		if err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt < maxNameAttempts; attempt++ {
		first, last, err := f.nameGen.GenerateFullName(f.rng, code)
		if err != nil {
			return nil, err
		}
		if f.registry == nil || f.registry.Reserve(code, first, last) {
			pos := ValidPositions[f.rng.Intn(len(ValidPositions))]
			age := lo + f.rng.Intn(hi-lo+1)
			ht, personality := generateTraitsAndPersonality(f.rng, age)
			return &GeneratedPlayer{
				FirstName:       first,
				LastName:        last,
				DisplayName:     displayName(first, last),
				NationalityCode: code,
				Age:             age,
				PrimaryPosition: pos,
				Attributes:      generateAttributes(f.rng, pos, qualityOffset),
				HiddenTraits:    ht,
				Personality:     personality,
				EmotionalState:  neutralEmotionalState(f.rng),
				Origin:          opts.Origin,
				AcademyProduct:  opts.AcademyProduct,
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
