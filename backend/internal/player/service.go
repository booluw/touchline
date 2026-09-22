// Package player owns player state: identity reads (S03-01), and from S06-03 the
// morale / playing-time / transfer-request engine. Generated players are
// persisted by the bootstrap orchestrator; morale rows are lazily upserted as
// matches and weekly ticks touch each player, so reads default to a neutral
// record rather than 404.
package player

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
)

var (
	ErrPlayerNotFound   = errors.New("player not found")
	ErrManagerHasNoClub = errors.New("manager is not in charge of any club")
	ErrPlayerNotInClub  = errors.New("player is not in the caller's club")
	ErrRequestNotFound  = errors.New("transfer request not found")
	ErrRequestResolved  = errors.New("transfer request already resolved")
	ErrOpenRequest      = errors.New("player already has an open transfer request")
	ErrRequestCooldown  = errors.New("player is not ready to request a transfer yet")
)

type Service struct {
	pool      *pgxpool.Pool
	bus       eventbus.Publisher
	transfers *transfer.Service
}

// NewService wires the player engine onto a pool, the event bus (may be nil in
// tests) and the transfer service used to turn an approved transfer request
// into a market listing.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher, transfers *transfer.Service) *Service {
	return &Service{pool: pool, bus: bus, transfers: transfers}
}

// GetPlayer fetches a player's identity joined to person.people.
func (s *Service) GetPlayer(ctx context.Context, id uuid.UUID) (*Player, error) {
	var p Player
	var clubName string
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.world_id, p.club_id, p.person_id, p.primary_position, p.squad_number,
		       pe.first_name, pe.last_name, pe.display_name, pe.date_of_birth::text, pe.nationality_code,
		       cc.name
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN club.clubs cc ON cc.id = p.club_id
		WHERE p.id = $1`, id,
	).Scan(&p.ID, &p.WorldID, &p.ClubID, &p.PersonID, &p.PrimaryPosition, &p.SquadNumber,
		&p.FirstName, &p.LastName, &p.DisplayName, &p.DateOfBirth, &p.Nationality, &clubName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.ClubID != nil && clubName != "" {
		p.Club = &apiref.ClubRef{ID: *p.ClubID, Name: clubName}
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

// GetEmotionalState returns the zero-value record until the emotional-state
// layer lands (S08).
func (s *Service) GetEmotionalState(ctx context.Context, playerID uuid.UUID) (*EmotionalState, error) {
	return &EmotionalState{PlayerID: playerID}, nil
}

// clubByManager resolves the club a manager currently manages within a world
// (active job only).
func (s *Service) clubByManager(ctx context.Context, worldID, managerID uuid.UUID) (uuid.UUID, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT cc.id
		 FROM club.clubs cc
		 JOIN manager.managers m ON m.id = cc.current_manager_id
		 WHERE cc.world_id = $1 AND cc.current_manager_id = $2
		   AND m.status = 'active'`,
		worldID, managerID,
	).Scan(&clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrManagerHasNoClub
	}
	if err != nil {
		return uuid.Nil, err
	}
	return clubID, nil
}

// recordEvent emits one world event with an optional explanation through the
// outbox (falls back to record-only when the bus is nil), returning the event
// ID for causal chaining.
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, tick int64,
	actorType string, actorID uuid.UUID, eventType string, payload, expJSON []byte,
) (uuid.UUID, error) {
	e := eventbus.Event{
		ID:          uuid.New(),
		WorldID:     worldID,
		WorldTick:   tick,
		EventType:   eventType,
		Payload:     payload,
		Explanation: expJSON,
	}
	if actorType != "" {
		at := actorType
		e.ActorType = &at
		if actorID != uuid.Nil {
			e.ActorID = &actorID
		}
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return uuid.Nil, err
	}
	return e.ID, nil
}

// worldTick reads the world's current tick inside the caller's tx.
func (s *Service) worldTick(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (int64, error) {
	var tick int64
	if err := tx.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&tick); err != nil {
		return 0, err
	}
	return tick, nil
}
