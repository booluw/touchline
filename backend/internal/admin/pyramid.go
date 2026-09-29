package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Pyramid
// ---------------------------------------------------------------------------

// Pyramid returns the country's leagues ordered by tier then name, each with
// its latest season (if any) and registered club count.
func (s *Service) Pyramid(ctx context.Context, worldID, countryID uuid.UUID) (*Pyramid, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations,
		       (SELECT COUNT(*) FROM competition.club_competitions cc
		         WHERE cc.competition_id = c.id AND cc.role = 'league')
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.world_id = $1 AND c.country_id = $2 AND c.competition_type = 'league'
		ORDER BY c.tier, c.name`, worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("admin: pyramid: %w", err)
	}
	defer rows.Close()

	py := &Pyramid{Country: country, Leagues: []LeagueRow{}}
	for rows.Next() {
		var l LeagueRow
		if err := rows.Scan(&l.ID, &l.Name, &l.Tier, &l.TeamCount, &l.Status,
			&l.Promotions, &l.Relegations, &l.ClubCount); err != nil {
			return nil, fmt.Errorf("admin: scan pyramid: %w", err)
		}
		py.Leagues = append(py.Leagues, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate pyramid: %w", err)
	}

	// Attach each league's latest (non-completed first, else latest) season.
	for i := range py.Leagues {
		py.Leagues[i].Season = s.latestSeason(ctx, py.Leagues[i].ID)
	}
	return py, nil
}

func (s *Service) latestSeason(ctx context.Context, competitionID uuid.UUID) *SeasonRef {
	var sr SeasonRef
	err := s.pool.QueryRow(ctx, `
		SELECT id, season_label, season_number, status
		FROM competition.seasons
		WHERE competition_id = $1
		ORDER BY (status = 'completed') ASC, season_number DESC LIMIT 1`, competitionID).
		Scan(&sr.SeasonID, &sr.SeasonLabel, &sr.SeasonNumber, &sr.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return &sr
}
