// Package player owns player reads (S03-01). Generated players are persisted
// by the bootstrap orchestrator; attributes/personality/emotional-state rows
// are absent until their owning sprints, so those reads return a zero-value
// record rather than 404.
package player

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPlayerNotFound = errors.New("player not found")

// Service reads player state.
type Service struct {
	pool *pgxpool.Pool
}

// NewService builds the player read service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// GetPlayer fetches a player's identity joined to person.people.
func (s *Service) GetPlayer(ctx context.Context, id uuid.UUID) (*Player, error) {
	var p Player
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.world_id, p.club_id, p.person_id, p.primary_position, p.squad_number,
		       pe.first_name, pe.last_name, pe.display_name, pe.date_of_birth::text, pe.nationality_code
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		WHERE p.id = $1`, id,
	).Scan(&p.ID, &p.WorldID, &p.ClubID, &p.PersonID, &p.PrimaryPosition, &p.SquadNumber,
		&p.FirstName, &p.LastName, &p.DisplayName, &p.DateOfBirth, &p.Nationality)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPlayerAttributes returns the zero-value record until the attribute model
// lands (S05).
func (s *Service) GetPlayerAttributes(ctx context.Context, playerID uuid.UUID) (*PlayerAttributes, error) {
	return &PlayerAttributes{PlayerID: playerID}, nil
}

// GetPlayerPersonality returns the zero-value record until S09-01.
func (s *Service) GetPlayerPersonality(ctx context.Context, playerID uuid.UUID) (*PlayerPersonality, error) {
	return &PlayerPersonality{PlayerID: playerID}, nil
}

// GetEmotionalState returns the zero-value record until the morale/playing-
// time work lands (S06-03).
func (s *Service) GetEmotionalState(ctx context.Context, playerID uuid.UUID) (*EmotionalState, error) {
	return &EmotionalState{PlayerID: playerID}, nil
}
