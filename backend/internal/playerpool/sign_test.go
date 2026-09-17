package playerpool

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCheckSignEligible(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name      string
		status    string
		club      *uuid.UUID
		origin    string
		age       int
		want      error
		wantBlank bool // club == nil and free agent => nil
	}{
		{name: "free agent adult academy", status: "free_agent", club: nil, origin: "academy", age: 17, wantBlank: true},
		{name: "free agent adult street", status: "free_agent", club: nil, origin: "street", age: 21, wantBlank: true},
		{name: "street under 18 blocked", status: "free_agent", club: nil, origin: "street", age: 17, want: ErrStreetUnder18},
		{name: "street 18 allowed", status: "free_agent", club: nil, origin: "street", age: 18, wantBlank: true},
		{name: "already on club", status: "active", club: &id, origin: "academy", age: 22, want: ErrNotFreeAgent},
		{name: "retired", status: "retired", club: nil, origin: "academy", age: 40, want: ErrNotFreeAgent},
		{name: "on loan status", status: "on_loan", club: nil, origin: "academy", age: 19, want: ErrNotFreeAgent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSignEligible(tc.status, tc.club, tc.origin, tc.age)
			if tc.wantBlank {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestYearsSince(t *testing.T) {
	ref := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got := yearsSince(time.Date(2000, 9, 1, 0, 0, 0, 0, time.UTC), ref); got != 26 {
		t.Fatalf("yearsSince(2000-09-01) = %d, want 26", got)
	}
	// Birthday later this year counts as one less.
	if got := yearsSince(time.Date(2000, 12, 1, 0, 0, 0, 0, time.UTC), ref); got != 25 {
		t.Fatalf("yearsSince(Dec) = %d, want 25", got)
	}
	if got := yearsSince(time.Time{}, ref); got != 0 {
		t.Fatalf("yearsSince(zero) = %d, want 0", got)
	}
}

func TestApplyFreeAgentFilter(t *testing.T) {
	base := func(pos string, age int, nat string) candidate {
		return candidate{fa: FreeAgent{PrimaryPosition: pos, Age: age, NationalityCode: nat}, ovr: 60}
	}
	fresh := func() []candidate {
		return []candidate{
			base("GK", 22, "eng"),
			base("ST", 28, "br"),
			base("CM", 19, "eng"),
			base("CB", 31, "fr"),
		}
	}

	pos := "ST"
	filtered := applyFreeAgentFilter(fresh(), FreeAgentFilter{Position: &pos})
	if len(filtered) != 1 || filtered[0].fa.PrimaryPosition != "ST" {
		t.Fatalf("position filter = %+v", filtered)
	}

	lo, hi := 20, 30
	filtered = applyFreeAgentFilter(fresh(), FreeAgentFilter{AgeMin: &lo, AgeMax: &hi})
	if len(filtered) != 2 {
		t.Fatalf("age range filter = %+v, want 2", filtered)
	}

	nat := "eng"
	filtered = applyFreeAgentFilter(fresh(), FreeAgentFilter{Nationality: &nat})
	if len(filtered) != 2 {
		t.Fatalf("nationality filter = %+v, want 2", filtered)
	}

	// Empty filter is identity (all four survive).
	if got := applyFreeAgentFilter(fresh(), FreeAgentFilter{}); len(got) != 4 {
		t.Fatalf("empty filter dropped candidates: %+v", got)
	}
}
