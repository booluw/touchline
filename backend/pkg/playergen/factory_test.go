package playergen

import (
	"math/rand"
	"reflect"
	"testing"
)

// optionFactory builds a fully-populated factory (real name pools + weighted
// nationalities) so CreatePlayerWithOptions has something to draw from.
func optionFactory(seed int64) *PlayerFactory {
	return NewPlayerFactory(testGenerator(), testPool(), rand.New(rand.NewSource(seed)))
}

func TestCreatePlayerWithOptionsMatchesCreatePlayer(t *testing.T) {
	a, aErr := optionFactory(42).CreatePlayer()
	b, bErr := optionFactory(42).CreatePlayerWithOptions(CreatePlayerOptions{})
	if aErr != nil || bErr != nil {
		t.Fatalf("unexpected error: a=%v b=%v", aErr, bErr)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("CreatePlayerWithOptions(zero) diverges from CreatePlayer:\n%+v\n%+v", a, b)
	}
}

func TestCreatePlayerWithOptionsAgeRange(t *testing.T) {
	f := optionFactory(7)
	for i := 0; i < 120; i++ {
		p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{MinAge: 13, MaxAge: 15})
		if err != nil {
			t.Fatalf("CreatePlayerWithOptions: %v", err)
		}
		if p.Age < 13 || p.Age > 15 {
			t.Fatalf("age %d outside requested [13,15]", p.Age)
		}
	}
}

func TestCreatePlayerWithOptionsAgeClamps(t *testing.T) {
	// MaxAge below MinAge (or equal to zero) falls back to the default span.
	f := optionFactory(11)
	for i := 0; i < 40; i++ {
		p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{MinAge: 13, MaxAge: 12})
		if err != nil {
			t.Fatalf("CreatePlayerWithOptions: %v", err)
		}
		if p.Age < 17 || p.Age > 33 {
			t.Fatalf("age %d outside default [17,33]", p.Age)
		}
	}
}

func TestCreatePlayerWithOptionsQualityOffset(t *testing.T) {
	// A positive offset must shift the aggregate quality up relative to the
	// baseline over many draws; a negative one must shift it down.
	avgFor := func(offset int) float64 {
		f := optionFactory(99)
		sum := 0
		const n = 250
		for i := 0; i < n; i++ {
			p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{QualityOffset: offset})
			if err != nil {
				t.Fatalf("offset %d: %v", offset, err)
			}
			var total, count int
			for _, v := range p.Attributes {
				total += v
				count++
			}
			sum += total / count
		}
		return float64(sum) / n
	}

	base := avgFor(0)
	high := avgFor(15)
	low := avgFor(-15)
	if high <= base {
		t.Fatalf("positive offset avg %v must exceed baseline %v", high, base)
	}
	if low >= base {
		t.Fatalf("negative offset avg %v must trail baseline %v", low, base)
	}
}

func TestCreatePlayerWithOptionsForcedNationality(t *testing.T) {
	f := optionFactory(3)
	for i := 0; i < 60; i++ {
		p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{Nationality: "ng"})
		if err != nil {
			t.Fatalf("CreatePlayerWithOptions: %v", err)
		}
		if p.NationalityCode != "ng" {
			t.Fatalf("forced nationality = %q, want ng", p.NationalityCode)
		}
	}
}

func TestCreatePlayerWithOptionsOriginAndAcademy(t *testing.T) {
	f := optionFactory(5)
	p, err := f.CreatePlayerWithOptions(CreatePlayerOptions{
		Origin:         "club_academy",
		AcademyProduct: true,
	})
	if err != nil {
		t.Fatalf("CreatePlayerWithOptions: %v", err)
	}
	if p.Origin != "club_academy" || !p.AcademyProduct {
		t.Fatalf("origin=%q academy=%v, want club_academy/true", p.Origin, p.AcademyProduct)
	}
}

func TestCreatePlayerWithOptionsRejectsNilDeps(t *testing.T) {
	if _, err := NewPlayerFactory(nil, NewNationalityPool(), rand.New(rand.NewSource(1))).CreatePlayerWithOptions(CreatePlayerOptions{}); err == nil {
		t.Fatal("nil name generator should error")
	}
	if _, err := NewPlayerFactory(&PoolGenerator{}, nil, rand.New(rand.NewSource(1))).CreatePlayerWithOptions(CreatePlayerOptions{}); err == nil {
		t.Fatal("nil nationality pool should error")
	}
	if _, err := NewPlayerFactory(&PoolGenerator{}, NewNationalityPool(), nil).CreatePlayerWithOptions(CreatePlayerOptions{}); err == nil {
		t.Fatal("nil rng should error")
	}
}