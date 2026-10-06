package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
)

// ---------------------------------------------------------------------------
// Create / read
// ---------------------------------------------------------------------------

// CreateCup declares a domestic cup under a country. Only the staging
// variables are checked here (N >= 0, X >= 1, X+N >= 2); the field-dependent
// checks run at campaign time when the bottom pool size is known.
func (s *Service) CreateCup(ctx context.Context, p CupParams) (*Cup, error) {
	name := trimSpace(p.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := cupStagingBaseValid(p.FirstTierBye, p.SurvivorThreshold); err != nil {
		return nil, err
	}
	policy, err := normalizeFinalDatePolicy(p.FinalDatePolicy)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create cup: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT world_id FROM world.countries WHERE id = $1`, p.CountryID).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}
	if worldID != p.WorldID {
		return nil, ErrCompetitionWorldMismatch
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.competitions
			WHERE country_id = $1 AND lower(name) = lower($2))`,
		p.CountryID, name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check cup name: %w", err)
	}
	if exists {
		return nil, ErrNameCollision
	}

	rules, err := cupRulesJSON(p.SchedulingRules)
	if err != nil {
		return nil, err
	}
	qual, err := cupQualificationJSON(p.FirstTierBye, p.SurvivorThreshold)
	if err != nil {
		return nil, err
	}

	var cupID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.competitions
			(world_id, country_id, name, competition_type, prize_pool, status,
			 final_date_mode, final_date, final_offset_days)
		VALUES ($1, $2, $3, 'domestic_cup', $4, 'active', $5, $6, $7)
		RETURNING id`, worldID, p.CountryID, name, p.PrizePool,
		policy.FinalDateMode, finalDateParam(policy.FinalDate), policy.FinalOffsetDays).Scan(&cupID); err != nil {
		return nil, fmt.Errorf("insert cup: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO competition.competition_rules
			(competition_id, format, qualification_rules, is_home_and_away, scheduling_rules)
		VALUES ($1, 'knockout', $2, FALSE, $3)`,
		cupID, qual, rules); err != nil {
		return nil, fmt.Errorf("insert cup rules: %w", err)
	}

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if err := s.recordAdminEvent(ctx, tx, worldID, EventCupCreated, map[string]any{
		"cup_id": cupID, "name": name, "competition_type": "domestic_cup", "country_id": p.CountryID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create cup: %w", err)
	}
	return cup, nil
}

// ListCups returns the world's knockout cups (country + regional) ordered by
// name. It reads the staging variables back from qualification_rules.
func (s *Service) ListCups(ctx context.Context, worldID uuid.UUID) ([]Cup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.region_id, c.tier, c.name, c.competition_type, c.status, c.prize_pool,
		       c.final_date_mode, c.final_date, c.final_offset_days,
		       wc.name, wc.code, wr.name, r.format, r.is_home_and_away,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'teams', '0')::int,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'enter_when_survivors', '0')::int
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		LEFT JOIN world.countries wc ON wc.id = c.country_id
		LEFT JOIN world.regions wr ON wr.id = c.region_id
		WHERE c.world_id = $1 AND c.competition_type IN ('domestic_cup', 'continental')
		ORDER BY c.name`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list cups: %w", err)
	}
	defer rows.Close()
	out := []Cup{}
	for rows.Next() {
		cup, err := scanCup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *cup)
	}
	return out, rows.Err()
}

// GetCup returns the manager cup view for a world-scoped cup.
func (s *Service) GetCup(ctx context.Context, worldID uuid.UUID, cupID uuid.UUID) (*CupCampaign, error) {
	cup, err := s.getCup(ctx, s.pool, cupID)
	if err != nil {
		return nil, err
	}
	if cup.WorldID != worldID {
		return nil, ErrCompetitionNotFound
	}

	var qual json.RawMessage
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(qualification_rules, '{}'::jsonb)
		FROM competition.competition_rules WHERE competition_id = $1`, cupID).Scan(&qual)
	if err != nil {
		return nil, fmt.Errorf("cup rules: %w", err)
	}

	var plan cupPlan
	if raw, ok := planFromQual(qual); ok {
		plan = raw
	}

	cam := &CupCampaign{
		Cup:                *cup,
		QualificationRules: qual,
		LateEntryRound:     plan.LateEntry,
		TotalRounds:        plan.Total,
		Rounds:             []CupRound{},
	}

	season, err := s.activeSeason(ctx, s.pool, cupID, worldID)
	if errors.Is(err, ErrNoSeason) {
		return cam, nil
	}
	if err != nil {
		return nil, err
	}
	cam.Season = season

	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name,
		       (SELECT m.id FROM match.matches m WHERE m.fixture_id = f.id AND m.status = 'completed')
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status <> 'cancelled'
		ORDER BY f.matchday, f.scheduled_at, f.home_club_id`, cupID, worldID)
	if err != nil {
		return nil, fmt.Errorf("cup fixtures: %w", err)
	}
	fixtures, err := scanFixtures(rows)
	if err != nil {
		return nil, err
	}

	byes, err := s.cupByes(ctx, season.ID, 0)
	if err != nil {
		return nil, err
	}
	for _, f := range fixtures {
		tie := CupTie{
			ID: f.ID, Home: f.HomeClub, Away: f.AwayClub,
			ScheduledAt: f.ScheduledAt, Status: f.Status,
			HomeScore: f.HomeScore, AwayScore: f.AwayScore,
		}
		if f.Status == "completed" && f.HomeScore != nil && f.AwayScore != nil &&
			*f.HomeScore != *f.AwayScore {
			if *f.HomeScore > *f.AwayScore {
				tie.Winner = &f.HomeClub
			} else {
				tie.Winner = &f.AwayClub
			}
		}
		if len(cam.Rounds) > 0 && cam.Rounds[len(cam.Rounds)-1].Round == f.Matchday {
			cam.Rounds[len(cam.Rounds)-1].Ties = append(cam.Rounds[len(cam.Rounds)-1].Ties, tie)
			continue
		}
		rb := byes[f.Matchday]
		rnd := CupRound{Round: f.Matchday, Ties: []CupTie{tie}, Byes: rb}
		cam.Rounds = append(cam.Rounds, rnd)
	}

	var championID *uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		SELECT club_id FROM competition.competition_entries
		WHERE season_id = $1 AND status = 'champion' LIMIT 1`, season.ID).Scan(&championID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("cup champion: %w", err)
		}
	}
	if championID != nil {
		var name string
		var short string
		if err := s.pool.QueryRow(ctx,
			`SELECT name, COALESCE(short_name, '') FROM club.clubs WHERE id = $1`, *championID).
			Scan(&name, &short); err != nil {
			return nil, fmt.Errorf("cup champion club: %w", err)
		}
		cam.Champion = &apiref.ClubRef{ID: *championID, Name: name, Short: short}
	}
	return cam, nil
}

