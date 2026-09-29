package admin

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
)

// ---------------------------------------------------------------------------
// Players
// ---------------------------------------------------------------------------

// PlayerSummary aggregates the country's player population.
func (s *Service) PlayerSummary(ctx context.Context, worldID, countryID uuid.UUID) (*PlayerSummary, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	ps := &PlayerSummary{Country: country,
		ByStatus: map[string]int{}, ByOrigin: map[string]int{},
		ByPosition: []PositionBreakdown{}, Nationalities: []NationalityMix{}, Intakes: []IntakeRow{}}

	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*), ROUND(AVG(EXTRACT(EPOCH FROM (age('now'::date, pe.date_of_birth))) / 31536000.0)::numeric, 1)::float8
		FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		WHERE p.world_id = $1 AND p.country_id = $2`, worldID, countryID).
		Scan(&ps.Total, &ps.AvgAge)

	ps.ByStatus = s.scanCountBy(ctx, `
		SELECT status, COUNT(*) FROM player.players
		WHERE world_id = $1 AND country_id = $2 GROUP BY status`, worldID, countryID)
	ps.ByOrigin = s.scanCountBy(ctx, `
		SELECT origin, COUNT(*) FROM player.players
		WHERE world_id = $1 AND country_id = $2 GROUP BY origin`, worldID, countryID)

	if err := s.scanPositions(ctx, worldID, countryID, ps); err != nil {
		return nil, err
	}
	if err := s.scanNationalities(ctx, worldID, countryID, ps); err != nil {
		return nil, err
	}
	s.scanIntakes(ctx, worldID, countryID, ps)
	return ps, nil
}

func (s *Service) scanCountBy(ctx context.Context, query string, args ...any) map[string]int {
	out := map[string]int{}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err == nil && key != "" {
			out[key] = n
		}
	}
	return out
}

func (s *Service) scanPositions(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) error {
	rows, err := s.pool.Query(ctx, `
		SELECT p.primary_position,
		       COUNT(*),
		       ROUND(COALESCE(AVG(a.technical), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.physical), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(a.mental), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.tactical), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(a.goalkeeping), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.positional), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(ht.potential), 0)::numeric, 1)::float8
		FROM player.players p
		LEFT JOIN LATERAL (
			SELECT AVG(value) FILTER (WHERE attribute_category = 'technical')  AS technical,
			       AVG(value) FILTER (WHERE attribute_category = 'physical')  AS physical,
			       AVG(value) FILTER (WHERE attribute_category = 'mental')    AS mental,
			       AVG(value) FILTER (WHERE attribute_category = 'tactical')  AS tactical,
			       AVG(value) FILTER (WHERE attribute_category = 'goalkeeping') AS goalkeeping,
			       AVG(value) FILTER (WHERE attribute_category = 'positional') AS positional
			FROM player.player_attributes pa WHERE pa.player_id = p.id
		) a ON TRUE
		LEFT JOIN player.player_hidden_traits ht ON ht.player_id = p.id
		WHERE p.world_id = $1 AND p.country_id = $2
		GROUP BY p.primary_position
		ORDER BY p.primary_position`, worldID, countryID)
	if err != nil {
		return fmt.Errorf("admin: positions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b PositionBreakdown
		var tech, phy, men, tac, gk, pos, pot float64
		if err := rows.Scan(&b.Position, &b.Count, &tech, &phy, &men, &tac, &gk, &pos, &pot); err != nil {
			return fmt.Errorf("admin: scan position: %w", err)
		}
		b.AvgOverall = squad.PositionalOverall(b.Position, squad.AttributeSnapshot{
			Technical:   int(math.Round(tech)),
			Physical:    int(math.Round(phy)),
			Mental:      int(math.Round(men)),
			Tactical:    int(math.Round(tac)),
			Goalkeeping: int(math.Round(gk)),
			Positional:  int(math.Round(pos)),
		})
		b.AvgPotential = int(math.Round(pot))
		ps.ByPosition = append(ps.ByPosition, b)
	}
	return rows.Err()
}

func (s *Service) scanNationalities(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) error {
	rows, err := s.pool.Query(ctx, `
		SELECT pe.nationality_code, COALESCE(n.name, pe.nationality_code), COUNT(*)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN ref.nationalities n ON n.code = pe.nationality_code
		WHERE p.world_id = $1 AND p.country_id = $2
		GROUP BY pe.nationality_code, n.name
		ORDER BY COUNT(*) DESC`, worldID, countryID)
	if err != nil {
		return fmt.Errorf("admin: nationalities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m NationalityMix
		if err := rows.Scan(&m.Code, &m.Name, &m.Count); err != nil {
			return fmt.Errorf("admin: scan nationality: %w", err)
		}
		ps.Nationalities = append(ps.Nationalities, m)
	}
	return rows.Err()
}

func (s *Service) scanIntakes(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) {
	rows, err := s.pool.Query(ctx, `
		SELECT season_number, SUM(player_count) FROM world.country_academy_intakes
		WHERE world_id = $1 AND country_id = $2
		GROUP BY season_number ORDER BY season_number`, worldID, countryID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ir IntakeRow
		if err := rows.Scan(&ir.SeasonNumber, &ir.PlayerCount); err == nil {
			ps.Intakes = append(ps.Intakes, ir)
		}
	}
}
