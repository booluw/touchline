package competition

import (
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

func TestPositionsAfterReplaysMatchdays(t *testing.T) {
	a := apiref.ClubRef{ID: uuid.New(), Name: "Ashby"}
	b := apiref.ClubRef{ID: uuid.New(), Name: "Brennock"}
	c := apiref.ClubRef{ID: uuid.New(), Name: "Calder"}
	d := apiref.ClubRef{ID: uuid.New(), Name: "Dunmore"}
	score := func(n int) *int { return &n }
	fx := func(md int, h, aw apiref.ClubRef, hs, as *int) Fixture {
		f := Fixture{ID: uuid.New(), HomeClub: h, AwayClub: aw, Matchday: md, Status: "scheduled"}
		if hs != nil {
			f.Status, f.HomeScore, f.AwayScore = "completed", hs, as
		}
		return f
	}
	league := []Fixture{
		fx(1, c, a, score(0), score(2)), // Calder lose
		fx(1, b, d, score(1), score(1)),
		fx(2, a, c, score(0), score(3)), // Calder win big
		fx(2, d, b, score(0), score(0)),
		fx(3, c, b, nil, nil), // unplayed
	}
	got := positionsAfter(league, c.ID)

	// MD1: Ashby 3, Brennock 1, Dunmore 1 (B before D by name), Calder 0 → 4th.
	if p := got[league[0].ID]; p != 4 {
		t.Fatalf("after MD1: want 4th, got %d", p)
	}
	// MD2: Calder 3 pts GD +1, Ashby 3 pts GD -1 → Calder 1st.
	if p := got[league[2].ID]; p != 1 {
		t.Fatalf("after MD2: want 1st, got %d", p)
	}
	if p, ok := got[league[4].ID]; ok {
		t.Fatalf("unplayed fixture must have no position, got %d", p)
	}
	if p, ok := got[league[1].ID]; ok {
		t.Fatalf("another club fixture must have no position, got %d", p)
	}
}
