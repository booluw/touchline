package playergen

import "math/rand"

// PlayerNameGenerator generates player names for a nationality code.
//
// Implementations must be read-only after construction so they can be shared
// safely; the *rand.Rand is passed per call so callers fully control seeding
// (one rng per world/team, seeded from world.event random_seed for
// deterministic output). Convention: first + last (given + surname) only;
// single-name and shirt-name rules are deferred.
type PlayerNameGenerator interface {
	// GenerateFirstName returns a random given name for the nationality.
	GenerateFirstName(rng *rand.Rand, nationalityCode string) (string, error)
	// GenerateLastName returns a random surname for the nationality.
	GenerateLastName(rng *rand.Rand, nationalityCode string) (string, error)
	// GenerateFullName returns a random (first, last) pair for the nationality.
	GenerateFullName(rng *rand.Rand, nationalityCode string) (first, last string, err error)
}
