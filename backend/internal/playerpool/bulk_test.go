package playerpool

import "testing"

func TestQualityOffset(t *testing.T) {
	cases := map[string]struct {
		want   int
		exists bool
	}{
		"low":   {-10, true},
		"mid":   {0, true},
		"high":  {15, true},
		"elite": {25, true},
		"world": {0, false},
		"":      {0, false},
	}
	for q, tc := range cases {
		got, ok := QualityOffset(q)
		if got != tc.want || ok != tc.exists {
			t.Errorf("QualityOffset(%q) = (%d, %v), want (%d, %v)", q, got, ok, tc.want, tc.exists)
		}
	}
}

func TestBulkOptsValidate(t *testing.T) {
	valid := BulkOpts{Count: 50, MinAge: 17, MaxAge: 25, Quality: "mid", Positions: []string{"CM", "ST"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid opts rejected: %v", err)
	}

	bad := []BulkOpts{
		{Count: 0, MinAge: 17, MaxAge: 25, Quality: "mid"},
		{Count: 501, MinAge: 17, MaxAge: 25, Quality: "mid"},
		{Count: 10, MinAge: 17, MaxAge: 25, Quality: "godlike"},
		{Count: 10, MinAge: 12, MaxAge: 20, Quality: "mid"},
		{Count: 10, MinAge: 20, MaxAge: 17, Quality: "mid"},
		{Count: 10, MinAge: 17, MaxAge: 25, Quality: "mid", Positions: []string{"XX"}},
		{Count: 10, MinAge: 17, MaxAge: 25, Quality: "mid", Positions: []string{"CM", "GK", "RB"}}, // all valid
	}
	// The last case is valid on purpose: adjust expectations explicitly.
	if err := bad[len(bad)-1].Validate(); err != nil {
		t.Errorf("valid mixed positions rejected: %v", err)
	}
	bad = bad[:len(bad)-1]

	for i, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("case %d expected validation error, got nil (%+v)", i, o)
		}
	}
}
