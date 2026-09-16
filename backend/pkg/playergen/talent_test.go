package playergen

import (
	"testing"
)

func TestRollTalentDeterministic(t *testing.T) {
	f := optionFactory(9)
	var classes []TalentClass
	for i := 0; i < 200; i++ {
		classes = append(classes, rollTalent(f.Rng(), WorldTalentOdds))
	}
	f2 := optionFactory(9)
	for i, want := range classes {
		if got := rollTalent(f2.Rng(), WorldTalentOdds); got != want {
			t.Fatalf("roll %d diverged: got %d want %d (determinism broken)", i, got, want)
		}
	}
}

func TestRollTalentProfileProducesEveryClass(t *testing.T) {
	// With a heavy profile every class must be observable across a fixed
	// stream (this asserts the cumulative bands resolve, not exact rates).
	f := optionFactory(1234)
	odds := TalentOdds{Journeyman: 1, TopProspect: 1, Wonderkid: 1, Generational: 1}
	seen := map[TalentClass]bool{}
	for i := 0; i < 6000; i++ {
		seen[rollTalent(f.Rng(), odds)] = true
	}
	for wanted := TalentJourneyman; wanted <= TalentGenerational; wanted++ {
		if !seen[wanted] {
			t.Fatalf("class %d never rolled with even weights", wanted)
		}
	}
}

func TestZeroProfileFallsBackToWorldOdds(t *testing.T) {
	a, err := optionFactory(21).CreatePlayerWithOptions(CreatePlayerOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, err := optionFactory(21).CreatePlayerWithOptions(CreatePlayerOptions{TalentOdds: TalentOdds{}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.Talent != b.Talent {
		t.Fatalf("zero TalentOdds diverged from world default: %d vs %d", a.Talent, b.Talent)
	}
}

func TestTalentRaisesPotentialCeiling(t *testing.T) {
	// Forced-generational students (potential +32 floor) must hit the 100
	// cap far more often than forced journeymen (+0), even though the
	// interleaved rng stream shifts after the draw.
	capped := func(odds TalentOdds) int {
		f := optionFactory(77)
		n := 0
		const draws = 260
		for i := 0; i < draws; i++ {
			p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{MinAge: 15, MaxAge: 17, TalentOdds: odds})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if p.HiddenTraits.Potential == 100 {
				n++
			}
		}
		return n
	}
	generational := capped(TalentOdds{Journeyman: 0, TopProspect: 0, Wonderkid: 0, Generational: 1})
	journeyman := capped(TalentOdds{Journeyman: 1})
	if generational <= journeyman {
		t.Fatalf("generational capped draws %d must exceed journeyman %d", generational, journeyman)
	}
	if journeyman == generational && generational == 0 {
		t.Fatal("potential never hit the 100 cap — bonus not reaching generation")
	}
}

func TestTalentBonusOrdering(t *testing.T) {
	if TalentJourneyman.potentialBonus() != 0 {
		t.Fatal("journeyman bonus must be 0")
	}
	if !(TalentTopProspect.potentialBonus() > TalentJourneyman.potentialBonus()) {
		t.Fatal("top prospect bonus must exceed journeyman")
	}
	if !(TalentWonderkid.potentialBonus() > TalentTopProspect.potentialBonus()) {
		t.Fatal("wonderkid bonus must exceed top prospect")
	}
	if !(TalentGenerational.potentialBonus() > TalentWonderkid.potentialBonus()) {
		t.Fatal("generational bonus must exceed wonderkid")
	}
}

func TestStringLabels(t *testing.T) {
	if TalentJourneyman.String() != "journeyman" ||
		TalentWonderkid.String() != "wonderkid" ||
		TalentGenerational.String() != "generational" ||
		TalentTopProspect.String() != "top_prospect" {
		t.Fatalf("unexpected labels: %q %q %q %q",
			TalentJourneyman.String(), TalentTopProspect.String(),
			TalentWonderkid.String(), TalentGenerational.String())
	}
}
