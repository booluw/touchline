package player

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/development"
	"github.com/touchline/backend/pkg/explanation"
)

// DevelopmentEventType is the S08-02 weekly stamp whose payload carries each
// evaluated player's explanation (kept in sync with internal/training's
// EventDevelopmentWeek).
const DevelopmentEventType = "DEVELOPMENT_WEEK"

// PlayerDevelopmentDetail is the manager-facing S08-02 development picture of
// one player (GET /api/clubs/:id/players/:playerID/development). Only the
// observable trajectory is surfaced: evaluated weeks, stagnation and recent
// attribute movement plus the recorded drivers. The hidden potential ceiling
// (player_hidden_traits.potential, expansion budget, lock state) is never
// exposed — a manager sees the trajectory, not the headroom.
type PlayerDevelopmentDetail struct {
	PlayerID                 uuid.UUID                `json:"player_id"`
	FirstName                string                   `json:"first_name"`
	LastName                 string                   `json:"last_name"`
	DisplayName              string                   `json:"display_name"`
	Position                 string                   `json:"position"`
	LastEvaluatedWeek        int64                    `json:"last_eval_week"`
	CumDevWeeks              int                      `json:"cum_dev_weeks"`
	ConsecutiveStagnantWeeks int                      `json:"consecutive_stagnant_weeks"`
	Stagnating               bool                     `json:"stagnating"`
	RecentDeltas             []AttributeDelta         `json:"recent_deltas"`
	Drivers                  *explanation.Explanation `json:"drivers"`
}

// AttributeDelta is one weekly net movement of an attribute key.
type AttributeDelta struct {
	AppliedWeek  int64  `json:"applied_week"`
	AttributeKey string `json:"attribute_key"`
	Delta        int    `json:"delta"`
}

// GetPlayerDevelopmentDetail returns one owned player's observable development
// picture. Ownership is enforced exactly like the morale read: the caller must
// manage the player's club in the same world.
func (s *Service) GetPlayerDevelopmentDetail(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*PlayerDevelopmentDetail, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	prof, err := playerProfile(ctx, s.pool, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.ClubID == nil || *prof.ClubID != clubID || prof.WorldID != worldID {
		return nil, ErrPlayerNotInClub
	}

	d := &PlayerDevelopmentDetail{
		PlayerID:     prof.ID,
		FirstName:    prof.FirstName,
		LastName:     prof.LastName,
		DisplayName:  prof.DisplayName,
		Position:     prof.PrimaryPosition,
		RecentDeltas: []AttributeDelta{},
	}

	var stamped int64
	var cum, consec int
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(last_eval_week, 0), cum_dev_weeks, consecutive_stagnant_weeks
		FROM player.player_development
		WHERE player_id = $1`, playerID).Scan(&stamped, &cum, &consec); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	d.LastEvaluatedWeek = stamped
	d.CumDevWeeks = cum
	d.ConsecutiveStagnantWeeks = consec
	d.Stagnating = consec >= development.StagnatingThreshold

	// Most recent eight weekly deltas; the morale pseudo-key row is excluded
	// (it lives in the same log as a movement for the morale read models).
	rows, err := s.pool.Query(ctx, `
		SELECT applied_week, attribute_key, delta
		FROM player.player_attribute_changes
		WHERE player_id = $1 AND attribute_key <> 'morale'
		ORDER BY applied_week DESC, attribute_key ASC
		LIMIT 8`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AttributeDelta
		if err := rows.Scan(&a.AppliedWeek, &a.AttributeKey, &a.Delta); err != nil {
			return nil, err
		}
		d.RecentDeltas = append(d.RecentDeltas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(d.RecentDeltas)-1; i < j; i, j = i+1, j-1 {
		d.RecentDeltas[i], d.RecentDeltas[j] = d.RecentDeltas[j], d.RecentDeltas[i]
	}

	d.Drivers, err = s.latestDevelopmentDrivers(ctx, worldID, playerID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// latestDevelopmentDrivers returns the player's stored explanation from the
// most recent DEVELOPMENT_WEEK event in the world, or nil when the club has
// never had a week evaluated. Explanations are stored reasons — never
// recomputed here (PRD §54).
func (s *Service) latestDevelopmentDrivers(ctx context.Context, worldID, playerID uuid.UUID) (*explanation.Explanation, error) {
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT payload::text
		FROM world.events
		WHERE world_id = $1 AND event_type = $2
		ORDER BY world_tick DESC, occurred_at DESC, id DESC
		LIMIT 1`, worldID, DevelopmentEventType).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var ev struct {
		Players []struct {
			PlayerID    string                   `json:"player_id"`
			Explanation *explanation.Explanation `json:"explanation"`
		} `json:"players"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, err
	}
	for _, p := range ev.Players {
		if p.PlayerID == playerID.String() {
			return p.Explanation, nil
		}
	}
	return nil, nil
}
