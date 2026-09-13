// XI selection (matchsim addendum v1.4 Part 8). No lineup concept is
// persisted for AI clubs: the engine's starting XI is derived deterministically
// from the generated squad at matchday, replayed identically from the same
// persisted squad + fixture seed.
//
//   - User-managed clubs: the manager's own XI (club.club_lineups) is honored,
//     with unavailable slots falling back to the deterministic selector.
//   - AI-managed clubs: a SEEDED draw "random, factoring injury and morale"
//     (PM): unavailable players are excluded, unhappy players start less
//     often, and the draw comes from the per-club fixture stream so the same
//     world replays byte-identical XIs.
//
// The shared slot order (Internal/template) keeps the persisted lineup table
// and the AI path interoperable.
package squad

import (
	"fmt"

	"github.com/google/uuid"
)

// lineupTag separates the lineup draw's RNG domain from other per-club
// orchestration draws (motivation, speculation) so two decisions sharing one
// club seed are not perfectly correlated.
const lineupTag uint64 = 0x4C494E4555500000 // "LINEUP\0\0"

// DefaultFormation is the fixed 4-3-3 slot order (GK, LB, CB, CB, RB, CM, CM,
// CM, RW, ST, LW). Slot i maps to club.club_lineups.slot i, so a persisted
// human XI always fills a legal starting eleven.
func DefaultFormation() []string {
	return []string{"GK", "LB", "CB", "CB", "RB", "CM", "CM", "CM", "RW", "ST", "LW"}
}

// SelectStarters fills the formation deterministically from the best available
// players: exact position matches first, then positional adjacency, then any
// remaining available player. It is the universal fallback for human-club gaps
// and the reference selector when no persisted lineup exists.
func SelectStarters(players []LoadedPlayer) ([]SquadMember, error) {
	avail := availablePlayers(players)
	if len(avail) < len(DefaultFormation()) {
		return nil, fmt.Errorf("only %d available players for an %d-man XI", len(avail), len(DefaultFormation()))
	}
	taken := make(map[uuid.UUID]bool, len(avail))
	xi := make([]SquadMember, 0, len(DefaultFormation()))
	for _, slot := range DefaultFormation() {
		best := pickBestFor(slot, avail, taken)
		if best == nil {
			best = pickBestForAny(avail, taken)
		}
		if best == nil {
			return nil, fmt.Errorf("cannot fill slot %q from %d available players", slot, len(avail))
		}
		taken[best.PlayerID] = true
		xi = append(xi, asSquadMember(*best))
	}
	return xi, nil
}

// SelectStartersForAI draws the AI starting XI from the per-club fixture seed:
// weighted random over available candidates for each slot, weight =
// position-fit × sentiment nudge (an unhappy player starts less, an on-fire
// one more) — the PM's "random, factoring injury and maybe morale". The draw
// is deterministic for a fixed (squad, matchSeed, clubID).
func SelectStartersForAI(players []LoadedPlayer, matchSeed int64, clubID uuid.UUID) []SquadMember {
	r := newRNG(matchSeed, hashClub(clubID)^lineupTag)
	avail := availablePlayers(players)

	taken := make(map[uuid.UUID]bool, len(avail))
	xi := make([]SquadMember, 0, len(DefaultFormation()))
	for _, slot := range DefaultFormation() {
		cands := candidatesFor(slot, avail, taken)
		if len(cands) == 0 {
			cands = remainingAvailable(avail, taken)
		}
		if len(cands) == 0 {
			break
		}
		pick := weightedPick(r, cands, slot)
		taken[pick.PlayerID] = true
		xi = append(xi, asSquadMember(pick))
	}
	// Defensive refill: a squad that somehow starves a slot mid-loop (e.g.
	// fewer than 11 candidates) still finishes deterministic — no draw, best
	// remaining by weight for each open slot.
	for len(xi) < len(DefaultFormation()) {
		best := pickBestForAny(avail, taken)
		if best == nil {
			break
		}
		taken[best.PlayerID] = true
		xi = append(xi, asSquadMember(*best))
	}
	return xi
}

