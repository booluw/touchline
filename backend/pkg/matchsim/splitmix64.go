package matchsim

// splitMix64 is the engine's canonical RNG (spec §4): a single deterministic
// stream produced by the SplitMix64 algorithm. All match randomness flows
// through one instance, and the call order documented in README §4 is the
// replay contract.
type splitMix64 struct {
	state uint64
}

// newSplitMix64 seeds the stream with the match seed.
func newSplitMix64(seed int64) *splitMix64 {
	return &splitMix64{state: uint64(seed)}
}

// next advances the stream and returns the next 64-bit value.
func (sm *splitMix64) next() uint64 {
	sm.state += 0x9E3779B97F4A7C15
	z := sm.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// nextFloat returns a value in [0,1).
func (sm *splitMix64) nextFloat() float64 {
	return float64(sm.next()>>11) * (1.0 / (1 << 53))
}

// nextInt returns a value in [0, n).
func (sm *splitMix64) nextInt(n int) int {
	if n <= 0 {
		return 0
	}
	return int(sm.next() % uint64(n))
}
