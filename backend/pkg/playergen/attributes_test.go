package playergen

import (
	"math/rand"
	"testing"
)

func newTestFactory(seed int64) *PlayerFactory {
	return NewPlayerFactory(&PoolGenerator{}, NewNationalityPool(), rand.New(rand.NewSource(seed)))
}

func TestGeneratedAttributesDeterministic(t *testing.T) {
	a := generateAttributes(newTestFactory(7).rng, "ST", 0)
	b := generateAttributes(newTestFactory(7).rng, "ST", 0)
	if len(a) != len(b) {
		t.Fatalf("attribute count differs: %d vs %d", len(a), len(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("determinism broken on %s: %d vs %d", k, b[k], v)
		}
	}
}

func TestGeneratedAttributesInRangeAndComplete(t *testing.T) {
	for _, pos := range ValidPositions {
		attrs := generateAttributes(newTestFactory(11).rng, pos, 0)
		want := 0
		for _, cat := range categoriesForPosition(pos) {
			want += len(attributeKeys[cat])
		}
		if len(attrs) != want {
			t.Fatalf("%s: attribute count %d, want %d", pos, len(attrs), want)
		}
		for k, v := range attrs {
			if v < 1 || v > 100 {
				t.Fatalf("%s: %s value %d outside [1,100]", pos, k, v)
			}
			if cat := CategoryForKey(k); cat == "" {
				t.Fatalf("%s: key %q missing from catalogue", pos, k)
			}
		}
	}
}

func TestGeneratedAttributesPositionFilter(t *testing.T) {
	for _, pos := range ValidPositions {
		attrs := generateAttributes(newTestFactory(3).rng, pos, 0)
		if pos == "GK" {
			for _, k := range attributeKeys["technical"] {
				if _, ok := attrs[k]; ok {
					t.Fatalf("GK must not carry technical key %q", k)
				}
			}
			if len(attributeKeys["goalkeeping"])-1 > 0 {
				var gkSeen int
				for _, k := range attributeKeys["goalkeeping"] {
					if _, ok := attrs[k]; ok {
						gkSeen++
					}
				}
				if gkSeen != len(attributeKeys["goalkeeping"]) {
					t.Fatalf("GK must carry every goalkeeping key, saw %d/%d", gkSeen, len(attributeKeys["goalkeeping"]))
				}
			}
			continue
		}
		for _, k := range attributeKeys["goalkeeping"] {
			if _, ok := attrs[k]; ok {
				t.Fatalf("%s must not carry goalkeeping key %q", pos, k)
			}
		}
	}
}

func TestGeneratedAttributesStrikerFinishingRanks(t *testing.T) {
	// Across many draws, a ST's finishing (technical mean 66 + 10 adjust) must
	// sit comfortably above his crossing (mean 66 + 6 adjust) — the position
	// bias is what separates generated squads from noise.
	f := newTestFactory(1234)
	sumFinish, sumCross := 0, 0
	const n = 200
	for i := 0; i < n; i++ {
		sumFinish += generateAttributes(f.rng, "ST", 0)["finishing"]
		sumCross += generateAttributes(f.rng, "ST", 0)["crossing"]
	}
	if float64(sumFinish)/n <= float64(sumCross)/n {
		t.Fatalf("ST finishing avg %v must exceed crossing avg %v", float64(sumFinish)/n, float64(sumCross)/n)
	}
}

func TestTraitsAndPersonalityMirrored(t *testing.T) {
	// The four shared dimensions must agree between the two rows.
	seen := map[string]int{}
	for i := 0; i < 20; i++ {
		ht, p := generateTraitsAndPersonality(newTestFactory(5).rng, 20, 0)
		for k, a := range map[string]struct{ h, p int }{
			"professionalism": {ht.Professionalism, p.Professionalism},
			"ambition":        {ht.Ambition, p.Ambition},
			"loyalty":         {ht.Loyalty, p.Loyalty},
			"adaptability":    {ht.Adaptability, p.Adaptability},
		} {
			if a.h != a.p {
				t.Fatalf("%s diverges between rows: hidden %d vs personality %d", k, a.h, a.p)
			}
			if a.h < 1 || a.h > 100 {
				t.Fatalf("%s out of range: %d", k, a.h)
			}
			seen[k]++
		}
		if ht.Potential < 1 || ht.Potential > 100 || p.Leadership < 1 || p.Leadership > 100 {
			t.Fatalf("potential/leadership out of range: %d/%d", ht.Potential, p.Leadership)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("saw %d shared dims, want 4", len(seen))
	}
}

func TestPotentialIsAgeAware(t *testing.T) {
	f := newTestFactory(9)
	const n = 300
	var young, old float64
	for i := 0; i < n; i++ {
		ht, _ := generateTraitsAndPersonality(f.rng, 18, 0) // young
		young += float64(ht.Potential)
		ht2, _ := generateTraitsAndPersonality(f.rng, 31, 0) // old
		old += float64(ht2.Potential)
	}
	if young/n <= old/n {
		t.Fatalf("young potential avg %v should exceed old %v", young/n, old/n)
	}
}

func TestTemperamentSpreadsToScoutingThreshold(t *testing.T) {
	// The volatile-temperament band (<= 30) must be reachable — a whole
	// generated league would otherwise never surface a volatile player.
	f := newTestFactory(21)
	volatile := 0
	for i := 0; i < 500; i++ {
		ht, _ := generateTraitsAndPersonality(f.rng, 20, 0)
		if ht.Temperament <= 30 {
			volatile++
		}
	}
	if volatile == 0 {
		t.Fatal("temperament never reached the volatile scouting threshold in 500 draws")
	}
}

func TestCategoryAverageRollup(t *testing.T) {
	f := newTestFactory(4)
	sum := 0
	const n = 100
	for i := 0; i < n; i++ {
		sum += CategoryAverage(generateAttributes(f.rng, "ST", 0), "technical")
	}
	avg := float64(sum) / n
	// ST technical mean is 66: jitter (width 8+~8) pulls the roll-up near it.
	if avg < 55 || avg > 75 {
		t.Fatalf("ST technical roll-up avg %v outside plausible band around 66", avg)
	}

	// GK: no categories produced the mean; asking for technical must read 0.
	if got := CategoryAverage(generateAttributes(f.rng, "GK", 0), "technical"); got != 0 {
		t.Fatalf("GK technical roll-up = %d, want 0", got)
	}
	if got := CategoryAverage(generateAttributes(f.rng, "GK", 0), "goalkeeping"); got == 0 {
		t.Fatal("GK goalkeeping roll-up must be present")
	}
}

func TestNeutralEmotionalState(t *testing.T) {
	e := neutralEmotionalState(newTestFactory(2).rng)
	if e.State != "content" || e.Cause != "preseason" {
		t.Fatalf("unexpected initial state: %+v", e)
	}
	if e.Intensity < 40 || e.Intensity > 70 {
		t.Fatalf("intensity %d outside [40,70]", e.Intensity)
	}
}
