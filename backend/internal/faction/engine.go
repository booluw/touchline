package faction

import (
	"sort"

	"github.com/touchline/backend/pkg/explanation"
)

// Numerical constants for the deterministic engine. All are proposal values
// awaiting PM sign-off (docs/design/squad-dynamics-numerics.md); recalibration
// means editing these constants (and the doc), never the steering logic.
const (
	// Contagion starts well under 100 and hardens hop by hop, so unrest is
	// always a gradual, evidenced outcome rather than an instant switch.
	ContagionStart    = 24
	ContagionHopBump  = 10
	ContagionHopDecay = 2
	ContagionCap      = 84

	// Unrest thresholds: contagion below the attention line is just mood; at
	// boarding-meeting level the room sends a delegation; en-masse at the top.
	UnrestThreshold  = 60
	EnMasseThreshold = 80

	// Bonding (approved contracts / motivational talks) raises cohesion.
	BondingCohesionBump = 6
	CohesionMax         = 95
	CohesionMin         = 10
	// CohesionNeutral is the midpoint; positive bonds pull above it.
	CohesionNeutral = 50

	// Hierarchy cutoffs by centrality rank.
	TierHighlyEnd      = 3 // ranks 0..2: team leader + highly influential
	TierInfluentialEnd = 6 // ranks 3..5: influential; ranks 6+: other

	// Veteran label also triggers on a high mean leadership.
	VeteranLeadership = 70
)

// Engine is the stateless, deterministic, DB-free faction engine.
type Engine struct{}

// NewEngine returns a deterministic faction engine.
func NewEngine() *Engine { return &Engine{} }

// Evaluate resolves one management action over the squad snapshot. Structure
// (tiers + factions) is always computed; the action selects the contagion or
// bonding branch. ActionInspect computes structure only.
func (e *Engine) Evaluate(a Action, snap SquadSnapshot) *SquadDynamics {
	res := &SquadDynamics{Action: a}
	res.Tiers = e.tiersOf(snap)
	res.Factions = e.factionsOf(snap)

	switch a {
	case ActionInspect:
		// structure only
	case ActionContractApproved, ActionTeamTalkMotivational:
		e.cohere(res)
	default:
		if epi, ok := e.epicentreFor(a, snap); ok {
			res.Contagion = e.Contagion(epi, snap)
		}
	}
	if res.Contagion != nil {
		res.Unrest = e.UnrestFrom(res.Contagion)
	}

	res.Explanation = explanation.New("squad_dynamics", len(res.Factions)).
		Add("squad members", len(snap.Members)).
		Add("relationship edges", len(snap.Edges)).
		Add("factions", len(res.Factions))
	return res
}

