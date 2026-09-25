package competition

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestDefaultBandForReputation(t *testing.T) {
	cases := []struct {
		name     string
		rep      int
		wantFrom int
		wantTo   int
	}{
		{"floor", 0, 1, 1},
		{"below 60", 50, 1, 1},
		{"at 60", 60, 1, 2},
		{"between 60 and 75", 74, 1, 2},
		{"at 75", 75, 1, 3},
		{"between 75 and 90", 89, 1, 3},
		{"at 90", 90, 1, 4},
		{"ceiling", 100, 1, 4},
		{"clamped low", -5, 1, 1},
		{"clamped high", 101, 1, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, to := DefaultBandForReputation(c.rep)
			if from != c.wantFrom || to != c.wantTo {
				t.Fatalf("DefaultBandForReputation(%d) = %d..%d, want %d..%d",
					c.rep, from, to, c.wantFrom, c.wantTo)
			}
		})
	}
}

func TestInputsToBands(t *testing.T) {
	league := uuid.New()
	nilTo := (*int)(nil)
	bands, err := inputsToBands([]QualBandInput{
		{LeagueID: league, From: 1, To: nilTo},
		{LeagueID: uuid.New(), From: 2, To: ptr(4)},
	})
	if err != nil {
		t.Fatalf("valid inputs: %v", err)
	}
	if len(bands) != 2 || bands[0].To != nil {
		t.Fatalf("bands = %+v, want 2 bands with the open-ended one first", bands)
	}

	if _, err := inputsToBands([]QualBandInput{{LeagueID: league, From: 0, To: ptr(1)}}); err == nil {
		t.Fatal("from_position 0 must be rejected")
	}
	if _, err := inputsToBands([]QualBandInput{{LeagueID: league, From: 2, To: ptr(1)}}); err == nil {
		t.Fatal("to_position < from_position must be rejected")
	}
	if _, err := inputsToBands([]QualBandInput{{From: 1, To: ptr(1)}}); err == nil {
		t.Fatal("missing league must be rejected")
	}
}

func TestValidateBandsNoOverlap(t *testing.T) {
	l1, l2 := uuid.New(), uuid.New()

	if err := validateBandsNoOverlap(nil); err != nil {
		t.Fatalf("empty bands: %v", err)
	}
	// Distinct leagues never conflict.
	if err := validateBandsNoOverlap([]Band{
		{LeagueID: l1, From: 1, To: ptr(2)},
		{LeagueID: l2, From: 1, To: ptr(4)},
	}); err != nil {
		t.Fatalf("distinct leagues: %v", err)
	}
	// Adjacent bands on one league are fine.
	if err := validateBandsNoOverlap([]Band{
		{LeagueID: l1, From: 1, To: ptr(2)},
		{LeagueID: l1, From: 3, To: ptr(4)},
	}); err != nil {
		t.Fatalf("adjacent: %v", err)
	}
	// Touching/overlapping bands do conflict...
	for _, bad := range [][]Band{
		{{LeagueID: l1, From: 1, To: ptr(2)}, {LeagueID: l1, From: 2, To: ptr(4)}},
		{{LeagueID: l1, From: 3, To: ptr(5)}, {LeagueID: l1, From: 1, To: ptr(4)}},
	} {
		if err := validateBandsNoOverlap(bad); !errors.Is(err, ErrQualificationOverlap) {
			t.Fatalf("overlap %+v err = %v, want ErrQualificationOverlap", bad, err)
		}
	}
	// An open-ended band (thru last place) blocks everything after it.
	if err := validateBandsNoOverlap([]Band{
		{LeagueID: l1, From: 1, To: nil},
		{LeagueID: l1, From: 2, To: ptr(4)},
	}); !errors.Is(err, ErrQualificationOverlap) {
		t.Fatalf("thru-last then band err = %v, want ErrQualificationOverlap", err)
	}
}
