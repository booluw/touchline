package pitchsim

import (
	"fmt"
	"reflect"
	"testing"
)

func side(club string) Side {
	pos := []string{"GK", "LB", "CB", "CB", "RB", "CM", "CM", "CM", "RW", "ST", "LW"}
	s := Side{ClubID: club}
	for i, p := range pos {
		s.XI = append(s.XI, Player{ID: fmt.Sprintf("%s-%d", club, i), Position: p})
	}
	return s
}

func input() Input {
	return Input{
		Seed: 7, Home: side("H"), Away: side("A"), HomeShare: 0.55, Minutes: 93,
		Events: []Event{
			{1, 1, evKickoff, "H", "", ""},
			{2, 12, evGoal, "H", "H-9", ""},
			{3, 12, evAssist, "H", "H-5", "H-9"},
			{4, 30, evYellow, "A", "A-3", ""},
			{5, 40, evRed, "A", "A-2", ""},
			{6, 45, evHalfTime, "", "", ""},
			{7, 60, evSub, "H", "H-sub", "H-9"},
			{8, 70, evPenAwarded, "A", "", ""},
			{9, 70, evPenScored, "A", "A-9", ""},
			{10, 80, evChance, "A", "A-10", ""},
			{11, 90, evFullTime, "", "", ""},
			{12, 93, evGoal, "A", "A-9", ""},
		},
	}
}

func TestGenerate(t *testing.T) {
	in := input()
	tr := Generate(in)
	if !reflect.DeepEqual(tr, Generate(in)) {
		t.Fatal("same input must give the same track")
	}
	if len(tr.Minutes) != in.Minutes {
		t.Fatalf("minutes = %d, want %d", len(tr.Minutes), in.Minutes)
	}

	// The live contract: a minute never changes once generated, whatever
	// happens later.
	for _, m := range []int{1, 12, 44, 69} {
		part := in
		part.Minutes = m
		part.Events = nil
		for _, e := range in.Events {
			if e.Minute <= m {
				part.Events = append(part.Events, e)
			}
		}
		if got := Generate(part); !reflect.DeepEqual(got.Minutes, tr.Minutes[:m]) {
			t.Fatalf("minutes 1..%d differ between a partial and a full generation", m)
		}
	}

	cues := map[int]int{}
	extraTypes := map[string]bool{}
	for _, x := range ExtraTypes {
		extraTypes[x] = true
	}
	extras, lastSeq := 0, ExtraSequenceBase
	for _, mn := range tr.Minutes {
		if len(mn.Frames) != FramesPerMinute {
			t.Fatalf("minute %d has %d frames", mn.Minute, len(mn.Frames))
		}
		for _, c := range mn.Cues {
			cues[c.Sequence]++
			if c.T < 0 || c.T >= 60000 {
				t.Errorf("minute %d cue %d at %dms", mn.Minute, c.Sequence, c.T)
			}
		}
		for _, x := range mn.Extras {
			extras++
			if !extraTypes[x.Type] || x.Minute != mn.Minute || x.Sequence <= lastSeq {
				t.Errorf("bad extra %+v", x)
			}
			lastSeq = x.Sequence
		}
		for _, f := range mn.Frames {
			for i, p := range f.Players {
				off := p == [2]int{-1, -1}
				if off != (mn.Lineup[i] == "") {
					t.Fatalf("minute %d slot %d: position %v does not match lineup %q", mn.Minute, i, p, mn.Lineup[i])
				}
				if !off && (p[0] < 0 || p[0] > Scale || p[1] < 0 || p[1] > Scale) {
					t.Fatalf("minute %d slot %d off the pitch: %v", mn.Minute, i, p)
				}
			}
			if f.Ball[0] < 0 || f.Ball[0] > Scale || f.Ball[1] < 0 || f.Ball[1] > Scale {
				t.Fatalf("minute %d ball off the pitch: %v", mn.Minute, f.Ball)
			}
		}
	}
	// Every matchsim event is staged exactly once; nothing is added or dropped.
	for _, e := range in.Events {
		if cues[e.Sequence] != 1 {
			t.Errorf("event %d (%s) has %d cues, want 1", e.Sequence, e.Type, cues[e.Sequence])
		}
	}
	byType, passes := map[string]int{}, [2]int{}
	for _, mn := range tr.Minutes {
		for _, x := range mn.Extras {
			byType[x.Type]++
		}
		passes[0] += mn.Passes[0]
		passes[1] += mn.Passes[1]
	}
	t.Logf("extras %v, passes home/away %v", byType, passes)
	if extras < 40 || extras > 250 {
		t.Errorf("extras = %d, want a few dozen to a couple of hundred", extras)
	}

	// The ball is in the right goal when a goal is scored.
	ballAt := func(minute, seq int) [3]int {
		mn := tr.Minutes[minute-1]
		for _, c := range mn.Cues {
			if c.Sequence == seq {
				return mn.Frames[c.T/FrameMillis].Ball
			}
		}
		t.Fatalf("no cue for %d", seq)
		return [3]int{}
	}
	if b := ballAt(12, 2); b[0] != Scale { // home, first half: attacks x = Scale
		t.Errorf("home goal: ball at %v", b)
	}
	if b := ballAt(70, 9); b[0] != Scale { // away, second half: ends swapped
		t.Errorf("away penalty: ball at %v", b)
	}
	if b := ballAt(93, 12); b[0] != Scale {
		t.Errorf("away extra-time goal: ball at %v", b)
	}

	// Red card: gone from the next minute. Substitution: replaced from the next.
	if tr.Minutes[39].Lineup[11+2] != "A-2" || tr.Minutes[40].Lineup[11+2] != "" {
		t.Error("sent-off player must leave after the minute of the card")
	}
	if tr.Minutes[59].Lineup[9] != "H-9" || tr.Minutes[60].Lineup[9] != "H-sub" {
		t.Error("substitute must take the slot from the next minute")
	}
	if p := tr.Minutes[0].Passes; p[0]+p[1] == 0 {
		t.Error("no passes in the first minute")
	}
}

// TestPossessionShare: the side given the larger share of the ball completes
// the larger share of the passes (measured over several seeds).
func TestPossessionShare(t *testing.T) {
	var p [2]int
	for seed := int64(1); seed <= 10; seed++ {
		in := input()
		in.Seed, in.HomeShare = seed, 0.65
		for _, m := range Generate(in).Minutes {
			p[0] += m.Passes[0]
			p[1] += m.Passes[1]
		}
	}
	if share := float64(p[0]) / float64(p[0]+p[1]); share < 0.58 || share > 0.72 {
		t.Errorf("home pass share = %.3f, want about 0.65", share)
	}
}
