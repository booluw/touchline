package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// offSeasonTicks returns the number of daily world ticks the league's next
// season is anchored after the previous one ends. Read at rollover so the value
// is stable with the season-end cascade. Precedence: per-league override
// (competition_rules.scheduling_rules->>'off_season_ticks') → world config
// (season.off_season_ticks) → DefaultOffSeasonTicks.
func (s *Service) offSeasonTicks(ctx context.Context, tx pgx.Tx, leagueID, worldID uuid.UUID) (int, error) {
	var override string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(scheduling_rules->>'off_season_ticks', '')
		FROM competition.competition_rules
		WHERE competition_id = $1`, leagueID).Scan(&override)
	if err != nil {
		return 0, fmt.Errorf("off-season ticks: override: %w", err)
	}
	if v, parseErr := strconv.Atoi(override); parseErr == nil && v > 0 {
		return v, nil
	}

	var raw json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT config_value FROM world.world_config
		WHERE world_id = $1 AND config_key = 'season.off_season_ticks'`, worldID).Scan(&raw)
	if err == nil {
		var v int
		if unmarshalErr := json.Unmarshal(raw, &v); unmarshalErr == nil && v > 0 {
			return v, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("off-season ticks: world config: %w", err)
	}
	return DefaultOffSeasonTicks, nil
}

// ActivateDueSeasons flips every rollover-created 'upcoming' season to
// 'in_progress' (and records SEASON_STARTED) once the world calendar has
// reached its first scheduled fixture. Season #1 is created 'in_progress'
// directly and is never touched here. It runs on the daily tick before
// KickoffDue so a season reads 'in_progress' the moment its first matchday
// kicks. Idempotent: the status guard confines each season to a single flip.
func (s *Service) ActivateDueSeasons(ctx context.Context, worldID uuid.UUID) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin activate-seasons tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var asOf time.Time
	err = tx.QueryRow(ctx, `
		SELECT date_trunc('day', COALESCE(launched_at, created_at)) + make_interval(days => current_day::int)
		FROM world.worlds
		WHERE id = $1`, worldID).Scan(&asOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrWorldNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("activate seasons: world date: %w", err)
	}

	rows, err := tx.Query(ctx, `
		UPDATE competition.seasons s
		SET status = 'in_progress'
		WHERE s.world_id = $1 AND s.status = 'upcoming'
		  AND EXISTS (
			SELECT 1 FROM match.fixtures f
			WHERE f.competition_id = s.competition_id AND f.world_id = s.world_id
			  AND f.status <> 'cancelled' AND f.scheduled_at::date <= $2::date)
		RETURNING s.id, s.competition_id, s.season_number, s.season_label`,
		worldID, asOf)
	if err != nil {
		return 0, fmt.Errorf("activate due seasons: %w", err)
	}
	type activated struct {
		seasonID      uuid.UUID
		competitionID uuid.UUID
		number        int
		label         string
	}
	seasons := []activated{}
	for rows.Next() {
		var a activated
		if err := rows.Scan(&a.seasonID, &a.competitionID, &a.number, &a.label); err != nil {
			rows.Close()
			return 0, fmt.Errorf("activate due seasons: scan: %w", err)
		}
		seasons = append(seasons, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("activate due seasons: iterate: %w", err)
	}

	for _, a := range seasons {
		if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
			WorldID:   worldID,
			EventType: "SEASON_STARTED",
			Payload: mustJSON(map[string]any{
				"season_id":      a.seasonID,
				"competition_id": a.competitionID,
				"season_number":  a.number,
				"season_label":   a.label,
			}),
		}); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit activate seasons: %w", err)
	}
	return len(seasons), nil
}
