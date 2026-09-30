package board

import (
	"strings"
	"testing"
)

func TestBuildFanReaction(t *testing.T) {
	h1, b1 := buildFanReaction(42, "Harbour FC", "Ada Lovelace", "Riverside", 0, 3, 20, 10)
	h2, b2 := buildFanReaction(42, "Harbour FC", "Ada Lovelace", "Riverside", 0, 3, 20, 10)
	if h1 != h2 || b1 != b2 {
		t.Fatal("same seed must give the same story")
	}
	for _, want := range []string{"Harbour FC", "Ada Lovelace", "0-3", "defeat to Riverside", "turn on"} {
		if !strings.Contains(h1, want) {
			t.Errorf("headline %q missing %q", h1, want)
		}
	}
	if n := strings.Count(b1, " said "); n < 2 || n > 3 {
		t.Errorf("fans quoted = %d, want 2-3", n)
	}
	if !strings.Contains(b1, "Ada Lovelace") {
		t.Errorf("body does not mention the manager: %q", b1)
	}
	if h, _ := buildFanReaction(7, "Harbour FC", "", "Riverside", 4, 0, 90, 95); !strings.Contains(h, "hail the manager") {
		t.Errorf("delighted headline = %q", h)
	}
	if fanMood(20, 10) != 0 || fanMood(45, 45) != 1 || fanMood(60, 60) != 2 || fanMood(90, 95) != 3 {
		t.Error("mood bands wrong")
	}
}