// getCup loads one cup competition joined with its scope (country and/or
// region) and rules. It serves both country-scoped and regional cups.
func (s *Service) getCup(ctx context.Context, q rowQueryer, cupID uuid.UUID) (*Cup, error) {
	cup, err := scanCup(q.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.region_id, c.tier, c.name, c.competition_type, c.status, c.prize_pool,
		       c.final_date_mode, c.final_date, c.final_offset_days,
		       wc.name, wc.code, wr.name, r.format, r.is_home_and_away,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'teams', '0')::int,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'enter_when_survivors', '0')::int
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		LEFT JOIN world.countries wc ON wc.id = c.country_id
		LEFT JOIN world.regions wr ON wr.id = c.region_id
		WHERE c.id = $1 AND c.competition_type IN ('domestic_cup', 'continental')`, cupID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompetitionNotFound
	}
	if err != nil {
		return nil, err
	}
	return cup, nil
}

// cupRow is the shared SELECT column shape for Cup reads (ListCups/getCup).
type cupRow interface {
	Scan(dest ...any) error
}

// scanCup decodes one cup row with nullable country/region/tier.
func scanCup(row cupRow) (*Cup, error) {
	var (
		cup                                  Cup
		countryID, regionID                  *uuid.UUID
		countryName, countryCode, regionName *string
		tier                                 *int
		finalDate                            *time.Time
		offset                               int
	)
	if err := row.Scan(&cup.ID, &cup.WorldID, &countryID, &regionID, &tier, &cup.Name,
		&cup.CompetitionType, &cup.Status, &cup.PrizePool,
		&cup.FinalDateMode, &finalDate, &offset,
		&countryName, &countryCode, &regionName, &cup.Format, &cup.IsHomeAndAway,
		&cup.FirstTierBye, &cup.SurvivorThreshold); err != nil {
		return nil, fmt.Errorf("scan cup: %w", err)
	}
	cup.FinalOffsetDays = &offset
	if finalDate != nil {
		fd := finalDate.Format("2006-01-02")
		cup.FinalDate = &fd
	}
	if countryID != nil {
		name, code := "", ""
		if countryName != nil {
			name = *countryName
		}
		if countryCode != nil {
			code = *countryCode
		}
		cup.Country = &apiref.CountryRef{ID: *countryID, Name: name, Code: code}
	}
	if regionID != nil {
		name := ""
		if regionName != nil {
			name = *regionName
		}
		cup.Region = &RegionRef{ID: *regionID, Name: name}
	}
	cup.Tier = tier
	return &cup, nil
}

// cupByes returns, per round, the clubs that advanced on a bye (is_bye rows).
// A round of 0 returns all byes of the season (used by the campaign view).
func (s *Service) cupByes(ctx context.Context, seasonID uuid.UUID, round int) (map[int][]apiref.ClubRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cb.round, cb.club_id, cl.name, COALESCE(cl.short_name, '')
		FROM competition.cup_bracket cb
		JOIN club.clubs cl ON cl.id = cb.club_id
		WHERE cb.season_id = $1 AND ($2 = 0 OR cb.round = $2) AND cb.is_bye
		ORDER BY cb.round, cb.seed`, seasonID, round)
	if err != nil {
		return nil, fmt.Errorf("cup byes: %w", err)
	}
	defer rows.Close()
	out := map[int][]apiref.ClubRef{}
	for rows.Next() {
		var r int
		var ref apiref.ClubRef
		if err := rows.Scan(&r, &ref.ID, &ref.Name, &ref.Short); err != nil {
			return nil, fmt.Errorf("scan cup bye: %w", err)
		}
		out[r] = append(out[r], ref)
	}
	return out, rows.Err()
}
