package competition

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
)

// validateAdjacency enforces the country-wide promotion/relegation symmetry
// that keeps every league at its admin-declared team_count across seasons:
// for every upward edge L->L.promotes_to the league above must relegate
// exactly as many as L promotes, and the downward edge must match in turn.
func validateAdjacency(leagues []League) error {
	byID := make(map[uuid.UUID]*League, len(leagues))
	for i := range leagues {
		byID[leagues[i].ID] = &leagues[i]
	}
	for i := range leagues {
		l := &leagues[i]
		if l.Promotions > 0 && l.PromotesTo == nil {
			return ErrAdjacencyMismatch
		}
		if l.Relegations > 0 && l.RelegatesTo == nil {
			return ErrAdjacencyMismatch
		}
	}
	for i := range leagues {
		l := &leagues[i]
		if l.PromotesTo != nil {
			up, ok := byID[l.PromotesTo.ID]
			if !ok {
				return ErrBadAdjacency
			}
			if up.Relegations != l.Promotions {
				return fmt.Errorf("%w: %s promotes %d, above relegates %d",
					ErrAdjacencyMismatch, l.Name, l.Promotions, up.Relegations)
			}
		}
		if l.RelegatesTo != nil {
			down, ok := byID[l.RelegatesTo.ID]
			if !ok {
				return ErrBadAdjacency
			}
			if down.Promotions != l.Relegations {
				return fmt.Errorf("%w: %s relegates %d, below promotes %d",
					ErrAdjacencyMismatch, l.Name, l.Relegations, down.Promotions)
			}
		}
	}
	return nil
}

// scanLeagues drains a competition/complexity rows stream into League values,
// joining competitions.competition_rules, the promotion/relegation target
// leagues, and the owning country. It always closes rows.
func scanLeagues(rows pgx.Rows) ([]League, error) {
	defer rows.Close()
	out := []League{}
	for rows.Next() {
		var l League
		var ptID, rtID *uuid.UUID
		var ptName, rtName, countryName, countryCode string
		if err := rows.Scan(&l.ID, &l.WorldID, &l.Country.ID, &l.Name, &l.Tier, &l.TeamCount, &l.Reputation, &l.Status,
			&l.Promotions, &l.Relegations, &ptID, &rtID, &ptName, &rtName, &countryName, &countryCode); err != nil {
			return nil, fmt.Errorf("scan league: %w", err)
		}
		l.Country.Name = countryName
		l.Country.Code = countryCode
		if ptID != nil {
			l.PromotesTo = &apiref.LeagueRef{ID: *ptID, Name: ptName}
		}
		if rtID != nil {
			l.RelegatesTo = &apiref.LeagueRef{ID: *rtID, Name: rtName}
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
