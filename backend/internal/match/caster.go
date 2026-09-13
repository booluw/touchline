// Deterministic player casting (PM sign-off): the engine's feed emits
// {player}/{assist}/{sub} tokens; internal/match resolves them into real
// player rows before persistence so the text feed is player-linked. The cast
// is a pure, seeded transform: every token consumes exactly one draw from the
// club's fixture stream, so the same match replays byte-identical row links.
package match

import (
	"hash/fnv"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/matchsim"
)

// castTag separates the cast's RNG domain from the XI selection / motivation
// draws of the same club seed.
const castTag uint64 = 0x4341535445440000 // "CASTED\0\0"

func fnv64a(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// sideCaster resolves one club's tokens, carrying the live on-pitch set so a
// substitution/injury/red card correctly changes which players the feed can
// reference afterwards.
type sideCaster struct {
	r       *castRNG
	onPitch []squad.SquadMember
	off     map[uuid.UUID]bool
	bench   []squad.SquadMember
	taker   *squad.SquadMember

	pendingSub *uuid.UUID // the player leaving via a forced (injury) sub
	lastScorer *uuid.UUID
}

func newSideCaster(matchSeed int64, clubID uuid.UUID, xi, bench []squad.SquadMember, taker *squad.SquadMember) *sideCaster {
	r := newCastRNG(uint64(matchSeed) ^ fnv64a(clubID[:]) ^ castTag)
	return &sideCaster{
		r:       r,
		onPitch: append([]squad.SquadMember{}, xi...),
		off:     make(map[uuid.UUID]bool, len(xi)),
		bench:   bench,
		taker:   taker,
	}
}

// resolve maps one engine event to its primary and related players. Plain
// markers (kickoff/half/full time) resolve to nil, nil.
func (c *sideCaster) resolve(ev matchsim.MatchEvent) (*uuid.UUID, *uuid.UUID) {
	switch ev.Type {
	case matchsim.EventGoal:
		sc := c.pickWeighted(betterAttackers(c.onPitch))
		c.lastScorer = &sc.PlayerID
		return &sc.PlayerID, nil

	case matchsim.EventAssist:
		pool := betterAttackers(c.onPitch)
		if len(pool) > 1 && c.lastScorer != nil {
			pool = without(pool, *c.lastScorer)
		}
		if len(pool) == 0 {
			pool = c.onPitch
		}
		a := c.pickWeighted(pool)
		return &a.PlayerID, c.lastScorer

	case matchsim.EventChance:
		m := c.pickWeighted(c.onPitch)
		return &m.PlayerID, nil

	case matchsim.EventYellowCard:
		m := c.pickWeighted(c.onPitch)
		return &m.PlayerID, nil

	case matchsim.EventRedCard:
		m := c.pickWeighted(c.onPitch)
		c.leave(m.PlayerID)
		return &m.PlayerID, nil

	case matchsim.EventInjury:
		m := c.pickWeighted(c.onPitch)
		c.off[m.PlayerID] = true
		c.pendingSub = &m.PlayerID
		return &m.PlayerID, nil

	case matchsim.EventSubstitution:
		var on uuid.UUID
		if c.pendingSub != nil {
			on = *c.pendingSub
			c.pendingSub = nil
		} else {
			m := c.pickWeighted(c.onPitch)
			on = m.PlayerID
		}
		s := c.bestBench()
		if s == nil {
			return &on, &on // no bench to bring on; the change is cosmetic
		}
		off := s.PlayerID
		c.off[off] = true
		c.leave(on)
		c.onPitch = append(c.onPitch, *s)
		return &off, &on // primary = the player coming ON, related = replaced

	case matchsim.EventPenaltyScored, matchsim.EventPenaltyMissed:
		if c.taker != nil {
			return &c.taker.PlayerID, nil
		}
		m := c.pickWeighted(c.onPitch)
		return &m.PlayerID, nil

	default:
		return nil, nil
	}
}

// resolveForced maps an engine substitution event using the manager's exact
// chosen players (S04-02/OPD-21): subIn comes on, subOut leaves. It replaces
// the bench draw so the feed links the players the manager actually picked.
// Conveniently mirrors resolve's return convention: primary = the player
// coming ON, related = the player replaced.
func (c *sideCaster) resolveForced(ev matchsim.MatchEvent, subIn, subOut uuid.UUID) (*uuid.UUID, *uuid.UUID) {
	if ev.Type != matchsim.EventSubstitution {
		return c.resolve(ev)
	}
	c.pendingSub = nil
	c.off[subOut] = true
	c.leave(subOut)
	for i := range c.bench {
		if c.bench[i].PlayerID == subIn {
			if !containsMember(c.onPitch, subIn) {
				c.onPitch = append(c.onPitch, c.bench[i])
			}
			return &subIn, &subOut
		}
	}
	// The chosen player is not on the snapshot bench (rare edge: e.g. already
	// brought on mid-match); link their ids without on-pitch bookkeeping.
	return &subIn, &subOut
}

// leave marks a player as no longer on the field.
func (c *sideCaster) leave(id uuid.UUID) {
	c.off[id] = true
	c.onPitch = without(c.onPitch, id)
}

// bestBench returns the highest-weight bench player not already involved on
// or off the pitch, or nil when the bench is exhausted.
func (c *sideCaster) bestBench() *squad.SquadMember {
	var best *squad.SquadMember
	for i := range c.bench {
		m := &c.bench[i]
		if c.off[m.PlayerID] || containsMember(c.onPitch, m.PlayerID) {
			continue
		}
		if best == nil || m.AttributeWeight > best.AttributeWeight {
			best = m
		}
	}
	return best
}

// pickWeighted draws a member by attribute weight (seeded). A zero/absent
// weight degenerates to the 50 neutral so an unweighted roster still samples.
func (c *sideCaster) pickWeighted(pool []squad.SquadMember) squad.SquadMember {
	ws := make([]float64, len(pool))
	var total float64
	for i, m := range pool {
		w := m.AttributeWeight
		if w <= 0 {
			w = 50
		}
		ws[i] = w
		total += w
	}
	if total <= 0 {
		return squad.SquadMember{}
	}
	u := c.r.nextFloat() * total
	for i, w := range ws {
		u -= w
		if u < 0 {
			return pool[i]
		}
	}
	return pool[len(pool)-1]
}

// betterAttackers is the goalward-leaning pool: the XI's attackers with their
// weight boosted (1.6×) so scoring lines stabilise on the strikers, falling
// back to the full XI when the formation fields no attackers.
func betterAttackers(xi []squad.SquadMember) []squad.SquadMember {
	out := make([]squad.SquadMember, 0, len(xi))
	for _, m := range xi {
		if isAttackerPos(m.Position) {
			m.AttributeWeight *= 1.6
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return xi
	}
	return out
}

func isAttackerPos(pos string) bool {
	return pos == "ST" || pos == "LW" || pos == "RW" || pos == "AM"
}

func without(xi []squad.SquadMember, id uuid.UUID) []squad.SquadMember {
	out := xi[:0]
	for _, m := range xi {
		if m.PlayerID != id {
			out = append(out, m)
		}
	}
	return out
}

func containsMember(xi []squad.SquadMember, id uuid.UUID) bool {
	for _, m := range xi {
		if m.PlayerID == id {
			return true
		}
	}
	return false
}

// castRNG is the tiny splitmix64 stream the cast draws from (replay-owned by
// the match seed + cast domain tag).
type castRNG struct {
	state uint64
}

func newCastRNG(seed uint64) *castRNG { return &castRNG{state: seed} }

func (r *castRNG) nextFloat() float64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float64(z>>11) / float64(1<<53)
}