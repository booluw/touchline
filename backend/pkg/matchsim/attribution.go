package matchsim

import (
	"hash/fnv"
	"math"
	"sort"

	"github.com/google/uuid"
)

// castTag separates the attribution stream's RNG domain from the match
// engine's canonical draw stream (v1.6 replay contract): the attribution pass
// draws EXACTLY from this independent per-side stream, so it can never perturb
// the v1.5 draw order. The tag and stream layout replicate the orchestration
// layer's pre-v1.6 caster so already-live matches re-link the same players.
const castTag uint64 = 0x4341535445440000 // "CASTED\0\0"

func fnv64a(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// clubCastSeed derives a side's attribution stream seed exactly as the caster
// did before v1.6: matchSeed ⊕ fnv64a(club UUID bytes) ⊕ castTag. A non-UUID
// club id (bare fixture callers) hashes its literal id bytes instead.
func clubCastSeed(matchSeed int64, clubID string) uint64 {
	if id, err := uuid.Parse(clubID); err == nil {
		return uint64(matchSeed) ^ fnv64a(id[:]) ^ castTag
	}
	return uint64(matchSeed) ^ fnv64a([]byte(clubID)) ^ castTag
}

// castSide is one club's live attribution state: the on-pitch set, the
// exhausted players, the bench pool, the penalty taker, and the cross-event
// links (a forced injury sub's pending player, the last scorer for assists).
// Its stream is seeded from the club, exactly mirroring the pre-v1.6 caster.
type castSide struct {
	r          *splitMix64
	onPitch    []PlayerRef
	off        map[string]bool
	bench      []PlayerRef
	taker      *PlayerRef
	pendingSub *string
	lastScorer *string
}

func castForSide(matchSeed int64, clubID string, l *PlayerLineups) *castSide {
	c := &castSide{
		r:       newSplitMix64(int64(clubCastSeed(matchSeed, clubID))),
		onPitch: append([]PlayerRef{}, l.XI...),
		off:     make(map[string]bool, len(l.XI)),
		bench:   l.Bench,
	}
	if l.Taker != "" {
		for i := range l.XI {
			if l.XI[i].ID == l.Taker {
				c.taker = &l.XI[i]
				break
			}
		}
	}
	return c
}

// resolve maps one engine event to its primary and related players. Plain
// markers (kickoff/half/full time) resolve to "", "".
func (c *castSide) resolve(ev MatchEvent) (string, string) {
	switch ev.Type {
	case EventGoal:
		sc := c.pickWeighted(betterAttackers(c.onPitch))
		c.lastScorer = &sc.ID
		return sc.ID, ""

	case EventAssist:
		pool := betterAttackers(c.onPitch)
		if len(pool) > 1 && c.lastScorer != nil {
			pool = withoutRef(pool, *c.lastScorer)
		}
		if len(pool) == 0 {
			pool = c.onPitch
		}
		a := c.pickWeighted(pool)
		if c.lastScorer == nil {
			return a.ID, ""
		}
		return a.ID, *c.lastScorer

	case EventChance:
		m := c.pickWeighted(c.onPitch)
		return m.ID, ""

	case EventYellowCard:
		m := c.pickWeighted(c.onPitch)
		return m.ID, ""

	case EventRedCard:
		m := c.pickWeighted(c.onPitch)
		c.leave(m.ID)
		return m.ID, ""

	case EventInjury:
		m := c.pickWeighted(c.onPitch)
		c.off[m.ID] = true
		c.pendingSub = &m.ID
		return m.ID, ""

	case EventSubstitution:
		var on string
		if c.pendingSub != nil {
			on = *c.pendingSub
			c.pendingSub = nil
		} else {
			m := c.pickWeighted(c.onPitch)
			on = m.ID
		}
		s := c.bestBench()
		if s == nil {
			return on, on // no bench to bring on; the change is cosmetic
		}
		off := s.ID
		c.off[off] = true
		c.leave(on)
		c.onPitch = append(c.onPitch, *s)
		return off, on // primary = the bench player coming ON, related = replaced

	case EventPenaltyScored, EventPenaltyMissed:
		if c.taker != nil {
			return c.taker.ID, ""
		}
		m := c.pickWeighted(c.onPitch)
		return m.ID, ""

	default:
		return "", ""
	}
}

// resolveForced maps an engine substitution event using the manager's exact
// chosen players (S04-02/OPD-21): subIn comes on, subOut leaves. It replaces
// the bench draw so the feed links the players the manager actually picked.
// Mirrors resolve's return convention: primary = the player coming ON, related
// = the player replaced.
func (c *castSide) resolveForced(subIn, subOut string) (string, string) {
	c.pendingSub = nil
	c.off[subOut] = true
	c.leave(subOut)
	for i := range c.bench {
		if c.bench[i].ID == subIn {
			if !containsRef(c.onPitch, subIn) {
				c.onPitch = append(c.onPitch, c.bench[i])
			}
			break
		}
	}
	// The chosen player is not on the snapshot bench (rare edge: e.g. already
	// brought on mid-match); their ids are linked without on-pitch bookkeeping.
	return subIn, subOut
}

// leave marks a player as no longer on the field.
func (c *castSide) leave(id string) {
	c.off[id] = true
	c.onPitch = withoutRef(c.onPitch, id)
}

// bestBench returns the highest-weight bench player not already involved on or
// off the pitch, or nil when the bench is exhausted.
func (c *castSide) bestBench() *PlayerRef {
	var best *PlayerRef
	for i := range c.bench {
		m := &c.bench[i]
		if c.off[m.ID] || containsRef(c.onPitch, m.ID) {
			continue
		}
		if best == nil || m.Weight > best.Weight {
			best = m
		}
	}
	return best
}

// pickWeighted draws a member by attribute weight from the club's attribution
// stream. A zero/absent weight degenerates to the neutral 50 so an unweighted
// lineup still samples.
func (c *castSide) pickWeighted(pool []PlayerRef) PlayerRef {
	ws := make([]float64, len(pool))
	var total float64
	for i, m := range pool {
		w := m.Weight
		if w <= 0 {
			w = 50
		}
		ws[i] = w
		total += w
	}
	if total <= 0 {
		return PlayerRef{}
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
func betterAttackers(xi []PlayerRef) []PlayerRef {
	out := make([]PlayerRef, 0, len(xi))
	for _, m := range xi {
		if isAttackerPosition(m.Position) {
			m.Weight *= 1.6
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return xi
	}
	return out
}

func isAttackerPosition(pos string) bool {
	return pos == "ST" || pos == "LW" || pos == "RW" || pos == "AM"
}

func withoutRef(xi []PlayerRef, id string) []PlayerRef {
	out := xi[:0]
	for _, m := range xi {
		if m.ID != id {
			out = append(out, m)
		}
	}
	return out
}

func containsRef(xi []PlayerRef, id string) bool {
	for _, m := range xi {
		if m.ID == id {
			return true
		}
	}
	return false
}

// subCast is one cast substitution (player coming on, player replaced) feeding
// the minutes derivation, mirroring the orchestration layer's appearance math.
type subCast struct {
	in, out string
	minute  int
}

// attribSide couples one club's cast state with its per-player tally accrual.
type attribSide struct {
	side    *castSide
	xi      []PlayerRef
	players map[string]*PlayerRating
	subs    []subCast
}

func newAttribSide(matchSeed int64, clubID string, l *PlayerLineups) *attribSide {
	as := &attribSide{
		side:    castForSide(matchSeed, clubID, l),
		xi:      append([]PlayerRef{}, l.XI...),
		players: make(map[string]*PlayerRating, len(l.XI)+len(l.Bench)),
	}
	for _, m := range as.xi {
		as.acc(m.ID)
	}
	return as
}

func (a *attribSide) acc(id string) *PlayerRating {
	p, ok := a.players[id]
	if !ok {
		p = &PlayerRating{PlayerID: id}
		a.players[id] = p
	}
	return p
}

// scores resolves a per-side rating list: minutes from the XI and the cast
// substitutions, then the 1..10 rating from the tuning block, sorted by player
// id for deterministic output.
func (a *attribSide) scores(t RatingsSpec) []PlayerRating {
	minutes := make(map[string]int, len(a.players))
	for _, m := range a.xi {
		minutes[m.ID] = 90
	}
	for _, s := range a.subs {
		minutes[s.out] -= 90 - s.minute
		minutes[s.in] += 90 - s.minute
	}
	out := make([]PlayerRating, 0, len(a.players))
	for _, p := range a.players {
		v := *p
		v.Minutes = clampPitch(minutes[v.PlayerID])
		v.Rating = matchRating(v, t)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlayerID < out[j].PlayerID })
	return out
}

// matchRating derives a player's 1..10 match rating from their tallies. The
// contribution is prorated toward the base for part-time appearances so a
// late cameo's score drifts toward neutral (numbers are proposal, addendum
// v1.6 §"Per-player match ratings").
func matchRating(p PlayerRating, t RatingsSpec) int {
	if t.Base <= 0 {
		t.Base = 6
	}
	half := float64(t.HalfMinutes)
	if half <= 0 {
		half = 45
	}
	raw := t.Base +
		float64(p.Goals)*t.Goal +
		float64(p.Assists)*t.Assist +
		float64(p.Chances)*t.Chance +
		float64(p.YellowCards)*t.Yellow +
		float64(p.RedCards)*t.Red +
		float64(p.PenaltiesScored)*t.PenaltyScored +
		float64(p.PenaltiesMissed)*t.PenaltyMissed
	boost := raw - t.Base
	if float64(p.Minutes) < half {
		boost *= float64(p.Minutes) / half
	}
	v := math.Round(t.Base + boost)
	if v < 1 {
		return 1
	}
	if v > 10 {
		return 10
	}
	return int(v)
}

func clampPitch(v int) int {
	if v < 0 {
		return 0
	}
	if v > 90 {
		return 90
	}
	return v
}

// substitutionPair extracts a manager's chosen players from a substitution
// LiveInput's Detail, reporting false when absent/malformed so the auto cast
// applies (the orchestration layer's forced-sub fallback).
func substitutionPair(in *LiveInput) (subCast, bool) {
	if in == nil || in.Detail == nil {
		return subCast{}, false
	}
	pIn, _ := in.Detail["player_in"].(string)
	pOut, _ := in.Detail["player_out"].(string)
	if pIn == "" || pOut == "" {
		return subCast{}, false
	}
	return subCast{in: pIn, out: pOut}, true
}

// attributeMatch runs the v1.6 attribution pass over a finished feed: it links
// every castable event to a player from each side's lineup (using that side's
// independent attribution stream, manager substitutions included) and derives
// the per-side player ratings. Sides without a Lineups cast are skipped.
func attributeMatch(opts Options, tuning Tuning, liveInputs liveInputs, res *MatchResult) {
	atts := make(map[string]*attribSide, 2)
	if opts.Home.Lineups != nil {
		atts[opts.Home.ID] = newAttribSide(opts.Seed, opts.Home.ID, opts.Home.Lineups)
	}
	if opts.Away.Lineups != nil {
		atts[opts.Away.ID] = newAttribSide(opts.Seed, opts.Away.ID, opts.Away.Lineups)
	}
	if len(atts) == 0 {
		return
	}

	evs := res.Events
	for i := range evs {
		ev := &evs[i]
		as, ok := atts[ev.ClubID]
		if !ok {
			continue
		}
		var pid, rid string
		if ev.Type == EventSubstitution {
			injDerived := i > 0 &&
				evs[i-1].Minute == ev.Minute &&
				evs[i-1].ClubID == ev.ClubID &&
				evs[i-1].Type == EventInjury
			if f, ok := substitutionPair(liveInputs.substitutionAt(ev.Minute, ev.ClubID)); ok && !injDerived {
				pid, rid = as.side.resolveForced(f.in, f.out)
			} else {
				pid, rid = as.side.resolve(*ev)
			}
		} else {
			pid, rid = as.side.resolve(*ev)
		}
		ev.PlayerID, ev.RelatedPlayerID = pid, rid

		switch ev.Type {
		case EventGoal:
			if pid != "" {
				as.acc(pid).Goals++
			}
		case EventAssist:
			if pid != "" {
				as.acc(pid).Assists++
			}
		case EventChance:
			if pid != "" {
				as.acc(pid).Chances++
			}
		case EventYellowCard:
			if pid != "" {
				as.acc(pid).YellowCards++
			}
		case EventRedCard:
			if pid != "" {
				as.acc(pid).RedCards++
			}
		case EventPenaltyScored:
			if pid != "" {
				as.acc(pid).PenaltiesScored++
			}
		case EventPenaltyMissed:
			if pid != "" {
				as.acc(pid).PenaltiesMissed++
			}
		case EventSubstitution:
			if pid != "" && rid != "" {
				as.acc(pid)
				as.acc(rid)
				as.subs = append(as.subs, subCast{in: pid, out: rid, minute: ev.Minute})
			}
		}
	}

	if as, ok := atts[opts.Home.ID]; ok {
		res.HomePlayerRatings = as.scores(tuning.Ratings)
	}
	if as, ok := atts[opts.Away.ID]; ok {
		res.AwayPlayerRatings = as.scores(tuning.Ratings)
	}
}