// SelectStartersWithLineup honours a user-managed club's persisted XI: each
// slot's chosen player starts when they exist in the squad and are available;
// unavailable or absent slots fall back through the deterministic selector via
// the remaining pool.
func SelectStartersWithLineup(players []LoadedPlayer, lineup map[int]uuid.UUID) ([]SquadMember, error) {
	byID := make(map[uuid.UUID]LoadedPlayer, len(players))
	for _, p := range players {
		byID[p.PlayerID] = p
	}
	avail := availablePlayers(players)

	taken := make(map[uuid.UUID]bool, len(avail))
	xi := make([]SquadMember, 0, len(DefaultFormation()))
	for slot, slotPos := range DefaultFormation() {
		if pid, ok := lineup[slot]; ok {
			if p, ok := byID[pid]; ok && p.Available && !taken[pid] {
				taken[pid] = true
				xi = append(xi, asSquadMember(p))
				continue
			}
		}
		best := pickBestFor(slotPos, avail, taken)
		if best == nil {
			best = pickBestForAny(avail, taken)
		}
		if best == nil {
			return nil, fmt.Errorf("cannot fill slot %d (%s)", slot, slotPos)
		}
		taken[best.PlayerID] = true
		xi = append(xi, asSquadMember(*best))
	}
	return xi, nil
}

// asSquadMember converts a LoadedPlayer into the aggregation unit, fixing its
// AttributeWeight = the member's own (attack+defense) weighted-score mean —
// the same per-position profiles BuildSquadRatings uses, so selection ranking
// and rating aggregation cannot disagree.
func asSquadMember(p LoadedPlayer) SquadMember {
	return SquadMember{
		PlayerID:         p.PlayerID,
		Position:         p.Position,
		Leadership:       p.Leadership,
		Hidden:           p.Hidden,
		Attributes:       p.Attributes,
		CurrentSentiment: p.CurrentSentiment,
		AttributeWeight:  memberWeight(p),
	}
}

// MemberWeight exposes the (attack+defense)/2 weight used for selection and
// bench ordering to orchestrators that need to sort non-selected players
// (e.g. internal/match's bench pool).
func MemberWeight(p LoadedPlayer) float64 { return memberWeight(p) }

// ToSquadMember converts a loaded player into the aggregation unit (same as
// the interval asSquadMember used by the selectors).
func ToSquadMember(p LoadedPlayer) SquadMember { return asSquadMember(p) }

// memberWeight = (attackScore + defenseScore) / 2 for the member's position.
func memberWeight(p LoadedPlayer) float64 {
	prof, ok := DefaultPositionWeights[p.Position]
	if !ok {
		prof = PositionProfile{Attack: balancedWeights, Defense: balancedWeights}
	}
	return (weightedScore(p.Attributes, prof.Attack) + weightedScore(p.Attributes, prof.Defense)) / 2
}

// availablePlayers keeps only players that may start.
func availablePlayers(players []LoadedPlayer) []LoadedPlayer {
	out := make([]LoadedPlayer, 0, len(players))
	for _, p := range players {
		if p.Available {
			out = append(out, p)
		}
	}
	return out
}

// candidatesFor returns available, untaken players for a slot: exact position
// matches first, then positional-adjacent, then any — each already ordered by
// the caller's needs via positionFit weighting.
func candidatesFor(slot string, avail []LoadedPlayer, taken map[uuid.UUID]bool) []LoadedPlayer {
	out := make([]LoadedPlayer, 0, len(avail))
	for _, p := range avail {
		if !taken[p.PlayerID] && positionFit(p.Position, slot) > 0 {
			out = append(out, p)
		}
	}
	return out
}

