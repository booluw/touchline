package competition

import "testing"

func TestReputationInRange(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want bool
	}{
		{n: -1, want: false},
		{n: 0, want: true},
		{n: 1, want: true},
		{n: 50, want: true},
		{n: 100, want: true},
		{n: 101, want: false},
	} {
		if got := reputationInRange(tt.n); got != tt.want {
			t.Errorf("reputationInRange(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}