// Contagion runs a deterministic, bounded BFS morale spill from src. It returns
// nil when src is not in the snapshot.
func (e *Engine) Contagion(src string, snap SquadSnapshot) *Contagion {
	if !e.hasMember(snap, src) {
		return nil
	}
	adj := adjacency(snap)

	seen := map[string]bool{src: true}
	order := []string{src}
	conf := ContagionStart
	queue := []depthNode{{id: src, depth: 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range adj[cur.id] {
			if seen[nb] {
				continue
			}
			seen[nb] = true
			conf += hopBump(cur.depth + 1)
			if conf > ContagionCap {
				conf = ContagionCap
			}
			order = append(order, nb)
			queue = append(queue, depthNode{id: nb, depth: cur.depth + 1})
			if conf >= ContagionCap {
				break
			}
		}
		if conf >= ContagionCap {
			break
		}
	}
	return &Contagion{
		Source:     src,
		Confidence: conf,
		Affected:   order,
		Explanation: explanation.New("dressing_room_contagion", conf).
			Add("epicentre influence", e.influenceOf(src, snap)).
			Add("affected team-mates", len(order)-1).
			Add("contagion cap", ContagionCap).
			Add("hops", len(order)-1),
	}
}

// hopBump is the confidence gained from a node at the given depth: full bump at
// one hop, decaying (never below 1) for further hops.
func hopBump(depth int) int {
	if depth <= 1 {
		return ContagionHopBump
	}
	b := ContagionHopBump - (depth-1)*ContagionHopDecay
	if b < 1 {
		return 1
	}
	return b
}

type depthNode struct {
	id    string
	depth int
}

// UnrestFrom turns severe contagion into documented dressing-room unrest. It
// returns nil below the attention threshold.
func (e *Engine) UnrestFrom(c *Contagion) *Unrest {
	if c == nil || c.Confidence < UnrestThreshold {
		return nil
	}
	demand := DemandBoardMeeting
	if c.Confidence >= EnMasseThreshold {
		demand = DemandEnMasseTransferReqs
	}
	return &Unrest{
		Severity: c.Confidence,
		Demand:   demand,
		Affected: c.Affected,
		Explanation: explanation.New("squad_unrest", c.Confidence).
			Add("contagion from epicentre", c.Confidence).
			Add("affected players", len(c.Affected)).
			Add("delegation demand", len(c.Affected)),
	}
}

// cohere is the bonding branch: approved contracts / motivational team talks
// raise faction cohesion deterministically (never contagion).
func (e *Engine) cohere(res *SquadDynamics) {
	for i := range res.Factions {
		if res.Factions[i].Cohesion >= CohesionMax {
			continue
		}
		res.Factions[i].Cohesion += BondingCohesionBump
		if res.Factions[i].Cohesion > CohesionMax {
			res.Factions[i].Cohesion = CohesionMax
		}
	}
}

// tiersOf derives the deterministic hierarchy from graph centrality.
func (e *Engine) tiersOf(snap SquadSnapshot) map[string]Tier {
	cent := centrality(snap)
	order := memberIDs(snap)
	sort.Slice(order, func(i, j int) bool {
		if cent[order[i]] != cent[order[j]] {
			return cent[order[i]] > cent[order[j]]
		}
		return order[i] < order[j]
	})
	out := make(map[string]Tier, len(order))
	for i, id := range order {
		switch {
		case i == 0:
			out[id] = TierTeamLeader
		case i < TierHighlyEnd:
			out[id] = TierHighlyInfluential
		case i < TierInfluentialEnd:
			out[id] = TierInfluential
		default:
			out[id] = TierOther
		}
	}
	return out
}

// factionsOf groups the squad into connected components. When the snapshot
// carries CTE-derived Roots those are authoritative; otherwise the engine
// union-finds the edges so it stays a pure function.
func (e *Engine) factionsOf(snap SquadSnapshot) []Faction {
	cent := centrality(snap)
	byID := membersByID(snap)
	groups := e.components(snap)

	leaderOf := func(ids []string) string {
		leader := ids[0]
		for _, id := range ids[1:] {
			if cent[id] > cent[leader] || (cent[id] == cent[leader] && id < leader) {
				leader = id
			}
		}
		return leader
	}

	leaders := make([]string, 0, len(groups))
	for _, ids := range groups {
		leaders = append(leaders, leaderOf(ids))
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i] < leaders[j] })

	memberSet := make(map[string]bool, len(snap.Members))
	for _, m := range snap.Members {
		memberSet[m.PlayerID] = true
	}

	out := make([]Faction, 0, len(groups))
	for _, leader := range leaders {
		ids := groupForLeader(groups, cent, leader)
		cohesion := factionCohesion(ids, snap)
		label := factionLabel(ids, snap, byID)
		out = append(out, Faction{
			Label:    label,
			LeaderID: leader,
			Members:  ids,
			Cohesion: cohesion,
			Explanation: explanation.New("faction_cohesion", cohesion).
				Add("members", len(ids)).
				Add("label", labelScore(label)).
				Add("cohesion", cohesion),
		})
	}
	return out
}

