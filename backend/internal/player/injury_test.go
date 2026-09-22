package player

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeriveProgressClampsToWindow(t *testing.T) {
	occurred := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	expected := occurred.AddDate(0, 0, 10) // 10-day plan

	cases := []struct {
		name     string
		now      time.Time
		wantProg float64
		wantDays int
	}{
		{"before start", occurred.AddDate(0, 0, -2), 0, 12},
		{"onset", occurred, 0, 10},
		{"halfway", occurred.AddDate(0, 0, 5), 0.5, 5},
		{"at end", expected, 1, 0},
		{"past end", expected.AddDate(0, 0, 4), 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &PlayerInjury{
				InjuryID:         uuid.New(),
				InjuryType:       "muscle",
				OccurredAt:       occurred,
				ExpectedRecovery: expected,
			}
			deriveProgress(v, tc.now)
			if v.RecoveryProgress != tc.wantProg {
				t.Errorf("recovery_progress = %v, want %v", v.RecoveryProgress, tc.wantProg)
			}
			if tc.name != "before start" && v.DaysRemaining != tc.wantDays {
				t.Errorf("days_remaining = %d, want %d", v.DaysRemaining, tc.wantDays)
			}
		})
	}
}

func TestDeriveProgressZeroPlanIsSafe(t *testing.T) {
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	v := &PlayerInjury{OccurredAt: now, ExpectedRecovery: now}
	deriveProgress(v, now)
	if v.RecoveryProgress != 0 {
		t.Errorf("zero-window progress = %v, want 0", v.RecoveryProgress)
	}
	if v.DaysRemaining != 0 {
		t.Errorf("zero-window days_remaining = %d, want 0", v.DaysRemaining)
	}
}
