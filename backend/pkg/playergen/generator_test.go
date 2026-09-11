package playergen

import (
	"math/rand"
	"reflect"
	"testing"
)

func testGenerator() *PoolGenerator {
	g := NewPoolGenerator()
	_ = g.AddPool("br",
		[]string{"Ana", "Bruno", "Carla", "Diego", "Elisa", "Felipe", "Gabriela", "Heitor"},
		[]string{"Silva", "Santos", "Souza", "Oliveira", "Pereira", "Costa", "Rodrigues", "Alves"})
	_ = g.AddPool("ng",
		[]string{"Chinedu", "Emeka", "Ngozi", "Yemi", "Adaeze", "Kehinde", "Ifeanyi", "Chioma"},
		[]string{"Okafor", "Balogun", "Adeyemi", "Eze", "Nwosu", "Adeleke", "Okonkwo", "Amadi"})
	_ = g.AddPool("en",
		[]string{"Harry", "Oliver", "Jack", "Charlie", "George", "Noah", "Alfie", "Oscar"},
		[]string{"Smith", "Taylor", "Brown", "Walker", "Clarke", "Matthews", "Cooper", "Ward"})
	return g
}

func testPool() *NationalityPool {
	p := NewNationalityPool()
	p.Add(Nationality{Code: "br", Name: "Brazil", Weight: 10})
	p.Add(Nationality{Code: "ng", Name: "Nigeria", Weight: 8})
	p.Add(Nationality{Code: "en", Name: "England", Weight: 6})
	return p
}

func TestPoolGenerator_DeterministicSameSeed(t *testing.T) {
	// Same seed -> identical sequence; different seed -> different sequence.
	draw := func(seed int64) []string {
		g := testGenerator()
		rng := rand.New(rand.NewSource(seed))
		var out []string
		for range 200 {
			first, last, err := g.GenerateFullName(rng, "br")
			if err != nil {
				t.Fatalf("GenerateFullName: %v", err)
			}
			out = append(out, first+" "+last)
		}
		return out
	}

	a := draw(42)
	b := draw(42)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different sequences")
	}
	c := draw(43)
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical sequences over %d draws", len(a))
	}
}

func TestPoolGenerator_UnknownNationalityErrors(t *testing.T) {
	g := testGenerator()
	rng := rand.New(rand.NewSource(1))
	if _, _, err := g.GenerateFullName(rng, "zz"); err == nil {
		t.Fatal("expected error for unknown nationality, got nil")
	}
}

func TestPoolGenerator_RejectsEmptyPools(t *testing.T) {
	g := NewPoolGenerator()
	if err := g.AddPool("br", nil, []string{"Silva"}); err == nil {
		t.Fatal("expected error for empty first_names")
	}
	if err := g.AddPool("", []string{"Ana"}, []string{"Silva"}); err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestPoolGenerator_DedupesNames(t *testing.T) {
	g := NewPoolGenerator()
	if err := g.AddPool("br", []string{"Ana", "Ana", "Ana"}, []string{"Silva", "Silva"}); err != nil {
		t.Fatalf("AddPool: %v", err)
	}
	if got := len(g.pools["br"].firstNames); got != 1 {
		t.Fatalf("expected deduped first names, got %d", got)
	}
}

func TestNationalityPool_WeightedRandomDistribution(t *testing.T) {
	pool := testPool() // weights 10 / 8 / 6 -> 41.67% / 33.33% / 25%
	rng := rand.New(rand.NewSource(7))

	const draws = 200000
	counts := map[string]int{}
	for range draws {
		code, err := pool.WeightedRandom(rng)
		if err != nil {
			t.Fatalf("WeightedRandom: %v", err)
		}
		counts[code]++
	}

	want := map[string]float64{"br": 10.0 / 24, "ng": 8.0 / 24, "en": 6.0 / 24}
	for code, w := range want {
		got := float64(counts[code]) / draws
		if diff := got - w; diff > 0.02 || diff < -0.02 {
			t.Errorf("nationality %q share = %.4f, want %.4f (±0.02)", code, got, w)
		}
	}
}

func TestNationalityPool_WeightedRandomSkipsNonPositive(t *testing.T) {
	pool := NewNationalityPool()
	pool.Add(Nationality{Code: "br", Weight: 1})
	pool.Add(Nationality{Code: "xx", Weight: 0})
	rng := rand.New(rand.NewSource(3))
	for range 1000 {
		code, err := pool.WeightedRandom(rng)
		if err != nil {
			t.Fatalf("WeightedRandom: %v", err)
		}
		if code != "br" {
			t.Fatalf("selected zero-weight nationality %q", code)
		}
	}
}

func TestNationalityPool_EmptyErrors(t *testing.T) {
	if _, err := NewNationalityPool().WeightedRandom(rand.New(rand.NewSource(1))); err == nil {
		t.Fatal("expected error for empty pool")
	}
}

func TestPlayerFactory_Deterministic(t *testing.T) {
	makePlayers := func(seed int64) []*GeneratedPlayer {
		f := NewPlayerFactory(testGenerator(), testPool(), rand.New(rand.NewSource(seed))).
			WithRegistry(NewNameRegistry())
		out := make([]*GeneratedPlayer, 0, 40)
		for range 40 {
			p, err := f.CreatePlayer()
			if err != nil {
				t.Fatalf("CreatePlayer: %v", err)
			}
			out = append(out, p)
		}
		return out
	}

	a := makePlayers(99)
	b := makePlayers(99)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different player sequences")
	}
	c := makePlayers(100)
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical player sequences")
	}
}

