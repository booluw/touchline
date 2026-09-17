package training

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/development"
	"github.com/touchline/backend/pkg/explanation"
)

// EventDevelopmentWeek is the S08-02 weekly development evaluation stamp
// (system actor, one per club per applied week). Its payload carries that
// club's per-player explanation.Explanation array — the auditable §54 "why"
// behind the week's movement. The numeric attribution itself is the
// player.player_attribute_changes rows the sweep already writes.
const EventDevelopmentWeek = "DEVELOPMENT_WEEK"

// developmentContext is one player's weekly development inputs minus skills
// (the caller attaches the loaded skill map, which lives outside the reads).
type developmentContext struct {
	development.Input
	traits   hiddenTraits
	prior    devState
	hasPrior bool
}

type hiddenTraits struct {
	potential       int
	locked          bool
	professionalism int
}

type devState struct {
	evaluatedWeeks int
	consecStagnant int
	expansionsLeft int
	lockedWeek     int64
}

// loadDevelopmentContexts reads the per-player development context in four
// club-wide queries so the weekly sweep issues constant SQL per club.
func loadDevelopmentContexts(ctx context.Context, tx pgx.Tx, players []playerRow, clubID uuid.UUID, weekTick int64, ids []uuid.UUID) (map[uuid.UUID]developmentContext, error) {
	traits, err := loadHiddenTraits(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	states, err := loadDevStates(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := loadRecentRatings(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	facility, err := clubTrainingFacility(ctx, tx, clubID)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]developmentContext, len(players))
	for _, p := range players {
		tr := traits[p.id]
		if tr.professionalism == 0 {
			tr.professionalism = 50
		}
		st, hasRow := states[p.id]
		expansions := development.ExpansionBudget
		if hasRow {
			expansions = st.expansionsLeft
		}
		consec := 0
		if hasRow {
			consec = st.consecStagnant
		}
		out[p.id] = developmentContext{
			Input: development.Input{
				PlayerID:            p.id,
				Age:                 p.age,
				Position:            p.position,
				FacilityLevel:       facility,
				Potential:           tr.potential,
				Locked:              tr.locked,
				ExpansionsLeft:      expansions,
				Professionalism:     tr.professionalism,
				SeasonAvgRating:     ratings[p.id],
				ConsecutiveStagnant: consec,
				WeekTick:            weekTick,
			},
			traits:   tr,
			prior:    st,
			hasPrior: hasRow,
		}
	}
	return out, nil
}

func loadHiddenTraits(ctx context.Context, tx pgx.Tx, playerIDs []uuid.UUID) (map[uuid.UUID]hiddenTraits, error) {
	out := make(map[uuid.UUID]hiddenTraits, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT player_id, COALESCE(potential, 0), COALESCE(potential_ceiling_locked, FALSE),
		       COALESCE(professionalism, 50)
		FROM player.player_hidden_traits
		WHERE player_id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("load hidden traits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t hiddenTraits
		var pid uuid.UUID
		if err := rows.Scan(&pid, &t.potential, &t.locked, &t.professionalism); err != nil {
			return nil, fmt.Errorf("scan hidden traits: %w", err)
		}
		out[pid] = t
	}
	return out, rows.Err()
}

func loadDevStates(ctx context.Context, tx pgx.Tx, playerIDs []uuid.UUID) (map[uuid.UUID]devState, error) {
	out := make(map[uuid.UUID]devState, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT player_id, cum_dev_weeks, consecutive_stagnant_weeks,
		       potential_expansions_remaining, COALESCE(potential_locked_week, 0)
		FROM player.player_development
		WHERE player_id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("load development state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s devState
		var pid uuid.UUID
		if err := rows.Scan(&pid, &s.evaluatedWeeks, &s.consecStagnant,
			&s.expansionsLeft, &s.lockedWeek); err != nil {
			return nil, fmt.Errorf("scan development state: %w", err)
		}
		out[pid] = s
	}
	return out, rows.Err()
}

// loadRecentRatings is the mean rating over each player's last 10 rated
// appearances (ordered by match completion); players without rated feed get 0.
func loadRecentRatings(ctx context.Context, tx pgx.Tx, playerIDs []uuid.UUID) (map[uuid.UUID]float64, error) {
	out := make(map[uuid.UUID]float64, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT player_id, AVG(rating)
		FROM (
			SELECT a.player_id, a.rating::float8,
			       ROW_NUMBER() OVER (PARTITION BY a.player_id ORDER BY m.ended_at DESC NULLS LAST, a.match_id) AS rn
			FROM player.player_appearances a
			JOIN match.matches m ON m.id = a.match_id
			WHERE a.player_id = ANY($1) AND a.rating IS NOT NULL
		) recent
		WHERE rn <= 10
		GROUP BY player_id`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("load recent ratings: %w", err)
	}
	defer rows.Close()
	var pid uuid.UUID
	var avg float64
	for rows.Next() {
		if err := rows.Scan(&pid, &avg); err != nil {
			return nil, fmt.Errorf("scan recent rating: %w", err)
		}
		out[pid] = avg
	}
	return out, rows.Err()
}

// clubTrainingFacility resolves the composite development facility level 1..10
// (neutral 5 default): the better of club.academies.coaching_level and the
// youth/training-ground facility rows.
func clubTrainingFacility(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (int, error) {
	var coaching, facility int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT coaching_level FROM club.academies WHERE club_id = $1), 0),
		       COALESCE((SELECT MAX(level) FROM club.facilities
		                 WHERE club_id = $1 AND facility_type IN ('training_ground', 'youth_facility')), 0)`,
		clubID).Scan(&coaching, &facility)
	if err != nil {
		return 5, fmt.Errorf("facility: %w", err)
	}
	if coaching > facility {
		facility = coaching
	}
	if facility == 0 {
		return 5, nil
	}
	if facility > 10 {
		return 10, nil
	}
	return facility, nil
}

// devWeekPlayer is one audited row of the club's weekly DEVELOPMENT_WEEK
// payload.
type devWeekPlayer struct {
	PlayerID    string                   `json:"player_id"`
	Explanation *explanation.Explanation `json:"explanation"`
}

type devWeekPayload struct {
	ClubID    string          `json:"club_id"`
	Archetype string          `json:"archetype"`
	Players   []devWeekPlayer `json:"players"`
}

// recordDevelopment writes one player's development outcome: the potential
// ceiling delta on player_hidden_traits (only when it changed or locked), the
// player.player_development state row (upserted; the first lock week wins on
// redelivery), and the auditable explanation for the club's weekly event.
func (s *Service) recordDevelopment(ctx context.Context, tx pgx.Tx, p playerRow, dc developmentContext, out development.Outcome) error {
	if out.PotentialChanged || (out.Locked && !dc.traits.locked) {
		if _, err := tx.Exec(ctx, `
			UPDATE player.player_hidden_traits
			SET potential = $2, potential_ceiling_locked = $3
			WHERE player_id = $1`, p.id, out.Potential, out.Locked); err != nil {
			return fmt.Errorf("potential: %w", err)
		}
	}

	var lockedWeek *int64
	if out.LockedWeek != 0 {
		week := out.LockedWeek
		lockedWeek = &week
	}
	cum := 1
	if dc.hasPrior {
		cum = dc.prior.evaluatedWeeks + 1
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO player.player_development
			(player_id, last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks,
			 potential_expansions_remaining, potential_locked_week, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (player_id) DO UPDATE SET
			last_eval_week = EXCLUDED.last_eval_week,
			cum_dev_weeks = EXCLUDED.cum_dev_weeks,
			consecutive_stagnant_weeks = EXCLUDED.consecutive_stagnant_weeks,
			potential_expansions_remaining = EXCLUDED.potential_expansions_remaining,
			potential_locked_week = COALESCE(player.player_development.potential_locked_week, EXCLUDED.potential_locked_week),
			updated_at = now()`,
		p.id, out.State.LastEvaluatedWeek, cum, out.State.ConsecutiveStagnant,
		out.State.ExpansionsLeft, lockedWeek); err != nil {
		return fmt.Errorf("development state: %w", err)
	}
	return nil
}

// emitDevelopmentWeek writes the club's weekly DEVELOPMENT_WEEK system event
// with every evaluated player's explanation in payload.
func (s *Service) emitDevelopmentWeek(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, weekTick int64, clubID uuid.UUID, archetype string, players []devWeekPlayer) error {
	if len(players) == 0 {
		return nil
	}
	payload, _ := json.Marshal(devWeekPayload{
		ClubID:    clubID.String(),
		Archetype: archetype,
		Players:   players,
	})
	return s.recordSystemEvent(ctx, tx, worldID, weekTick, EventDevelopmentWeek, payload)
}