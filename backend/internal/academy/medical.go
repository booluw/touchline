package academy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/explanation"
)

// Medical facility (S08-03): a club's medical-staff level lives in
// club.facilities.facility_type = 'medical'. A club with no medical row reads
// as the documented neutral default (level 5), so the upgrade command
// materialises a row at the current implied level on first use and ratchets it
// one step at a time. Costs are cumulative (see MedicalUpgradeCost), debited
// through finance and deduped per (club, target level) so a redelivered
// request never double-charges.

// Event type for the medical facility.
const (
	EventMedicalUpgrade = "MEDICAL_FACILITY_UPGRADED"

	MedicalNeutralLevel = 5 // implied level for a club with no medical row
	MedicalMinLevel     = 1
	MedicalMaxLevel     = 10
	MedicalCostBase     = 200_000 // £ per cost step; see MedicalUpgradeCost
)

// MedicalFacility is the read shape for GET /api/clubs/:id/medical-facility.
// The club id stays internal; the wire carries a nested club ref.
type MedicalFacility struct {
	ClubID     uuid.UUID       `json:"-"`
	Club       *apiref.ClubRef `json:"club"`
	Level      int             `json:"level"`
	UpgradedAt *time.Time      `json:"upgraded_at,omitempty"`
}

var ErrMedicalMaxLevel = errors.New("academy: medical facility already at maximum level")

// MedicalUpgradeCost returns the cumulative cost to raise the medical facility
// from level from to level to. The per-step cost is MedicalCostBase × step, so
// the total is MedicalCostBase × (to(to-1) − from(from-1)) / 2: reaching level
// 6 from the neutral 5 costs £1.0M, level 10 costs £7.0M from 1.
func MedicalUpgradeCost(from, to int) int64 {
	if to <= from {
		return 0
	}
	return MedicalCostBase * int64(to*(to-1)-from*(from-1)) / 2
}

// ensureMedicalTx materialises a medical row at the current implied level for a
// club that has none yet (ON CONFLICT DO NOTHING keeps it idempotent).
func ensureMedicalTx(ctx context.Context, tx pgx.Tx, clubID uuid.UUID, level int) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO club.facilities (club_id, facility_type, level)
		VALUES ($1, 'medical', $2)
		ON CONFLICT (club_id, facility_type) DO NOTHING`, clubID, level); err != nil {
		return fmt.Errorf("ensure medical facility: %w", err)
	}
	return nil
}

// GetMedicalFacility returns a club's current medical level (neutral default 5
// before the club ever builds a facility; no row is written on read).
func (s *Service) GetMedicalFacility(ctx context.Context, clubID uuid.UUID) (MedicalFacility, error) {
	var (
		level      int
		upgradedAt time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT level, upgraded_at FROM club.facilities
		WHERE club_id = $1 AND facility_type = 'medical'`, clubID).Scan(&level, &upgradedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.medicalView(ctx, clubID, MedicalNeutralLevel, nil)
	}
	if err != nil {
		return MedicalFacility{}, fmt.Errorf("get medical facility: %w", err)
	}
	t := upgradedAt
	return s.medicalView(ctx, clubID, level, &t)
}

// medicalView builds a MedicalFacility with its nested club ref resolved.
func (s *Service) medicalView(ctx context.Context, clubID uuid.UUID, level int, upgradedAt *time.Time) (MedicalFacility, error) {
	m := MedicalFacility{ClubID: clubID, Level: level, UpgradedAt: upgradedAt}
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM club.clubs WHERE id = $1`, clubID).Scan(&name); err != nil {
		return MedicalFacility{}, fmt.Errorf("medical facility: club name: %w", err)
	}
	m.Club = &apiref.ClubRef{ID: clubID, Name: name}
	return m, nil
}

// UpgradeMedical raises a club's medical facility one level on behalf of its
// manager (ownership-gated): the step is debited from the club account under
// the 'facilities' category with a (club, target-level) dedup key, the row is
// materialised if absent, and MEDICAL_FACILITY_UPGRADED is emitted. A club at
// level 10 returns the current row untouched (no-op, like SetActive).
func (s *Service) UpgradeMedical(ctx context.Context, managerID, clubID uuid.UUID) (MedicalFacility, error) {
	if err := s.RequireOwnership(ctx, managerID, clubID); err != nil {
		return MedicalFacility{}, err
	}
	cur, err := s.GetMedicalFacility(ctx, clubID)
	if err != nil {
		return MedicalFacility{}, err
	}
	if cur.Level >= MedicalMaxLevel {
		return cur, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MedicalFacility{}, fmt.Errorf("upgrade medical: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ctxClub, err := loadClubContext(ctx, tx, clubID)
	if err != nil {
		return MedicalFacility{}, err
	}
	// Re-read inside the tx so a concurrent upgrade serialises on the row.
	var from int
	err = tx.QueryRow(ctx, `
		SELECT level FROM club.facilities
		WHERE club_id = $1 AND facility_type = 'medical'
		FOR UPDATE`, clubID).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		from = MedicalNeutralLevel
	} else if err != nil {
		return MedicalFacility{}, fmt.Errorf("upgrade medical: lock: %w", err)
	}
	if from >= MedicalMaxLevel {
		v, err := s.medicalView(ctx, clubID, from, cur.UpgradedAt)
		if err != nil {
			_ = tx.Rollback(ctx)
			return MedicalFacility{}, err
		}
		return v, tx.Commit(ctx)
	}
	to := from + 1

	if err := ensureMedicalTx(ctx, tx, clubID, from); err != nil {
		return MedicalFacility{}, err
	}
	accountID, err := finance.EnsureAccount(ctx, tx, ctxClub.WorldID, clubID)
	if err != nil {
		return MedicalFacility{}, err
	}
	cost := MedicalUpgradeCost(from, to)
	if _, err := finance.Post(ctx, tx, accountID, "debit", "facilities", cost,
		fmt.Sprintf("Medical facility upgrade to level %d", to), nil, time.Now().UTC(),
		fmt.Sprintf("medical:upgrade:%s:%d", clubID, to)); err != nil {
		return MedicalFacility{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE club.facilities SET level = $3, upgraded_at = now()
		WHERE club_id = $1 AND facility_type = $2`, clubID, "medical", to); err != nil {
		return MedicalFacility{}, fmt.Errorf("upgrade medical: update: %w", err)
	}

	exp := explanation.New("medical_facility_upgrade", 0).
		Add(fmt.Sprintf("medical staff level %d", to), 0).
		Add(fmt.Sprintf("upgrade cost £%d", cost), int(-cost))
	exJSON, _ := json.Marshal(exp)
	payload := mustJSON(map[string]any{
		"club_id":    clubID,
		"from_level": from,
		"to_level":   to,
		"cost":       cost,
	})
	if err := s.recordEvent(ctx, tx, ctxClub.WorldID, EventMedicalUpgrade, "manager", &managerID, payload, exJSON); err != nil {
		return MedicalFacility{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MedicalFacility{}, fmt.Errorf("upgrade medical: commit: %w", err)
	}
	now := time.Now().UTC()
	return s.medicalView(ctx, clubID, to, &now)
}
