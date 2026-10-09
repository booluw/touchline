package competition

import (
	"testing"

	"github.com/google/uuid"
)

func TestStandingsWindow(t *testing.T) {
	table := func(n int) []StandingRow {
		rows := make([]StandingRow, n)
		for i := range rows {
			rows[i].Club.ID = uuid.New()
			rows[i].Position = i + 1
		}
		return rows
	}
	cases := []struct {
		name          string
		size, at      int // league size, our 1-based position
		first, length int // expected first position and row count
	}{
		{"middle", 24, 12, 9, 7},
		{"first", 24, 1, 1, 7},
		{"second", 24, 2, 1, 7},
		{"fourth", 24, 4, 1, 7},
		{"fifth", 24, 5, 2, 7},
		{"last", 24, 24, 18, 7},
		{"second last", 24, 23, 18, 7},
		{"small league", 5, 3, 1, 5},
		{"exactly seven", 7, 7, 1, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := table(tc.size)
			got, ok := StandingsWindow(rows, rows[tc.at-1].Club.ID, 3)
			if !ok {
				t.Fatal("club not found")
			}
			if len(got) != tc.length || got[0].Position != tc.first {
				t.Fatalf("got %d rows from %d, want %d from %d", len(got), got[0].Position, tc.length, tc.first)
			}
			found := false
			for _, r := range got {
				found = found || r.Club.ID == rows[tc.at-1].Club.ID
			}
			if !found {
				t.Fatal("window does not contain our club")
			}
		})
	}
	if _, ok := StandingsWindow(table(10), uuid.New(), 3); ok {
		t.Fatal("unknown club must report !ok")
	}
}
