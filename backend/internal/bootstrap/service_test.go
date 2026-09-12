package bootstrap

import (
	"math/rand"
	"testing"
	"time"

	"github.com/touchline/backend/pkg/playergen"
)

func TestSquadTemplateBalance(t *testing.T) {
	tmpl := squadTemplate(SquadSizeDefault)
	if got, want := len(tmpl), SquadSizeDefault; got != want {
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

func TestGenerateSquadBalanceAndSize(t *testing.T) {
	gen, nat := unitPools(t)
	factory := playergen.NewPlayerFactory(gen, nat, rand.New(rand.NewSource(7))).WithRegistry(playergen.NewNameRegistry())
	squad, err := generateSquad(factory, SquadSizeDefault)
	if err != nil {
		t.Fatalf("generateSquad: %v", err)
	}
	if len(squad) != SquadSizeDefault {
		t.Fatalf("squad size = %d, want %d", len(squad), SquadSizeDefault)
	}

	positionCounts := map[string]int{}
	for _, p := range squad {
		positionCounts[p.PrimaryPosition]++
	}
	if positionCounts["GK"] < 2 {
		t.Fatalf("GK count = %d, want >= 2", positionCounts["GK"])
	}
	// Every slot is drawn from its allowed set, so no GK beyond the 2 rigid
	// slots can appear, and every player must carry a valid position.
	for _, p := range squad {
		if !contains(playergen.ValidPositions, p.PrimaryPosition) {
			t.Fatalf("invalid position %q", p.PrimaryPosition)
		}
	}
}

func TestGenerateSquadDeterministic(t *testing.T) {
	gen1, nat1 := unitPools(t)
	gen2, nat2 := unitPools(t)
	s1, err := generateSquad(playergen.NewPlayerFactory(gen1, nat1, rand.New(rand.NewSource(99))).WithRegistry(playergen.NewNameRegistry()), 10)
	if err != nil {
		t.Fatalf("first squad: %v", err)
	}
	s2, err := generateSquad(playergen.NewPlayerFactory(gen2, nat2, rand.New(rand.NewSource(99))).WithRegistry(playergen.NewNameRegistry()), 10)
	if err != nil {
		t.Fatalf("second squad: %v", err)
	}
	for i := range s1 {
		a, b := s1[i], s2[i]
		if a.FirstName != b.FirstName || a.LastName != b.LastName ||
			a.PrimaryPosition != b.PrimaryPosition || a.Age != b.Age {
			t.Fatalf("player %d differs under the same seed: %+v vs %+v", i, a, b)
		}
	}
}

func TestDobFor(t *testing.T) {
	ref := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got, want := dobFor(ref, 20), time.Date(2006, 9, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("dobFor(age 20) = %v, want %v", got, want)
	}
	if got, want := dobFor(ref, 17), time.Date(2009, 9, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("dobFor(age 17) = %v, want %v", got, want)
	}
}

func TestShortName(t *testing.T) {
	if got, want := shortName("Harbour City FC"), "Har"; got != want {
		t.Fatalf("shortName = %q, want %q", got, want)
	}
	if got, want := shortName("AC"), "AC"; got != want {
		t.Fatalf("shortName short = %q, want %q", got, want)
	}
}

// unitPools builds in-memory name/nationality pools with a large name space so
// the registry can produce 24+ unique players.
func unitPools(t *testing.T) (*playergen.PoolGenerator, *playergen.NationalityPool) {
	t.Helper()
	gen := playergen.NewPoolGenerator()
	firsts := make([]string, 30)
	lasts := make([]string, 30)
	for i := range firsts {
		firsts[i] = "First" + string(rune('A'+i))
		lasts[i] = "Last" + string(rune('A'+i))
	}
	if err := gen.AddPool("eng", firsts, lasts); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	nat := playergen.NewNationalityPool()
	nat.Add(playergen.Nationality{Code: "eng", Name: "England", Weight: 1.0})
	return gen, nat
}
