package admin

import (
	"context"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Finance
// ---------------------------------------------------------------------------

// Finance returns the country economy: crisis clubs, wage bill and budgets.
func (s *Service) Finance(ctx context.Context, worldID, countryID uuid.UUID) (*FinancePanels, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	f := &FinancePanels{Country: country, CrisisClubs: []CrisisClubRow{}, TopWageBills: []WageRow{}}
	if len(clubIDs) == 0 {
		return f, nil
	}
	f.CrisisClubs = s.crisisClubs(ctx, clubIDs)
	es := s.economySummary(ctx, clubIDs)
	f.WageBill = es.WageBill
	f.Cash = es.Cash
	f.WageBudget.Allocated = es.WageAllocated
	f.WageBudget.Committed = es.WageCommitted
	f.WageBudget.Available = es.WageAllocated - es.WageCommitted
	f.WageBudget.UtilizedPct = pct(es.WageCommitted, es.WageAllocated)
	f.TransferBudget.Allocated = es.TransferAllocated
	f.TransferBudget.Committed = es.TransferCommitted
	f.TransferBudget.Available = es.TransferAllocated - es.TransferCommitted
	f.TransferBudget.UtilizedPct = pct(es.TransferCommitted, es.TransferAllocated)
	f.TopWageBills = s.topWageBills(ctx, clubIDs)
	return f, nil
}

func (s *Service) crisisClubs(ctx context.Context, clubIDs []uuid.UUID) []CrisisClubRow {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.short_name, f.stage, f.started_at
		FROM finance.financial_crisis_states f
		JOIN club.clubs c ON c.id = f.club_id
		WHERE f.club_id = ANY($1) AND f.resolved_at IS NULL
		ORDER BY f.started_at DESC`, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CrisisClubRow{}
	for rows.Next() {
		var r CrisisClubRow
		if err := rows.Scan(&r.ClubID, &r.ClubName, &r.Stage, &r.StartedAt); err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

func (s *Service) topWageBills(ctx context.Context, clubIDs []uuid.UUID) []WageRow {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.short_name, COALESCE(SUM(w.weekly_wage)::bigint, 0), COUNT(w.id)
		FROM club.clubs c
		LEFT JOIN finance.wage_commitments w
		  ON w.club_id = c.id AND w.end_date > CURRENT_DATE
		WHERE c.id = ANY($1)
		GROUP BY c.id
		ORDER BY COALESCE(SUM(w.weekly_wage), 0) DESC
		LIMIT 20`, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []WageRow{}
	for rows.Next() {
		var r WageRow
		if err := rows.Scan(&r.ClubID, &r.ClubName, &r.WeeklyBill, &r.Count); err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

func pct(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	return int(float64(part) / float64(whole) * 100)
}
