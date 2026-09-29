package transfer

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// PlayerLifecycle is the pluggable hook the transfer completion transaction
// calls so downstream player state (fresh-start morale, open request cleanup)
// lands atomically with the club move. Implemented by internal/player; declared
// here (net interface, not an import) so transfer never depends on player.
type PlayerLifecycle interface {
	OnPlayerTransferred(ctx context.Context, tx pgx.Tx, playerID, newClubID uuid.UUID) error
}

// SquadDynamics is the pluggable hook the transfer completion transaction calls
// BEFORE the ownership flip so dressing-room relationships and unrest settle
// against the pre-sale squad, atomically with the move. Implemented by
// internal/faction; declared here (net interface, not an import) so transfer
// never depends on faction.
type SquadDynamics interface {
	OnPlayerSold(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, clubID, playerID uuid.UUID) error
}

// Service is the transfer market engine. All writes that emit events go
// through the transactional outbox (OPD-23) via eventbus.WriteTx inside the
// caller's transaction; bus may be nil in tests and falls back to RecordTx.
type Service struct {
	pool            *pgxpool.Pool
	bus             eventbus.Publisher
	store           *Store
	playerLifecycle PlayerLifecycle
	squadDynamics   SquadDynamics
}

// NewService wires the transfer engine onto a pool and the event bus.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service {
	return &Service{pool: pool, bus: bus, store: NewStore(pool)}
}

// WithPlayerLifecycle plugs the downstream hook invoked on transfer completion.
func (s *Service) WithPlayerLifecycle(h PlayerLifecycle) *Service {
	s.playerLifecycle = h
	return s
}

// WithSquadDynamics plugs the dressing-room hook invoked before the ownership
// flip on transfer completion.
func (s *Service) WithSquadDynamics(h SquadDynamics) *Service {
	s.squadDynamics = h
	return s
}