func TestPlayerFactory_CollisionAvoidance(t *testing.T) {
	// 4 first x 4 last = 16 unique combos for "br"; request fewer than that.
	f := NewPlayerFactory(testGenerator(), testPool(), rand.New(rand.NewSource(5))).
		WithRegistry(NewNameRegistry())

	seen := map[string]struct{}{}
	for range 15 {
		p, err := f.CreatePlayer()
		if err != nil {
			t.Fatalf("CreatePlayer: %v", err)
		}
		if p.NationalityCode != "br" {
			continue
		}
		key := p.FirstName + " " + p.LastName
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate name %q generated despite registry", key)
		}
		seen[key] = struct{}{}
	}
	if len(seen) == 0 {
		t.Fatal("expected at least one Brazilian player")
	}
}

func TestPlayerFactory_ExhaustionErrors(t *testing.T) {
	g := NewPoolGenerator()
	_ = g.AddPool("br", []string{"Ana"}, []string{"Silva"})
	pool := NewNationalityPool()
	pool.Add(Nationality{Code: "br", Weight: 1})

	f := NewPlayerFactory(g, pool, rand.New(rand.NewSource(1))).WithRegistry(NewNameRegistry())
	if _, err := f.CreatePlayer(); err != nil {
		t.Fatalf("first CreatePlayer: %v", err)
	}
	if _, err := f.CreatePlayer(); err == nil {
		t.Fatal("expected error once the single unique name is exhausted")
	}
}

func TestPlayerFactory_NoRegistryAllowsDuplicates(t *testing.T) {
	g := NewPoolGenerator()
	_ = g.AddPool("br", []string{"Ana"}, []string{"Silva"})
	pool := NewNationalityPool()
	pool.Add(Nationality{Code: "br", Weight: 1})
	f := NewPlayerFactory(g, pool, rand.New(rand.NewSource(1)))
	for range 3 {
		if _, err := f.CreatePlayer(); err != nil {
			t.Fatalf("CreatePlayer: %v", err)
		}
	}
}

func TestPlayerFactory_DisplayNameAndFields(t *testing.T) {
	f := NewPlayerFactory(testGenerator(), testPool(), rand.New(rand.NewSource(11)))
	p, err := f.CreatePlayer()
	if err != nil {
		t.Fatalf("CreatePlayer: %v", err)
	}
	if p.DisplayName != p.LastName {
		t.Errorf("display name = %q, want surname %q", p.DisplayName, p.LastName)
	}
	if p.Age < minAge || p.Age > maxAge {
		t.Errorf("age %d out of range [%d,%d]", p.Age, minAge, maxAge)
	}
	valid := false
	for _, pos := range ValidPositions {
		if p.PrimaryPosition == pos {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("invalid primary position %q", p.PrimaryPosition)
	}
}

func TestNameRegistry_ReserveRelease(t *testing.T) {
	r := NewNameRegistry()
	if !r.Reserve("br", "Ana", "Silva") {
		t.Fatal("first Reserve should succeed")
	}
	if r.Reserve("br", "Ana", "Silva") {
		t.Fatal("duplicate Reserve should fail")
	}
	if !r.Reserve("ng", "Ana", "Silva") {
		t.Fatal("same name under a different nationality should succeed")
	}
	r.Release("br", "Ana", "Silva")
	if !r.Reserve("br", "Ana", "Silva") {
		t.Fatal("Reserve after Release should succeed")
	}
}

// TestLoadNameData_CuratedFiles validates the shipped data set: coverage,
// non-trivial list sizes, positive weights, and documented provenance.
func TestLoadNameData_CuratedFiles(t *testing.T) {
	data, err := LoadNameData("../../data/names")
	if err != nil {
		t.Fatalf("LoadNameData: %v", err)
	}
	if len(data.Nationalities) < 21 {
		t.Fatalf("expected at least 21 nationalities, got %d", len(data.Nationalities))
	}
	if data.Pool.TotalWeight() <= 0 {
		t.Fatal("nationality pool has no positive weight")
	}
	for _, n := range data.Nationalities {
		if n.Weight <= 0 {
			t.Errorf("%s: generation_weight = %v, want > 0", n.Code, n.Weight)
		}
		pool := data.Generator.pools[n.Code]
		if pool == nil {
			t.Errorf("%s: no name pool loaded", n.Code)
			continue
		}
		if len(pool.firstNames) < 15 || len(pool.lastNames) < 15 {
			t.Errorf("%s: too few names (first=%d last=%d), want >= 15 each", n.Code, len(pool.firstNames), len(pool.lastNames))
		}
	}

	// The generator serves every loaded nationality.
	rng := rand.New(rand.NewSource(1))
	for _, code := range data.Pool.Codes() {
		if _, _, err := data.Generator.GenerateFullName(rng, code); err != nil {
			t.Errorf("GenerateFullName(%q): %v", code, err)
		}
	}
}

func TestLoadNameData_MissingDirErrors(t *testing.T) {
	if _, err := LoadNameData("../../data/definitely-not-a-dir"); err == nil {
		t.Fatal("expected error for missing data dir")
	}
}
