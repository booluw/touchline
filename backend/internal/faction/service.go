// Service is the S09-02 façade: ownership-gated reads of the dressing-room
// graph (GetDynamics) and the transfer-time hook that keeps squad relationships
// current and raises SQUAD_UNREST_TRIGGERED (OnPlayerSold). The pure math lives
// in engine.go; this layer owns transactions, wiring and API shape.
package faction

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// ErrNotOwned is returned when the acting manager does not manage the club.
var ErrNotOwned = errors.New("faction: club not managed by actor")

// Service is the dressing-room dynamics service.
type Service struct {
	pool   *pgxpool.Pool
	bus    eventbus.Publisher
	store  *store
	engine *Engine
}

// NewService wires squad dynamics onto a pool and the event bus. bus may be nil
// in tests; the transactional outbox then falls back to RecordTx.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service {
	return &Service{pool: pool, bus: bus, store: &store{}, engine: NewEngine()}
}

// RequireOwnership enforces that the actor currently manages the club.
func (s *Service) RequireOwnership(ctx context.Context, managerID, clubID uuid.UUID) error {
	var current uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || current != clubID {
		return ErrNotOwned
	}
	if err != nil {
		return fmt.Errorf("faction: ownership: %w", err)
	}
	return nil
}

// GetDynamics returns the club's computed hierarchy, factions, cohesion,
// manager support, dressing-room mood and any current unrest. Generating the
// graph is idempotent, so the read self-heals for clubs transferred before
// S09-02 shipped.
func (s *Service) GetDynamics(ctx context.Context, worldID, managerID, clubID uuid.UUID) (Dynamics, error) {
	if err := s.RequireOwnership(ctx, managerID, clubID); err != nil {
		return Dynamics{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Dynamics{}, fmt.Errorf("faction: begin dynamics: %w", err)
	}
	defer tx.Rollback(ctx)

	profiles, err := s.store.ensureSquadRelationships(ctx, tx, worldID, clubID)
	if err != nil {
		return Dynamics{}, err
	}
	snap, err := s.store.squadSnapshot(ctx, tx, worldID, clubID)
	if err != nil {
		return Dynamics{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Dynamics{}, fmt.Errorf("faction: commit dynamics: %w", err)
	}

	names := make(map[string]string, len(profiles))
	for _, p := range profiles {
		names[p.PlayerID] = p.Name
	}

	res := s.engine.Evaluate(ActionInspect, snap)
	mood, err := s.store.dressingRoomMood(ctx, s.pool, clubID)
	if err != nil {
		return Dynamics{}, err
	}
	support, err := s.store.managerSupport(ctx, s.pool, worldID, clubID, managerID)
	if err != nil {
		return Dynamics{}, err
	}
	unrest, err := s.store.recentUnrest(ctx, s.pool, worldID, clubID)
	if err != nil {
		return Dynamics{}, err
	}

	return Dynamics{
		ClubID:           clubID,
		Action:           res.Action,
		Cohesion:         overallCohesion(res.Factions),
		ManagerSupport:   support,
		DressingRoomMood: mood,
		Tiers:            sortedTiers(res.Tiers, names),
		Factions:         res.Factions,
		Contagion:        res.Contagion,
		Unrest:           unrest,
		Explanation:      res.Explanation,
	}, nil
}

// OnPlayerSold runs in the transfer transaction BEFORE the ownership flip, so
// the snapshot still contains the departing player. It refreshes the club's
// graph, records former-teammate bonds, and — when contagion from the sale
// crosses the unrest threshold — emits SQUAD_UNREST_TRIGGERED.
func (s *Service) OnPlayerSold(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, clubID, playerID uuid.UUID) error {
	profiles, err := s.store.ensureSquadRelationships(ctx, tx, worldID, clubID)
	if err != nil {
		return err
	}
	snap, err := s.store.squadSnapshot(ctx, tx, worldID, clubID)
	if err != nil {
		return err
	}

	others := make([]string, 0, len(profiles))
	for _, p := range profiles {
		if p.PlayerID != playerID.String() {
			others = append(others, p.PlayerID)
		}
	}
	if len(others) > 0 {
		if err := s.store.upsertEdges(ctx, tx, worldID, FormerTeammateEdges(playerID.String(), others)); err != nil {
			return err
		}
	}

	c := s.engine.Contagion(playerID.String(), snap)
	u := s.engine.UnrestFrom(c)
	if u == nil {
		return nil
	}
	return s.store.emitUnrest(ctx, tx, s.bus, worldID, worldTick, clubID, playerID, u)
}

// overallCohesion is the member-weighted mean faction cohesion, bounded
// [10,95]; neutral 50 for an empty room.
func overallCohesion(factions []Faction) int {
	total, members := 0, 0
	for _, f := range factions {
		total += f.Cohesion * len(f.Members)
		members += len(f.Members)
	}
	if members == 0 {
		return 50
	}
	return clampInt(total/members, CohesionMin, CohesionMax)
}
