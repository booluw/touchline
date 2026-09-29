package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// ---------------------------------------------------------------------------
// Clubs
// ---------------------------------------------------------------------------

// Clubs returns the country's clubs with league, squad size, finance sanity
// and crisis state per line.
func (s *Service) Clubs(ctx context.Context, worldID, countryID uuid.UUID) (*ClubsPanels, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if len(clubIDs) == 0 {
		return &ClubsPanels{Country: country, Clubs: []ClubRow{}}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.short_name, c.is_ai_controlled, c.tier,
		       cc.competition_id, COALESCE(ccomp.name, ''), COALESCE(ccomp.tier, 0),
		       (SELECT COUNT(*) FROM player.players p WHERE p.club_id = c.id),
		       COALESCE((SELECT COALESCE(SUM(w.weekly_wage)::bigint, 0)
		                  FROM finance.wage_commitments w
		                  WHERE w.club_id = c.id AND w.end_date > CURRENT_DATE), 0),
		       COALESCE((SELECT f.stage FROM finance.financial_crisis_states f
		                  WHERE f.club_id = c.id AND f.resolved_at IS NULL
		                  ORDER BY f.started_at DESC LIMIT 1), ''),
		       COALESCE(top.display_name, ''), COALESCE(top.mv, 0)
		FROM club.clubs c
		JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		LEFT JOIN competition.club_competitions cc
		       ON cc.club_id = c.id AND cc.role = 'league'
		LEFT JOIN competition.competitions ccomp ON ccomp.id = cc.competition_id
		LEFT JOIN LATERAL (
			SELECT pe.display_name, p.market_value::bigint AS mv
			FROM player.players p
			JOIN person.people pe ON pe.id = p.person_id
			WHERE p.club_id = c.id
			ORDER BY p.market_value DESC NULLS LAST
			LIMIT 1
		) top ON TRUE
		WHERE wc.id = $1
		ORDER BY c.name`, countryID)
	if err != nil {
		return nil, fmt.Errorf("admin: clubs: %w", err)
	}
	defer rows.Close()

	panel := &ClubsPanels{Country: country, Clubs: []ClubRow{}}
	for rows.Next() {
		var r ClubRow
		if err := rows.Scan(&r.ID, &r.Name, &r.ShortName, &r.IsAIControlled, &r.Tier,
			&r.LeagueID, &r.LeagueName, &r.LeagueTier,
			&r.SquadSize, &r.WageBill, &r.CrisisStage, &r.TopPlayerName, &r.TopPlayerMarket); err != nil {
			return nil, fmt.Errorf("admin: scan club: %w", err)
		}
		if r.CrisisStage != nil && *r.CrisisStage == "" {
			r.CrisisStage = nil
		}
		if r.LeagueID != nil {
			r.League = &apiref.LeagueRef{ID: *r.LeagueID, Name: r.LeagueName}
		}
		panel.Clubs = append(panel.Clubs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate clubs: %w", err)
	}

	// Budget envelopes (latest season per club+type) + cash, batch joined.
	budgets, err := s.clubBudgets(ctx, clubIDs)
	if err != nil {
		return nil, err
	}
	cash, err := s.clubCash(ctx, clubIDs)
	if err != nil {
		return nil, err
	}
	for i := range panel.Clubs {
		if b, ok := budgets[panel.Clubs[i].ID]; ok {
			panel.Clubs[i].WageAllocated = b.WageAllocated
			panel.Clubs[i].WageCommitted = b.WageCommitted
			panel.Clubs[i].TransferAllocated = b.TransferAllocated
			panel.Clubs[i].TransferCommitted = b.TransferCommitted
		}
		panel.Clubs[i].Cash = cash[panel.Clubs[i].ID]
	}
	return panel, nil
}

type clubBudgetLine struct {
	WageAllocated, WageCommitted, TransferAllocated, TransferCommitted int64
}

func (s *Service) clubBudgets(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID]clubBudgetLine, error) {
	out := map[uuid.UUID]clubBudgetLine{}
	rows, err := s.pool.Query(ctx, `
		SELECT b.club_id, b.budget_type, b.allocated_amount::bigint, b.committed_amount::bigint
		FROM (
			SELECT DISTINCT ON (b.club_id, b.budget_type) b.club_id, b.budget_type,
			       b.allocated_amount, b.committed_amount
			FROM finance.budgets b
			WHERE b.club_id = ANY($1)
			ORDER BY b.club_id, b.budget_type, b.season DESC
		) b`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("admin: club budgets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var typ string
		var alloc, comm int64
		if err := rows.Scan(&id, &typ, &alloc, &comm); err != nil {
			return nil, fmt.Errorf("admin: scan budget: %w", err)
		}
		line := out[id]
		if typ == "wage" {
			line.WageAllocated, line.WageCommitted = alloc, comm
		} else {
			line.TransferAllocated, line.TransferCommitted = alloc, comm
		}
		out[id] = line
	}
	return out, rows.Err()
}

func (s *Service) clubCash(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	rows, err := s.pool.Query(ctx, `
		SELECT a.club_id,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'credit')::bigint, 0)
		         - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'debit')::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = ANY($1)
		GROUP BY a.club_id`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("admin: club cash: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var cash int64
		if err := rows.Scan(&id, &cash); err != nil {
			return nil, fmt.Errorf("admin: scan cash: %w", err)
		}
		out[id] = cash
	}
	return out, rows.Err()
}
