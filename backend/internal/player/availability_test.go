package player

import (
	"testing"

	"github.com/touchline/backend/internal/squad"
)

func TestAvailabilityReason(t *testing.T) {
	cases := []struct {
		in     squad.LoadedPlayer
		ok     bool
		reason string
	}{
		{squad.LoadedPlayer{Available: true}, true, ""},
		{squad.LoadedPlayer{Injured: true}, false, "injured"},
		{squad.LoadedPlayer{}, false, "ineligible"},
	}
	for _, c := range cases {
		ok, reason := availability(c.in)
		if ok != c.ok || reason != c.reason {
			t.Errorf("%+v: got %v %q, want %v %q", c.in, ok, reason, c.ok, c.reason)
		}
	}
}
