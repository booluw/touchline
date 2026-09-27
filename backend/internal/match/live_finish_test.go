package match

import (
	"testing"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/matchsim"
)

// TestLiveFinished walks the whole minute ladder of a live match and asserts the
// one invariant the "match never ends" bug lived on: full time is reported
// exactly once, on the last step the match is allowed to take.
func TestLiveFinished(t *testing.T) {
	cases := []struct {
		name       string
		goldenGoal bool
		lastMinute int
		finishedAt int // minute whose pace call reports finished (0 = never)
	}{
		{name: "regulation", lastMinute: matchsim.RegulationMinutes, finishedAt: matchsim.RegulationMinutes},
		{name: "golden goal tie", goldenGoal: true, lastMinute: matchsim.RegulationMinutes + 1, finishedAt: matchsim.RegulationMinutes + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := &LiveSession{fc: squad.FixtureContext{GoldenGoal: tc.goldenGoal}}
			if got := liveMinuteBound(tc.goldenGoal); got != tc.lastMinute {
				t.Fatalf("liveMinuteBound = %d, want %d", got, tc.lastMinute)
			}
			for m := 1; m <= tc.lastMinute; m++ {
				sess.nextMinute = m + 1
				finished := liveFinished(sess, m)
				want := m == tc.finishedAt
				if finished != want {
					t.Fatalf("minute %d: finished = %v, want %v", m, finished, want)
				}
			}
			// Past the bound PaceMinute short-circuits to finished without
			// persisting, which is what lets Finalize run.
			sess.nextMinute = tc.lastMinute + 1
			if !liveFinished(sess, tc.lastMinute+1) {
				t.Fatalf("minute %d: finished = false past the bound", tc.lastMinute+1)
			}
		})
	}
}
