package injury

import (
	"fmt"
	"hash/fnv"

	"github.com/google/uuid"
)

// Stream helpers derive the deterministic RNG keys that feed Evaluate/Setback.
// They mirror internal/training's rngFor pattern: a fnv64a hash over a domain
// tag + inputs, so a redelivered match or weekly tick replays the same draw —
// and so these draws NEVER consume the matchsim canonical stream (the frozen
// v1.6 replay contract stays byte-identical).

// MatchStream keys one match injury to (match seed, injured player). A match
// can injure many players; each gets its own independent stream.
func MatchStream(matchSeed int64, playerID uuid.UUID) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("injury-match:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s", matchSeed, playerID.String())))
	return h.Sum64()
}

// WeekStream keys one weekly injury/training draw to (week, player, tag).
// tag distinguishes domains ("training") so unrelated draws never alias.
func WeekStream(weekTick int64, playerID uuid.UUID, tag string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("injury-week:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s:%s", weekTick, playerID.String(), tag)))
	return h.Sum64()
}

// SetbackStream keys one weekly setback roll to an open injury row, so each
// injury has an independent, replayable setback schedule.
func SetbackStream(injuryID uuid.UUID, weekTick int64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("injury-setback:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%s:%d", injuryID.String(), weekTick)))
	return h.Sum64()
}