// components maps each member to its connected-component member set.
func (e *Engine) components(snap SquadSnapshot) map[string][]string {
	ids := memberIDs(snap)
	root := make(map[string]string, len(ids))

	if len(snap.Roots) > 0 {
		for _, id := range ids {
			r, ok := snap.Roots[id]
			if !ok || r == "" {
				r = id
			}
			root[id] = r
		}
	} else {
		parent := make(map[string]string, len(ids))
		var find func(string) string
		find = func(x string) string {
			if _, ok := parent[x]; !ok {
				parent[x] = x
			}
			if parent[x] != x {
				parent[x] = find(parent[x])
			}
			return parent[x]
		}
		union := func(a, b string) {
			ra, rb := find(a), find(b)
			if ra == rb {
				return
			}
			if ra > rb {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
		for _, ed := range snap.Edges {
			union(ed.From, ed.To)
		}
		for _, id := range ids {
			root[id] = find(id)
		}
	}

	groups := make(map[string][]string)
	for _, id := range ids {
		r := root[id]
		groups[r] = append(groups[r], id)
	}
	for r := range groups {
		sort.Strings(groups[r])
	}
	return groups
}

// groupForLeader finds the member set whose leader is the given id.
func groupForLeader(groups map[string][]string, cent map[string]int, leader string) []string {
	for _, ids := range groups {
		best := ids[0]
		for _, id := range ids[1:] {
			if cent[id] > cent[best] || (cent[id] == cent[best] && id < best) {
				best = id
			}
		}
		if best == leader {
			return ids
		}
	}
	return nil
}

// factionCohesion is bounded [10,95]: 50 neutral, positive bonds pull it up,
// rivalries pull it down.
func factionCohesion(ids []string, snap SquadSnapshot) int {
	in := make(map[string]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	sum, n := 0, 0
	for _, ed := range snap.Edges {
		if in[ed.From] && in[ed.To] {
			sum += ed.Strength + ed.Trust
			n++
		}
	}
	if n == 0 {
		return CohesionMin
	}
	avg := sum / (2 * n) // -100..100
	c := CohesionNeutral + avg/2
	return clampInt(c, CohesionMin, CohesionMax)
}

// factionLabel labels a component from its dominant relationship kind, falling
// back to member leadership for the veteran archetype.
func factionLabel(ids []string, snap SquadSnapshot, byID map[string]SquadMember) string {
	in := make(map[string]bool, len(ids))
	for _, id := range ids {
		in[id] = true
	}
	counts := map[string]int{}
	for _, ed := range snap.Edges {
		if in[ed.From] && in[ed.To] {
			counts[ed.Kind]++
		}
	}
	switch {
	case counts[KindNationalTeam] > 0 && counts[KindNationalTeam] >= maxKind(counts):
		return LabelForeignCohort
	case counts[KindAcademy] > 0 && counts[KindAcademy] >= maxKind(counts):
		return LabelYouthAlliance
	case counts[KindMentorship] > 0 && counts[KindMentorship] >= maxKind(counts):
		return LabelVeteranCore
	}
	sum, n := 0, 0
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			sum += m.Leadership
			n++
		}
	}
	if n > 0 && sum/n >= VeteranLeadership {
		return LabelVeteranCore
	}
	return LabelNeutralRoom
}

func maxKind(counts map[string]int) int {
	max := 0
	for _, v := range counts {
		if v > max {
			max = v
		}
	}
	return max
}

func labelScore(label string) int {
	switch label {
	case LabelVeteranCore:
		return 3
	case LabelForeignCohort:
		return 2
	case LabelYouthAlliance:
		return 1
	default:
		return 0
	}
}

// epicentreFor picks the deterministic epicentre for an action: the member the
// action most plausibly touches, tie-broken by PlayerID.
func (e *Engine) epicentreFor(a Action, snap SquadSnapshot) (string, bool) {
	score := func(m SquadMember) int {
		switch a {
		case ActionReleasedFromSquad, ActionDroppedFromStartingXI, ActionBenchedLongTerm:
			return m.Leadership*10 + m.Loyalty
		case ActionMissedPromise, ActionWageCutOffered:
			return m.Volatility*10 + m.Loyalty
		case ActionTransferBidBlocked:
			return m.Leadership*10 + m.Volatility
		case ActionTransferBidQuery:
			return m.Leadership * 10
		case ActionTrainingLoadIncreased:
			return m.Volatility*10 + m.Loyalty
		case ActionTeamTalkCritical:
			return m.Volatility*10 + m.Leadership
		default:
			return m.Leadership*10 + m.Loyalty
		}
	}
	best, bestScore := "", -1
	for _, m := range snap.Members {
		s := score(m)
		if s > bestScore || (s == bestScore && (best == "" || m.PlayerID < best)) {
			best, bestScore = m.PlayerID, s
		}
	}
	return best, best != ""
}

func (e *Engine) influenceOf(id string, snap SquadSnapshot) int {
	cent := centrality(snap)
	if v, ok := cent[id]; ok {
		return v
	}
	return 0
}

func (e *Engine) hasMember(snap SquadSnapshot, id string) bool {
	for _, m := range snap.Members {
		if m.PlayerID == id {
			return true
		}
	}
	return false
}

// centrality = Σ(strength + trust) over incident edges.
func centrality(snap SquadSnapshot) map[string]int {
	cent := make(map[string]int, len(snap.Members))
	for _, m := range snap.Members {
		cent[m.PlayerID] = 0
	}
	for _, ed := range snap.Edges {
		w := ed.Strength + ed.Trust
		cent[ed.From] += w
		cent[ed.To] += w
	}
	return cent
}

func adjacency(snap SquadSnapshot) map[string][]string {
	adj := map[string][]string{}
	for _, m := range snap.Members {
		if _, ok := adj[m.PlayerID]; !ok {
			adj[m.PlayerID] = nil
		}
	}
	for _, ed := range snap.Edges {
		adj[ed.From] = append(adj[ed.From], ed.To)
		adj[ed.To] = append(adj[ed.To], ed.From)
	}
	for id := range adj {
		sort.Strings(adj[id])
		adj[id] = dedupeSorted(adj[id])
	}
	return adj
}

func dedupeSorted(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

func memberIDs(snap SquadSnapshot) []string {
	ids := make([]string, 0, len(snap.Members))
	for _, m := range snap.Members {
		ids = append(ids, m.PlayerID)
	}
	sort.Strings(ids)
	return ids
}

func membersByID(snap SquadSnapshot) map[string]SquadMember {
	out := make(map[string]SquadMember, len(snap.Members))
	for _, m := range snap.Members {
		out[m.PlayerID] = m
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
