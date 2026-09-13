package squad

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// rng is a tiny splitmix64 stream used only for orchestration-level draws
// (motivation rolls, performance variance). It is deliberately NOT part of
// pkg/matchsim's canonical engine replay contract — the engine's own
// simulation sequence is the replay authority. These draws are deterministic
// because internal/match seeds them from the world/replay seed and salts them
// per club/player, so a resimulated matchday reproduces the same aggregates.
type rng struct {
	state uint64
}

func newRNG(seed int64, salt uint64) *rng {
	return &rng{state: uint64(seed) ^ salt}
}

// next returns one splitmix64 value.
func (r *rng) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// nextFloat returns a float64 in [0, 1).
func (r *rng) nextFloat() float64 {
	return float64(r.next()>>11) / float64(1<<53)
}

// hashClub salts the motivation draw with the club identity so two clubs
// sharing the caller's fixture seed still draw independently.
func hashClub(id uuid.UUID) uint64 {
	if id == uuid.Nil {
		return 0x01
	}
	return fnv64(id[:])
}

// hashPlayer salts the performance draw with the player identity.
func hashPlayer(id uuid.UUID) uint64 {
	if id == uuid.Nil {
		return 0x02
	}
	return fnv64(id[:])
}

func fnv64(b []byte) uint64 {
	h := fnv.New64()
	_, _ = h.Write(b)
	return h.Sum64()
}
