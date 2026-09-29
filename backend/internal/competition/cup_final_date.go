package competition

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Final-date policy (IM10)
// ---------------------------------------------------------------------------

// regionCountries returns the countries of the region a continental cup scopes
// to (the league-day union set its calendar anchors on).
func (s *Service) regionCountries(ctx context.Context, tx pgx.Tx, regionID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM world.countries WHERE region_id = $1`, regionID)
	if err != nil {
		return nil, fmt.Errorf("region countries: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan region country: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// cupScopeLeagueDays returns the league-day union a cup's calendar anchors to
// (IM08): a country's league days for domestic cups; the region-wide union for
// continental cups.
func (s *Service) cupScopeLeagueDays(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, cup *Cup) ([]time.Time, error) {
	if cup.Region != nil {
		countries, err := s.regionCountries(ctx, tx, cup.Region.ID)
		if err != nil {
			return nil, err
		}
		return s.unionLeagueDays(ctx, tx, worldID, countries)
	}
	return s.countryLeagueDays(ctx, tx, worldID, cup.Country.ID)
}

// SetCupFinalDate edits a cup's final-date policy (IM10). It updates the
// declaration for future seasons and, when the cup has a live campaign,
// re-stamps only the rounds that have not materialized yet — the fixture
// history never moves. On a 'fixed' cup only final_date may be edited (offset
// edits are rejected). On a 'calculated' cup final_offset_days updates the
// declaration and clears any override, re-deriving from the live league
// calendar; a final_date stamps a per-campaign override onto the live ladder,
// and final_date:null clears it. Any edit that lands on or behind an
// already-scheduled round is rejected with ErrCupFinalDateLocked. A moved
// campaign final publishes a scheduling story. Returns the refreshed cup.
func (s *Service) SetCupFinalDate(ctx context.Context, cupID uuid.UUID, finalDate *string, finalDateSet bool, finalOffsetDays *int) (*Cup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cup final-date edit: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}

	var parsedFinalDate *time.Time
	if finalDateSet && finalDate != nil {
		d, err := parseDateOnly(*finalDate)
		if err != nil {
			return nil, fmt.Errorf("%w: final_date must be a calendar date (YYYY-MM-DD)", ErrCupFinalDateInvalid)
		}
		d = daysTruncate(d)
		parsedFinalDate = &d
	}

	newMode := cup.FinalDateMode
	newDeclDate := cup.FinalDate
	newDeclOffset := cup.FinalOffsetDays

	if newMode != cupFinalModeFixed {
		// 'calculated' tolerates a missing/invalid mode string by defaulting.
		newMode = cupFinalModeCalculated
	}
	switch newMode {
	case cupFinalModeFixed:
		if finalOffsetDays != nil {
			return nil, fmt.Errorf("%w: fixed cups ignore offsets", ErrCupFinalDateInvalid)
		}
		if parsedFinalDate == nil {
			return nil, fmt.Errorf("%w: fixed cups require a final_date", ErrCupFinalDateInvalid)
		}
		d := parsedFinalDate.Format("2006-01-02")
		newDeclDate = &d
	default: // calculated
		if finalOffsetDays != nil {
			if *finalOffsetDays < 0 {
				return nil, fmt.Errorf("%w: final_offset_days must be >= 0", ErrCupFinalDateInvalid)
			}
			newDeclOffset = finalOffsetDays
		}
	}

	// Load the live campaign plan (nil unless a campaign exists).
	var qual json.RawMessage
	if err := tx.QueryRow(ctx,
		`SELECT qualification_rules FROM competition.competition_rules WHERE competition_id = $1`, cupID).Scan(&qual); err != nil {
		return nil, fmt.Errorf("load cup rules: %w", err)
	}
	plan, hasPlan := planFromQual(qual)

	// How much of the bracket is already played: the highest materialized
	// matchday and its latest fixture date (the hard, never-movable history).
	var frontierRound int
	var frontierDate *time.Time
	if hasPlan {
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(matchday), 0), MAX(scheduled_at)::date
			FROM match.fixtures WHERE competition_id = $1 AND status <> 'cancelled'`, cupID).
			Scan(&frontierRound, &frontierDate); err != nil {
			return nil, fmt.Errorf("cup frontier: %w", err)
		}
		if len(plan.Ladder) > 0 && frontierRound >= len(plan.Ladder) {
			return nil, ErrCupFinalDateLocked
		}
	}

	var ladder []roundPlan
	if hasPlan {
		ladder = plan.Ladder
		k := len(ladder)
		if k == 0 {
			hasPlan = false
		} else if newMode == cupFinalModeFixed || newMode == cupFinalModeCalculated {
			var newFinal *time.Time
			switch {
			case newMode == cupFinalModeFixed:
				newFinal = parsedFinalDate
				d := parsedFinalDate.Format("2006-01-02")
				newDeclDate = &d
			case finalDateSet:
				// Calculated + a body date: per-campaign override only (the
				// declaration does not change mode or gain a date). An explicit
				// null leaves newFinal nil and re-derives below (clears it).
				newFinal = parsedFinalDate
			}

			leagueDays, err := s.cupScopeLeagueDays(ctx, tx, cup.WorldID, cup)
			if err != nil {
				return nil, err
			}
			p, err := s.scheduleParams(ctx, tx, cupID, cup.WorldID)
			if err != nil {
				return nil, err
			}
			weekdays := p.allowedWeekdays

			if newFinal == nil {
				// Re-derive (or clear): the override dies, the declaration
				// offset rules again.
				if derived, ok := resolveCupFinalDate(newMode, newFinal, *newDeclOffset, leagueDays, weekdays); ok {
					newFinal = &derived
				}
			}

			if newFinal != nil {
				// Reject edits that land on/behind the materialized history or
				// when the final is already played.
				if frontierDate != nil && !newFinal.After(*frontierDate) {
					return nil, ErrCupFinalDateLocked
				}
				var seed int64
				if err := tx.QueryRow(ctx,
					`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, cup.WorldID).Scan(&seed); err != nil {
					return nil, fmt.Errorf("load world seed: %w", err)
				}
				ladder[k-1].Date = newFinal
				for idx := k - 2; idx >= frontierRound; idx-- {
					next := *ladder[idx+1].Date
					round := idx + 1 // 1-based round
					target := next.AddDate(0, 0, -cupGap(seed, cupID, round, k))
					date := findCupRoundDay(target, next, leagueDays, weekdays)
					ladder[idx].Date = &date
				}
				// The earliest re-stamped round must still clear the frontier.
				if frontierDate != nil {
					earliest := ladder[frontierRound].Date
					if earliest == nil || !earliest.After(*frontierDate) {
						return nil, ErrCupFinalDateLocked
					}
				}
			} else {
				// Nothing to anchor against: un-stamp the unfrozen tail so
				// those rounds fall back to the legacy weekly pace.
				for idx := k - 1; idx >= frontierRound && idx >= 0; idx-- {
					ladder[idx].Date = nil
				}
			}
		}
	}

	moved := 0
	if hasPlan {
		for idx := frontierRound; idx < len(ladder); idx++ {
			a, b := plan.Ladder[idx].Date, ladder[idx].Date
			if (a == nil) != (b == nil) || (a != nil && !a.Equal(*b)) {
				moved++
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE competition.competitions
		SET final_date_mode = $2, final_date = $3, final_offset_days = $4
		WHERE id = $1`,
		cupID, newMode, finalDateParam(newDeclDate), newDeclOffset); err != nil {
		return nil, fmt.Errorf("update cup final-date policy: %w", err)
	}

	if moved > 0 {
		ladderJSON, err := json.Marshal(ladder)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE competition.competition_rules
			SET qualification_rules = jsonb_set(qualification_rules, '{campaign,ladder}', $2::jsonb)
			WHERE competition_id = $1`, cupID, ladderJSON); err != nil {
			return nil, fmt.Errorf("persist cup ladder: %w", err)
		}
		if final := ladder[len(ladder)-1].Date; final != nil {
			name, err := s.competitionName(ctx, tx, cupID)
			if err != nil {
				return nil, err
			}
			if err := s.publishSchedulingNews(ctx, tx, cup.WorldID, cupID,
				fmt.Sprintf("%s: final date set", name),
				fmt.Sprintf("The %s final now takes place on %s.", name, final.Format("Mon 2 Jan 2006"))); err != nil {
				return nil, err
			}
		}
	}

	if err := s.recordAdminEvent(ctx, tx, cup.WorldID, EventCupFinalDatePolicySet, map[string]any{
		"cup_id": cupID, "final_date_mode": newMode, "final_date": newDeclDate,
		"final_offset_days": newDeclOffset, "rounds_moved": moved,
	}); err != nil {
		return nil, err
	}

	cup, err = s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cup final-date edit: %w", err)
	}
	return cup, nil
}
