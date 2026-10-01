// Package pitchsim is the positional match engine (IM34). It is pure and
// deterministic like pkg/matchsim, and it never decides anything: matchsim's
// events are an input, and pitchsim produces the movement that realises them
// (the goal matchsim recorded in minute 37 is a move that ends with that player
// scoring in minute 37) plus extra, outcome-neutral events (shots, saves,
// tackles, fouls, corners, offsides). See README.md for the contract.
package pitchsim

import (
	"math"
	"sort"
)

const (
	// Version is the track format/behaviour version. Bump it on any change that
	// alters generated output: stored extra events belong to one version.
	Version = 1
	// FramesPerMinute keyframes are produced per match minute, FrameMillis apart
	// in match time. The renderer interpolates between them.
	FramesPerMinute = 12
	FrameMillis     = 60000 / FramesPerMinute
	// Scale is the integer coordinate range: x runs goal line to goal line, y
	// touchline to touchline, both 0..Scale. Off-pitch players are {-1,-1}.
	Scale = 1000
	// ExtraSequenceBase is where pitchsim event sequence numbers start, clear of
	// matchsim's (which stay exactly as matchsim numbered them).
	ExtraSequenceBase = 100000
)

// matchsim event types this engine scripts (kept as strings so the package has
// no dependency on matchsim).
const (
	evKickoff    = "kickoff"
	evGoal       = "goal"
	evAssist     = "assist"
	evChance     = "chance_created"
	evYellow     = "yellow_card"
	evRed        = "red_card"
	evSub        = "substitution"
	evPenAwarded = "penalty_awarded"
	evPenScored  = "penalty_scored"
	evPenMissed  = "penalty_missed"
	evHalfTime   = "half_time"
	evFullTime   = "full_time"
)

// Extra event types — the superset pitchsim adds. None of them changes a score.
const (
	ExtraShot    = "shot"
	ExtraSave    = "save"
	ExtraTackle  = "tackle"
	ExtraFoul    = "foul"
	ExtraCorner  = "corner"
	ExtraOffside = "offside"
)

// ExtraTypes lists every extra event type (the migration's CHECK mirrors it).
var ExtraTypes = []string{ExtraShot, ExtraSave, ExtraTackle, ExtraFoul, ExtraCorner, ExtraOffside}

// Player is one squad member: id and primary position (GK, CB, LB, ...).
type Player struct {
	ID       string
	Position string
}

// Side is one team's starting XI. Bench players need no entry: a substitute
// takes over the slot of the player replaced.
type Side struct {
	ClubID string
	XI     []Player
}

// Event is one matchsim event, exactly as matchsim produced it.
type Event struct {
	Sequence        int
	Minute          int
	Type            string
	ClubID          string
	PlayerID        string
	RelatedPlayerID string
}

// Input is everything Generate reads. Events must hold every matchsim event up
// to Minutes; later events are ignored.
type Input struct {
	Seed       int64
	Home, Away Side
	// HomeShare is the home side's share of the ball (0..1), fixed for the whole
	// match so a minute never changes once generated.
	HomeShare float64
	Events    []Event
	Minutes   int
}

// Frame is one keyframe: T milliseconds into the minute, the ball {x,y,height}
// and the 22 player positions (home slots 0-10, then away).
type Frame struct {
	T       int        `json:"t"`
	Ball    [3]int     `json:"b"`
	Players [22][2]int `json:"p"`
}

// Cue places one event (either engine's, by sequence) inside its minute.
type Cue struct {
	Sequence int `json:"seq"`
	T        int `json:"t"`
}

// Extra is one pitchsim-only event.
type Extra struct {
	Sequence int
	Minute   int
	Offset   int
	Type     string
	ClubID   string
	PlayerID string
	Text     string // commentary; {player} is resolved by the feed layer
}

// Minute is one match minute of track.
type Minute struct {
	Minute int        `json:"minute"`
	Lineup [22]string `json:"lineup"` // player id per slot at the start of the minute; "" = nobody
	Frames []Frame    `json:"frames"`
	Cues   []Cue      `json:"cues"`
	Passes [2]int     `json:"passes"` // completed passes, home then away
	Extras []Extra    `json:"-"`
}

// Track is the generated movement for minutes 1..len(Minutes).
type Track struct {
	Version int      `json:"version"`
	Minutes []Minute `json:"minutes"`
}

type vec struct{ x, y float64 }

type rng struct{ s uint64 }

func (r *rng) float() float64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return float64((z^(z>>31))>>11) * (1.0 / (1 << 53))
}

