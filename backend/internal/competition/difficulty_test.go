package competition

import (
	"testing"

	"github.com/touchline/backend/internal/squad"
)

func TestRateDifficultyLevels(t *testing.T) {
	cases := []struct {
		name         string
		ours, theirs float64
		home         bool
		form         []string
		level        int
	}{
		// Equal sides, neutral form: home is one notch easier than away
		// only through the venue swing, both inside "Even".
		{"even at home", 70, 70, true, []string{"W", "W", "D", "L", "L"}, 3},
		{"even away", 70, 70, false, []string{"W", "W", "D", "L", "L"}, 3},
		{"much weaker opponent at home", 75, 65, true, nil, 1},
		{"much stronger opponent away", 65, 75, false, []string{"W", "W", "W", "W", "W"}, 5},
		{"slightly stronger away", 70, 73, false, nil, 4}, // 3 + 2 = 5 -> Hard
		{"bound: score exactly -8 is very easy", 76, 70, true, nil, 1},
		{"bound: score -7 is easy", 75, 70, true, nil, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := rateDifficulty(tc.ours, tc.theirs, tc.home, tc.form)
			if d.Level != tc.level {
				t.Fatalf("level = %d (%s), want %d; factors %+v", d.Level, d.Label, tc.level, d.Factors)
			}
			if len(d.Factors) != 3 {
				t.Fatalf("want 3 factors, got %d", len(d.Factors))
			}
		})
	}
}

func TestRateDifficultyVenueSwing(t *testing.T) {
	home := rateDifficulty(70, 70, true, nil)
	away := rateDifficulty(70, 70, false, nil)
	if away.Factors[1].Delta-home.Factors[1].Delta != 2*difficultyVenue {
		t.Fatalf("venue swing = %d, want %d", away.Factors[1].Delta-home.Factors[1].Delta, 2*difficultyVenue)
	}
}

func TestXIStrengthSkipsUnavailableAndCapsAtEleven(t *testing.T) {
	strong := squad.AttributeSnapshot{Technical: 90, Physical: 90, Mental: 90, Tactical: 90, Goalkeeping: 90, Positional: 90}
	players := []squad.LoadedPlayer{}
	for i := 0; i < 11; i++ {
		players = append(players, squad.LoadedPlayer{Position: "MC", Available: true, Attributes: strong})
	}
	full := xiStrength(players)
	// A weaker 12th player must not drag the XI down; an injured star must
	// not lift it.
	weak := squad.LoadedPlayer{Position: "MC", Available: true}
	injured := squad.LoadedPlayer{Position: "MC", Available: false, Attributes: strong}
	if got := xiStrength(append(players, weak, injured)); got != full {
		t.Fatalf("xi = %v, want %v", got, full)
	}
	if xiStrength(nil) != 0 {
		t.Fatal("empty squad must rate 0")
	}
}