func remainingAvailable(avail []LoadedPlayer, taken map[uuid.UUID]bool) []LoadedPlayer {
	out := make([]LoadedPlayer, 0, len(avail))
	for _, p := range avail {
		if !taken[p.PlayerID] {
			out = append(out, p)
		}
	}
	return out
}

// pickBestFor selects the deterministic best candidate for a slot: exact
// position match, then adjacency, then any — ties by member weight, then (for
// matches) anchor priority.
func pickBestFor(slot string, avail []LoadedPlayer, taken map[uuid.UUID]bool) *LoadedPlayer {
	var best *LoadedPlayer
	for i := range avail {
		p := &avail[i]
		if taken[p.PlayerID] {
			continue
		}
		f := positionFit(p.Position, slot)
		if f <= 0 {
			continue
		}
		if best == nil || betterFor(p, best, slot) {
			best = p
		}
	}
	return best
}

func pickBestForAny(avail []LoadedPlayer, taken map[uuid.UUID]bool) *LoadedPlayer {
	var best *LoadedPlayer
	for i := range avail {
		p := &avail[i]
		if taken[p.PlayerID] {
			continue
		}
		if best == nil || memberWeight(*p) > memberWeight(*best) {
			best = p
		}
	}
	return best
}

// betterFor ranks candidates for a slot: fit first (exact > adjacent > other),
// weight next, anchor priority last.
func betterFor(a, b *LoadedPlayer, slot string) bool {
	fa := positionFit(a.Position, slot)
	fb := positionFit(b.Position, slot)
	if fa != fb {
		return fa > fb
	}
	wa := memberWeight(*a)
	wb := memberWeight(*b)
	if wa != wb {
		return wa > wb
	}
	return priorityScore(asSquadMember(*a)) > priorityScore(asSquadMember(*b))
}

// weightedPick draws one candidate seeded by r, weight = fit × sentiment
// nudge. Determining deterministically: every slot consumes exactly one draw
// from the club's stream, so the same squad+seed reproduces the same XI.
func weightedPick(r *rng, cands []LoadedPlayer, slot string) LoadedPlayer {
	ws := make([]float64, len(cands))
	var total float64
	for i, c := range cands {
		w := positionFit(c.Position, slot) * sentimentNudge(c.CurrentSentiment)
		if w <= 0 {
			w = 0.01
		}
		ws[i] = w
		total += w
	}
	u := r.nextFloat() * total
	for i, w := range ws {
		u -= w
		if u < 0 {
			return cands[i]
		}
	}
	return cands[len(cands)-1]
}

// sentimentNudge biases the AI draw toward morale: a very happy player starts
// somewhat more, a very unhappy one clearly less (an angry starter is possible,
// just rarer — morale "factors in"). Band [0.6, 1.4].
func sentimentNudge(sent int) float64 {
	return 1 + float64(clampSentiment(sent))/100*0.2
}

// positionFit scores a candidate vs a slot: exact 1.0, same positional family
// 0.75, cross-family 0.3, and a keeper never plays outfield (nor an outfielder
// in goal beyond a 0.05 desperation floor).
func positionFit(candidate, slot string) float64 {
	if candidate == slot {
		return 1.0
	}
	switch {
	case isDefensive(candidate) && isDefensive(slot):
		return 0.75
	case isMidfield(candidate) && isMidfield(slot):
		return 0.75
	case isForward(candidate) && isForward(slot):
		return 0.75
	case slot == "GK" || candidate == "GK":
		return 0.05
	default:
		return 0.3
	}
}

func isDefensive(pos string) bool {
	return pos == "GK" || pos == "CB" || pos == "LB" || pos == "RB"
}

func isMidfield(pos string) bool {
	return pos == "DM" || pos == "CM" || pos == "AM" || pos == "LM" || pos == "RM"
}

func isForward(pos string) bool {
	return pos == "ST" || pos == "LW" || pos == "RW"
}