// seedTag separates pitchsim's RNG stream from matchsim's, which is seeded with
// the bare match seed.
const seedTag uint64 = 0x5049544348534D31 // "PITCHSM1"

// roleBase is where each position stands, for a team attacking towards x = 1.
var roleBase = map[string]vec{
	"GK": {0.05, 0.50}, "CB": {0.20, 0.50}, "LB": {0.25, 0.12}, "RB": {0.25, 0.88},
	"DM": {0.36, 0.50}, "CM": {0.48, 0.50}, "LM": {0.52, 0.12}, "RM": {0.52, 0.88},
	"AM": {0.62, 0.50}, "LW": {0.72, 0.15}, "RW": {0.72, 0.85}, "ST": {0.80, 0.50},
}

type restart struct {
	side   int
	centre bool // kick-off from the centre spot; otherwise a goal kick / free ball to the side's keeper
}

type gen struct {
	r       rng
	share   float64
	clubs   [2]string
	on      [2][11]string
	base    [2][11]vec
	gk      [2]int
	leaving [2][11]bool
	pending []func()
	poss    int
	holder  int
	ball    vec
	pos     [2][11]vec
	restart *restart
	seq     int
	cur     *Minute
}

// Generate produces the track for minutes 1..in.Minutes. The output for a
// minute depends only on the seed, the sides and the events up to that minute,
// so generating a longer match never changes an earlier minute.
func Generate(in Input) Track {
	g := &gen{r: rng{s: uint64(in.Seed) ^ seedTag}, share: math.Max(0.35, math.Min(0.65, in.HomeShare))}
	if in.HomeShare <= 0 {
		g.share = 0.5
	}
	for s, side := range []Side{in.Home, in.Away} {
		g.clubs[s] = side.ClubID
		g.place(s, side.XI)
	}
	byMinute := map[int][]Event{}
	for _, e := range in.Events {
		m := e.Minute
		if m < 1 {
			m = 1
		}
		if m <= in.Minutes {
			byMinute[m] = append(byMinute[m], e)
		}
	}
	g.centre(0)

	out := Track{Version: Version, Minutes: make([]Minute, 0, in.Minutes)}
	for m := 1; m <= in.Minutes; m++ {
		for _, apply := range g.pending {
			apply()
		}
		g.pending = nil
		evs := byMinute[m]
		sort.SliceStable(evs, func(a, b int) bool { return evs[a].Sequence < evs[b].Sequence })
		out.Minutes = append(out.Minutes, g.minute(m, evs))
	}
	return out
}

// place gives each starter a slot and a base position from their role.
func (g *gen) place(s int, xi []Player) {
	count := map[string]int{}
	for i := 0; i < len(xi) && i < 11; i++ {
		count[role(xi[i].Position)]++
	}
	seen := map[string]int{}
	g.gk[s] = 0
	for i := 0; i < len(xi) && i < 11; i++ {
		r := role(xi[i].Position)
		b := roleBase[r]
		k, n := seen[r], count[r]
		seen[r]++
		switch {
		case r == "GK":
			if k == 0 {
				g.gk[s] = i
			}
		case b.y == 0.5 && n > 1: // central roles spread across the pitch
			b.y = 0.5 + (float64(k)-float64(n-1)/2)*math.Min(0.28, 0.7/float64(n-1))
		case n > 1: // a second wide player tucks in behind the first
			b.x -= 0.08 * float64(k)
		}
		g.on[s][i] = xi[i].ID
		g.base[s][i] = b
	}
}

func role(pos string) string {
	if _, ok := roleBase[pos]; ok {
		return pos
	}
	return "CM"
}

