// Injury reads + the manager's rushed-return command (S08-03). The weekly
// recovery scan (setbacks + due recoveries) runs inside WeeklyTick;
// these two methods are the read model and the one player-facing write. The
// recurrence/severity/history side effects live in internal/injury.
package player

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/injury"
)

// PlayerInjury is the read shape for one open injury with the recovery
// progress derived at read time (elapsed / planned window), never stored.
type PlayerInjury struct {
	InjuryID         uuid.UUID  `json:"injury_id"`
	PlayerID         uuid.UUID  `json:"player_id"`
	InjuryType       string     `json:"injury_type"`
	Severity         int        `json:"severity"`
	OccurredAt       time.Time  `json:"occurred_at"`
	ExpectedRecovery time.Time  `json:"expected_recovery_date"`
	RecurrenceRisk   float64    `json:"recurrence_risk"`
	RecoveryProgress float64    `json:"recovery_progress"`
	DaysRemaining    int        `json:"days_remaining"`
	ActualRecovery   *time.Time `json:"actual_recovery_date,omitempty"`
}

// ErrNoOpenInjury surfaces when a player has no injury to rush back from.
var ErrNoOpenInjury = errors.New("player has no open injury")

// GetPlayerInjury returns the player's open injury with a read-time derived
// recovery progress, or nil when the player is not sidelined. Ownership is
// enforced exactly like the morale/development reads (S08-03).
func (s *Service) GetPlayerInjury(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*PlayerInjury, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	prof, err := playerProfile(ctx, s.pool, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.ClubID == nil || *prof.ClubID != clubID || prof.WorldID != worldID {
		return nil, ErrPlayerNotInClub
	}

	v, err := s.loadInjury(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	deriveProgress(v, time.Now().UTC())
	return v, nil
}

// RushReturn closes a player's open injury immediately on manager instruction,
// stamping the rushed-return recurrence floor and emitting PLAYER_RUSHED_RETURN
// (S08-03, dedicated endpoint). Ownership and club membership are enforced; a
// fit player or an injury already recovered is a no-open-injury error.
func (s *Service) RushReturn(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*PlayerInjury, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	prof, err := playerProfile(ctx, s.pool, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.ClubID == nil || *prof.ClubID != clubID || prof.WorldID != worldID {
		return nil, ErrPlayerNotInClub
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("rush return: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tick, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return nil, fmt.Errorf("rush return: %w", err)
	}
	injuryID, _, _, ok, err := injury.OpenForPlayer(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoOpenInjury
	}
	if err := injury.RushClose(ctx, tx, s.bus, worldID, tick, clubID, playerID, injuryID, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("rush return: %w", err)
	}

	var v PlayerInjury
	var actual time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, player_id, injury_type, severity, occurred_at, expected_recovery_date,
		       recurrence_risk::float8, actual_recovery_date
		FROM player.injuries WHERE id = $1`, injuryID).
		Scan(&v.InjuryID, &v.PlayerID, &v.InjuryType, &v.Severity, &v.OccurredAt,
			&v.ExpectedRecovery, &v.RecurrenceRisk, &actual)
	if err != nil {
		return nil, fmt.Errorf("rush return: reload: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("rush return: commit: %w", err)
	}
	v.ActualRecovery = &actual
	v.RecoveryProgress = 1
	v.DaysRemaining = 0
	return &v, nil
}

// loadInjury reads the player's most recent open injury row, or nil.
func (s *Service) loadInjury(ctx context.Context, playerID uuid.UUID) (*PlayerInjury, error) {
	var v PlayerInjury
	err := s.pool.QueryRow(ctx, `
		SELECT id, player_id, injury_type, severity, occurred_at, expected_recovery_date,
		       recurrence_risk::float8
		FROM player.injuries
		WHERE player_id = $1 AND actual_recovery_date IS NULL
		ORDER BY occurred_at DESC LIMIT 1`, playerID).
		Scan(&v.InjuryID, &v.PlayerID, &v.InjuryType, &v.Severity, &v.OccurredAt,
			&v.ExpectedRecovery, &v.RecurrenceRisk)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load injury: %w", err)
	}
	return &v, nil
}

// deriveProgress fills recovery_progress (clamped 0..1) and days_remaining from
// the persisted dates at read time.
func deriveProgress(v *PlayerInjury, now time.Time) {
	total := int(math.Round(v.ExpectedRecovery.Sub(v.OccurredAt).Hours() / 24))
	if total < 1 {
		total = 1
	}
	elapsed := int(math.Floor(now.Sub(v.OccurredAt).Hours() / 24))
	if elapsed < 0 {
		elapsed = 0
	}
	progress := float64(elapsed) / float64(total)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	v.RecoveryProgress = progress
	remaining := int(math.Ceil(v.ExpectedRecovery.Sub(now).Hours() / 24))
	if remaining < 0 {
		remaining = 0
	}
	v.DaysRemaining = remaining
}
