package finance

import "testing"

func TestWeeklyWageMonotonicAndBounded(t *testing.T) {
	for _, pos := range []string{"GK", "CB", "LB", "RB", "DM", "CM", "AM", "LM", "RM", "LW", "RW", "ST", "UNKNOWN"} {
		var prev int64 = -1
		for mean := 0; mean <= 99; mean++ {
			w := WeeklyWage(pos, mean)
			if w < prev {
				t.Fatalf("%s: wage at mean %d (%d) dropped below mean %d (%d)", pos, mean, w, mean-1, prev)
			}
			if w < 5000 || w > 60000 {
				t.Fatalf("%s: wage at mean %d out of bounds: %d", pos, mean, w)
			}
			prev = w
		}
	}
}

func TestWeeklyWageSpotChecks(t *testing.T) {
	cases := []struct {
		pos  string
		mean int
		want int64
	}{
		{"GK", 50, 9000},
		{"ST", 70, 9520},            // 9500 + round(20^2/20)
		{"CM", 85, 8561},            // 8500 + round(35^2/20)
		{"RW", 60, 8005},            // 8000 + round(10^2/20)
		{"MIDFIELD_LEFT", 50, 7500}, // unknown position fallback base
	}
	for _, c := range cases {
		if got := WeeklyWage(c.pos, c.mean); got != c.want {
			t.Errorf("WeeklyWage(%q, %d) = %d, want %d", c.pos, c.mean, got, c.want)
		}
	}
}

func TestMeanAttribute(t *testing.T) {
	if got := MeanAttribute(nil); got != 0 {
		t.Fatalf("MeanAttribute(nil) = %d, want 0", got)
	}
	if got := MeanAttribute(map[string]int{}); got != 0 {
		t.Fatalf("MeanAttribute(empty) = %d, want 0", got)
	}
	if got := MeanAttribute(map[string]int{"pace": 20, "shooting": 40}); got != 30 {
		t.Fatalf("MeanAttribute = %d, want 30", got)
	}
}

func TestContractSeasons(t *testing.T) {
	cases := []struct {
		age  int
		want int
	}{
		{17, 4}, {22, 4}, {23, 3}, {28, 3}, {29, 2}, {33, 2},
	}
	for _, c := range cases {
		if got := ContractSeasons(c.age); got != c.want {
			t.Errorf("ContractSeasons(%d) = %d, want %d", c.age, got, c.want)
		}
	}
}