// abs converts a team-frame point (team attacks towards x = 1) to pitch
// coordinates (home attacks towards x = 1).
func abs(s int, p vec) vec {
	if s == 0 {
		return p
	}
	return vec{1 - p.x, 1 - p.y}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func dist(a, b vec) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

// shape moves both teams relative to the ball: the block slides up and across
// with it, the side in possession pushes on, the other drops off.
func (g *gen) shape() {
	for s := 0; s < 2; s++ {
		bt := abs(s, g.ball)
		push := (bt.x - 0.5) * 0.45
		if s == g.poss {
			push += 0.05
		} else {
			push -= 0.03
		}
		for i := 0; i < 11; i++ {
			if g.on[s][i] == "" {
				continue
			}
			if g.leaving[s][i] { // sent off: walk to the touchline
				g.pos[s][i] = vec{0.5, 0}
				continue
			}
			f := 1.0
			if i == g.gk[s] {
				f = 0.12
			}
			b := g.base[s][i]
			g.pos[s][i] = abs(s, vec{
				clamp(b.x+push*f+(g.r.float()-0.5)*0.03, 0.02, 0.98),
				clamp(b.y+(bt.y-0.5)*0.25*f+(g.r.float()-0.5)*0.03, 0.02, 0.98),
			})
		}
	}
	// The nearest opponent closes the ball down.
	o := 1 - g.poss
	if i := g.nearest(o, g.ball, true); i >= 0 {
		p := g.pos[o][i]
		g.pos[o][i] = vec{p.x + (g.ball.x-p.x)*0.5, p.y + (g.ball.y-p.y)*0.5}
	}
}

// nearest returns the side's on-pitch slot closest to p (-1 when the side has
// nobody), optionally ignoring the goalkeeper.
func (g *gen) nearest(s int, p vec, outfield bool) int {
	best, bd := -1, math.MaxFloat64
	for i := 0; i < 11; i++ {
		if g.on[s][i] == "" || g.leaving[s][i] || (outfield && i == g.gk[s]) {
			continue
		}
		if d := dist(g.pos[s][i], p); d < bd {
			best, bd = i, d
		}
	}
	if best < 0 && outfield {
		return g.nearest(s, p, false)
	}
	return best
}

func (g *gen) find(s int, id string) int {
	if id == "" {
		return -1
	}
	for i := 0; i < 11; i++ {
		if g.on[s][i] == id {
			return i
		}
	}
	return -1
}

func (g *gen) side(clubID string) int {
	if clubID == g.clubs[1] {
		return 1
	}
	return 0
}

// centre resets both teams to their base shape for a kick-off by side s.
func (g *gen) centre(s int) {
	g.poss, g.ball = s, vec{0.5, 0.5}
	for t := 0; t < 2; t++ {
		for i := 0; i < 11; i++ {
			b := g.base[t][i]
			b.x = math.Min(b.x, 0.47) // everyone in their own half
			g.pos[t][i] = abs(t, b)
		}
	}
	if h := g.nearest(s, g.ball, true); h >= 0 {
		g.holder = h
		g.pos[s][h] = g.ball
	}
}

// give hands the ball to slot h of side s and reshapes around it.
func (g *gen) give(s, h int) {
	g.poss = s
	if h >= 0 {
		g.holder = h
	}
	g.shape()
	if g.on[s][g.holder] != "" {
		g.ball = g.pos[s][g.holder]
	}
}

func (g *gen) extra(m, t int, typ string, s, slot int, text string) {
	g.seq++
	pid := ""
	if slot >= 0 {
		pid = g.on[s][slot]
	}
	seq := ExtraSequenceBase + g.seq
	g.cur.Extras = append(g.cur.Extras, Extra{Sequence: seq, Minute: m, Offset: t, Type: typ, ClubID: g.clubs[s], PlayerID: pid, Text: text})
	g.cur.Cues = append(g.cur.Cues, Cue{Sequence: seq, T: t})
}

// play is one frame of open play. forced >= 0 keeps the ball with that side
// (the build-up to a scripted event); want names the player who must receive.
func (g *gen) play(m, t, forced int, want string) {
	if forced < 0 {
		lose := 0.40 * g.share // chance the side in possession loses the ball this frame
		if g.poss == 0 {
			lose = 0.40 * (1 - g.share)
		}
		if g.r.float() < lose {
			o := 1 - g.poss
			n := g.nearest(o, g.ball, true)
			switch x := g.r.float(); {
			case x < 0.10: // foul: the side in possession keeps the ball
				g.extra(m, t, ExtraFoul, o, n, "Foul by {player}.")
				g.shape()
			case x < 0.25:
				g.give(o, n)
				g.extra(m, t, ExtraTackle, o, n, "{player} wins the ball with a tackle.")
			default:
				g.give(o, n)
			}
			return
		}
	} else if g.poss != forced {
		g.give(forced, g.nearest(forced, g.ball, true))
		return
	}

	s := g.poss
	to := g.find(s, want)
	if to < 0 {
		to = g.receiver()
	}
	if to >= 0 && to != g.holder {
		g.cur.Passes[s]++
	}
	g.give(s, to)
	if forced >= 0 || abs(s, g.ball).x < 0.70 || g.r.float() > 0.11 {
		return
	}

	// The attack reaches the final third and ends without a goal.
	o := 1 - s
	switch x := g.r.float(); {
	case x < 0.45:
		g.extra(m, t, ExtraShot, s, g.holder, "{player} shoots wide.")
		g.ball = abs(s, vec{1, 0.5 + g.wide(0.12, 0.25)})
		g.restart = &restart{side: o}
	case x < 0.65:
		g.extra(m, t, ExtraSave, o, g.gk[o], "{player} makes the save.")
		g.ball = g.pos[o][g.gk[o]]
		g.restart = &restart{side: o}
	case x < 0.90:
		g.extra(m, t, ExtraCorner, s, g.holder, "Corner won by {player}.")
		y := 0.0
		if g.r.float() < 0.5 {
			y = 1
		}
		g.ball = abs(s, vec{1, y})
	default:
		g.extra(m, t, ExtraOffside, s, g.holder, "{player} is caught offside.")
		g.restart = &restart{side: o}
	}
}

// wide returns a signed offset with magnitude in [lo, hi).
func (g *gen) wide(lo, hi float64) float64 {
	d := lo + g.r.float()*(hi-lo)
	if g.r.float() < 0.5 {
		return -d
	}
	return d
}

// receiver picks who the holder passes to: nearby team-mates, preferably ahead.
func (g *gen) receiver() int {
	s := g.poss
	from := abs(s, g.pos[s][g.holder])
	var total float64
	var w [11]float64
	for i := 0; i < 11; i++ {
		if i == g.holder || g.on[s][i] == "" || g.leaving[s][i] {
			continue
		}
		p := abs(s, g.pos[s][i])
		w[i] = 0.6
		if dx := p.x - from.x; dx > -0.05 && dx < 0.35 {
			w[i] = 3
		}
		if i == g.gk[s] {
			w[i] = 0.15
		}
		w[i] /= 0.15 + dist(p, from)
		total += w[i]
	}
	if total == 0 {
		return g.holder
	}
	x := g.r.float() * total
	for i := 0; i < 11; i++ {
		if x -= w[i]; w[i] > 0 && x <= 0 {
			return i
		}
	}
	return g.holder
}

// minute generates one minute: scripted frames for the matchsim events, open
// play for the rest.
func (g *gen) minute(m int, evs []Event) Minute {
	out := Minute{Minute: m, Frames: make([]Frame, 0, FramesPerMinute), Cues: []Cue{}}
	g.cur = &out
	for s := 0; s < 2; s++ {
		for i := 0; i < 11; i++ {
			out.Lineup[s*11+i] = g.on[s][i]
		}
	}

	// Give every event a frame: kick-off first, half/full time last, the rest
	// spread across the minute in matchsim's order. An assist shares its goal's
	// frame.
	var script [FramesPerMinute][]Event
	n := 0
	for _, e := range evs {
		if e.Type != evKickoff && e.Type != evHalfTime && e.Type != evFullTime && e.Type != evAssist {
			n++
		}
	}
	i, last := 0, 1
	for _, e := range evs {
		switch e.Type {
		case evKickoff:
			script[0] = append(script[0], e)
		case evHalfTime, evFullTime:
			script[FramesPerMinute-1] = append(script[FramesPerMinute-1], e)
		case evAssist:
			script[last] = append(script[last], e)
		default:
			last = 1 + ((2*i+1)*(FramesPerMinute-2))/(2*n)
			if last > FramesPerMinute-2 {
				last = FramesPerMinute - 2
			}
			script[last] = append(script[last], e)
			i++
		}
	}

	for f := 0; f < FramesPerMinute; f++ {
		t := f * FrameMillis
		switch {
		case len(script[f]) > 0:
			g.restart = nil
			for k, e := range script[f] {
				out.Cues = append(out.Cues, Cue{Sequence: e.Sequence, T: t + k})
				g.script(m, t, e, script[f], evs)
			}
		case g.restart != nil:
			rs := g.restart
			g.restart = nil
			if rs.centre {
				g.centre(rs.side)
			} else {
				g.give(rs.side, g.gk[rs.side])
			}
		default:
			forced, want := -1, ""
			for d := 1; d <= 2 && f+d < FramesPerMinute && forced < 0; d++ {
				if nx := script[f+d]; len(nx) > 0 {
					switch nx[0].Type {
					case evGoal, evChance, evPenAwarded:
						forced = g.side(nx[0].ClubID)
						if d == 1 {
							for _, a := range nx {
								if a.Type == evAssist {
									want = a.PlayerID
								}
							}
						}
					}
					break
				}
			}
			g.play(m, t, forced, want)
		}
		out.Frames = append(out.Frames, g.frame(m, t))
	}
	return out
}

// script stages one matchsim event on its frame.
func (g *gen) script(m, t int, e Event, frame, minute []Event) {
	s := g.side(e.ClubID)
	o := 1 - s
	slot := g.find(s, e.PlayerID)
	switch e.Type {
	case evKickoff:
		g.centre(s)
	case evGoal, evChance:
		if slot < 0 {
			slot = g.nearest(s, abs(s, vec{0.9, 0.5}), true)
		}
		g.give(s, slot)
		if slot >= 0 {
			g.pos[s][slot] = abs(s, vec{0.92, 0.5 + g.wide(0, 0.12)})
		}
		if e.Type == evGoal {
			g.ball = abs(s, vec{1, 0.5 + g.wide(0, 0.05)})
			g.restart = &restart{side: o, centre: true}
		} else {
			g.ball = g.pos[o][g.gk[o]]
			g.restart = &restart{side: o}
		}
	case evPenAwarded:
		for _, x := range minute { // the taker is named on the kick that follows
			if x.Sequence > e.Sequence && (x.Type == evPenScored || x.Type == evPenMissed) {
				slot = g.find(s, x.PlayerID)
				break
			}
		}
		g.penalty(s, slot)
	case evPenScored, evPenMissed:
		g.penalty(s, slot)
		if e.Type == evPenScored {
			g.ball = abs(s, vec{1, 0.5 + g.wide(0.02, 0.06)})
			g.restart = &restart{side: o, centre: true}
		} else {
			g.ball = abs(s, vec{1, 0.5 + g.wide(0.10, 0.20)})
			g.restart = &restart{side: o}
		}
	case evYellow, evRed:
		// The card follows a foul by the booked player; the other side restarts.
		ft := t - 500
		if ft < 0 {
			ft = 0
		}
		g.extra(m, ft, ExtraFoul, s, slot, "Foul by {player}.")
		g.give(o, g.nearest(o, g.ball, true))
		if slot >= 0 {
			g.pos[s][slot] = vec{clamp(g.ball.x+0.02, 0, 1), clamp(g.ball.y+0.02, 0, 1)}
			if e.Type == evRed {
				g.leaving[s][slot] = true
				g.pending = append(g.pending, func() { g.on[s][slot], g.leaving[s][slot] = "", false })
			}
		}
	case evSub:
		// PlayerID comes on for RelatedPlayerID, from the next minute.
		if out := g.find(s, e.RelatedPlayerID); out >= 0 && e.PlayerID != e.RelatedPlayerID {
			in := e.PlayerID
			g.pending = append(g.pending, func() {
				if g.on[s][out] != "" { // not sent off in the meantime
					g.on[s][out] = in
				}
			})
		}
		g.shape()
	case evHalfTime:
		g.shape()
		g.restart = &restart{side: 1, centre: true}
	default: // injury, full time, assist: play stops where it is
		g.shape()
	}
}

// penalty puts the ball on the spot with the taker behind it and the keeper on
// the line.
func (g *gen) penalty(s, taker int) {
	o := 1 - s
	if taker < 0 {
		taker = g.nearest(s, abs(s, vec{0.88, 0.5}), true)
	}
	g.give(s, taker)
	g.ball = abs(s, vec{0.885, 0.5})
	if taker >= 0 {
		g.pos[s][taker] = abs(s, vec{0.86, 0.5})
	}
	g.pos[o][g.gk[o]] = abs(o, vec{0.01, 0.5})
}

// frame snapshots the current state. Teams change ends at half time, so second
// half (and extra time) frames are mirrored.
func (g *gen) frame(m, t int) Frame {
	q := func(p vec) [2]int {
		if m > 45 {
			p = vec{1 - p.x, 1 - p.y}
		}
		return [2]int{int(math.Round(clamp(p.x, 0, 1) * Scale)), int(math.Round(clamp(p.y, 0, 1) * Scale))}
	}
	f := Frame{T: t}
	b := q(g.ball)
	f.Ball = [3]int{b[0], b[1], 0} // ponytail: height is always 0; give long balls and shots an arc when the 3D view needs it
	for s := 0; s < 2; s++ {
		for i := 0; i < 11; i++ {
			if g.on[s][i] == "" {
				f.Players[s*11+i] = [2]int{-1, -1}
				continue
			}
			f.Players[s*11+i] = q(g.pos[s][i])
		}
	}
	return f
}
