// Package-level unit coverage for the season-kickoff date rule (StartSeasonKickoff):
// the pure anchor resolution and the invariant that matchday 1 lands exactly on
// the requested date under BOTH pacing formulas (legacy IM03 day-formula and the
// IM05 weekday walk).
package competition

import (
	"errors"
	"testing"
	"time"
)

func TestResolveKickoffAnchor(t *testing.T) {
	today := time.Date(2031, 8, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		kickoff   time.Time
		weekdays  []int
		want      time.Time
		wantErr   error
		checkPace bool // assert matchday 1 lands on the kickoff under both pacers
	}{
		{
			name: "kickoff on the world's current date is allowed",
			// 2031-08-01 is a Friday (ISO 5).
			kickoff:  today,
			weekdays: nil,
			want:     today.AddDate(0, 0, -1),
		},
		{
			name:      "future kickoff anchors the day before",
			kickoff:   today.AddDate(0, 0, 3),
			weekdays:  nil,
			want:      today.AddDate(0, 0, 2),
			checkPace: true,
		},
		{
			name:     "yesterday is rejected",
			kickoff:  today.AddDate(0, 0, -1),
			weekdays: nil,
			wantErr:  ErrKickoffDateInPast,
		},
		{
			name:     "off-weekday kickoff rejected",
			kickoff:  today.AddDate(0, 0, 1), // Saturday
			weekdays: []int{5, 5, 7},         // Fri + Sun declared
			wantErr:  ErrKickoffNotAllowedWeekday,
		},
		{
			name:      "allowed-weekday kickoff honored by the weekday walk",
			kickoff:   today.AddDate(0, 0, 1), // Saturday
			weekdays:  []int{5, 6, 7},         // Fri/Sat/Sun
			want:      today,                  // anchor = day before
			checkPace: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor, err := resolveKickoffAnchor(today, tc.kickoff, tc.weekdays)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !anchor.Equal(tc.want) {
				t.Fatalf("anchor = %v, want %v", anchor, tc.want)
			}
			if tc.checkPace {
				day := daysTruncate(tc.kickoff)
				// Legacy formula: matchday 1 = anchor + 1.
				if got := scheduledAtFromDay(anchor, 1, 7, 3, 14); !daysTruncate(got).Equal(day) {
					t.Errorf("legacy pacing matchday 1 = %v, want %v", daysTruncate(got), day)
				}
				if len(tc.weekdays) > 0 {
					if got := paceWeekdayMatchday(anchor, 1, normalizeWeekdays(tc.weekdays), 14); !daysTruncate(got).Equal(day) {
						t.Errorf("weekday-walk matchday 1 = %v, want %v", daysTruncate(got), day)
					}
				}
			}
		})
	}
}
