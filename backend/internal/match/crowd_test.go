package match

import (
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
)

func TestStadiumCapacityStableAndBanded(t *testing.T) {
	id := uuid.MustParse("6f1c2b4a-0000-4000-8000-000000000001")
	if a, b := stadiumCapacity(40, id), stadiumCapacity(40, id); a != b {
		t.Fatalf("capacity not stable for one club: %d vs %d", a, b)
	}
	for _, rep := range []int{0, 50, 100} {
		mid := float64(crowdCapacityBase + rep*crowdCapacityPerRep)
		for i := 0; i < 50; i++ {
			c := float64(stadiumCapacity(rep, uuid.New()))
			if c < mid*(1-crowdCapacityJitter)-1 || c > mid*(1+crowdCapacityJitter)+1 {
				t.Fatalf("rep %d: capacity %.0f outside ±%.0f%% of %.0f", rep, c, crowdCapacityJitter*100, mid)
			}
		}
	}
	if stadiumCapacity(100, id) <= stadiumCapacity(0, id) {
		t.Fatal("a bigger league must give the same club a bigger ground")
	}
}

func TestCrowdAttendance(t *testing.T) {
	const capacity = 20000
	neutral := CrowdInputs{Sentiment: 50, Loyalty: 50}
	if a, b := crowdAttendance(capacity, neutral, 7), crowdAttendance(capacity, neutral, 7); a != b {
		t.Fatalf("same seed gave different crowds: %d vs %d", a, b)
	}

	euphoric := CrowdInputs{Sentiment: 100, Loyalty: 100, Fixture: squad.FixtureContext{IsDerby: true, IsSixPointer: true}}
	if got := crowdAttendance(capacity, euphoric, 7); got != capacity {
		t.Fatalf("over-full demand must cap at capacity, got %d", got)
	}
	bleak := CrowdInputs{Sentiment: 0, Loyalty: 0, Fixture: squad.FixtureContext{IsDeadRubber: true, IsCupTie: true}}
	if got, floor := crowdAttendance(capacity, bleak, 7), int(capacity*crowdMinFill); got != floor {
		t.Fatalf("collapsed demand must floor at %d, got %d", floor, got)
	}

	derby := neutral
	derby.Fixture.IsDerby = true
	if crowdAttendance(capacity, derby, 7) <= crowdAttendance(capacity, neutral, 7) {
		t.Fatal("a derby must draw a bigger crowd than the same fixture without it")
	}
	happy := CrowdInputs{Sentiment: 80, Loyalty: 50}
	if crowdAttendance(capacity, happy, 7) <= crowdAttendance(capacity, neutral, 7) {
		t.Fatal("higher supporter sentiment must draw a bigger crowd")
	}
}
