package faction

import (
	"fmt"
	"hash/fnv"
)

// PairStream keys a deterministic, order-independent draw to a pair of players
// and the world seed. It mirrors internal/injury's stream helpers: the same
// inputs always produce the same edge, so relationship generation is replayable
// and never consumes any other subsystem's RNG stream.
func PairStream(worldSeed int64, a, b string) uint64 {
	if a > b {
		a, b = b, a
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("faction-pair:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s:%s", worldSeed, a, b)))
	return h.Sum64()
}

// SeedStream derives a stable per-club generation seed from the world seed and
// the club id, so each club's graph is independent yet fully reproducible.
func SeedStream(worldSeed int64, clubID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("faction-seed:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s", worldSeed, clubID)))
	return h.Sum64()
}
