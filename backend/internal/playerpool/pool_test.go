package playerpool

import (
	"math/rand"
	"testing"
	"time"
)

func TestSquadTemplateBalance(t *testing.T) {
	tmpl := squadTemplate(24)
	if got, want := len(tmpl), 24; got != want {
		t.Fatalf("template size = %d, want %d", got, want)
	}

	groups := map[string]int{}
	for i, allowed := range tmpl {
		switch {
		case allowed[0] == "GK":
			groups["GK"]++
		case i >= len(tmpl)-2:
			// The last two slots are flexible: every outfield position.
			if len(allowed) != 11 || contains(allowed, "GK") {
				t.Fatalf("flex slot %d = %v, want all 11 outfield positions", i, allowed)
			}
			groups["FLEX"]++
		case contains(allowed, "CB"):
			groups["DEF"]++
		case contains(allowed, "CM"):
			groups["MID"]++
		case contains(allowed, "ST"):
			groups["FWD"]++
		}
	}
	want := map[string]int{"GK": 2, "DEF": 7, "MID": 7, "FWD": 6, "FLEX": 2}
	for group, n := range want {
		if groups[group] != n {
			t.Fatalf("%s slots = %d, want %d (full=%v)", group, groups[group], n, groups)
		}
	}
}

func TestSquadTemplateVariableSize(t *testing.T) {
	// A smaller squad keeps the head of the template intact (2 GK first).
	for _, size := range []int{11, 18, 24, 31} {
		tmpl := squadTemplate(size)
		if len(tmpl) != size {
			t.Fatalf("size %d: template len %d", size, len(tmpl))
		}
		if tmpl[0][0] != "GK" || tmpl[1][0] != "GK" {
			t.Fatalf("size %d: first two slots are not the goakeeper pair", size)
		}
	}
}

func TestPoolAgeDistribution(t *testing.T) {
	// Over 1000 draws every band is hit and ages stay inside [17,33].
	rng := rand.New(rand.NewSource(7))
	seen := map[int]int{}
	const n = 2000
	for i := 0; i < n; i++ {
		a := poolAge(rng)
		if a < 17 || a > 33 {
			t.Fatalf("age %d outside [17,33]", a)
		}
		seen[a]++
	}
	total := 0
	for _, c := range seen {
		total += c
	}
	if total != n {
		t.Fatalf("only %d draws tracked", total)
	}
	// Teenage band should be a minority (15% ± tolerance).
	teens := 0
	for a := 17; a <= 20; a++ {
		teens += seen[a]
	}
	if pct := float64(teens) / n; pct < 0.10 || pct > 0.22 {
		t.Fatalf("teen share = %.2f, want the 15%% band roughly", pct)
	}
	// Prime band (21-28) is the largest slice (~50%).
	prime := 0
	for a := 21; a <= 28; a++ {
		prime += seen[a]
	}
	if pct := float64(prime) / n; pct < 0.40 || pct > 0.62 {
		t.Fatalf("prime share = %.2f, want the 50%% band roughly", pct)
	}
}

func TestPoolAgeDeterministic(t *testing.T) {
	a := rand.New(rand.NewSource(5))
	b := rand.New(rand.NewSource(5))
	for i := 0; i < 50; i++ {
		if poolAge(a) != poolAge(b) {
			t.Fatal("poolAge not deterministic under the same seed")
		}
	}
}

func TestDobForAndAgeFor(t *testing.T) {
	ref := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got, want := dobFor(ref, 20), time.Date(2006, 9, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("dobFor(age 20) = %v, want %v", got, want)
	}
	if got, want := dobFor(ref, 17), time.Date(2009, 9, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("dobFor(age 17) = %v, want %v", got, want)
	}
	if got := ageFor(ref, dobFor(ref, 27)); got != 27 {
		t.Fatalf("ageFor round-trips to %d, want 27", got)
	}
}
