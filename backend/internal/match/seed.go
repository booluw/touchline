package match

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// fixtureSeed derives a fixture's canonical replay seed from its ID alone:
// stable, world-independent, and replay-safe without any base-seed lookup.
//
// Seed scheme (documented contract, upstream of the engine's replay order):
//
//	match.matches.seed = int64(fnv1a64(fixture_id))
//	effective per-club orchestration stream = splitmix64(matchSeed, tag = fnv1a(club_id))
//	effective per-player stream           = splitmix64(matchSeed, tag = fnv1a(player_id))
//
// The per-club/per-player tags give every actor an independent stream from one
// shared match seed; internal/squad applies them in newRNG(seed, hash), so a
// saved world reproduces identical XI selections, motivation rolls, and
// performance draws.
func fixtureSeed(id uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write(id[:])
	return int64(h.Sum64())
}