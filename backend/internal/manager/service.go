// Package manager owns the manager's career lifecycle: the job-offer state
// machine (S02-02), club assignment boundary, and the append-only reputation
// log. Assignments are the ONLY sanctioned path to a first club: an AI club
// offers a job, the human manager accepts or declines.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// Sentinel errors. Handlers map these to HTTP status codes; everything else
// surfaces as 500.
var (
	ErrOfferNotFound      = errors.New("job offer not found")
	ErrOfferResolved      = errors.New("job offer already responded to")
	ErrNotOfferCandidate  = errors.New("not the offer's candidate")
	ErrManagerEmployed    = errors.New("manager already holds a club")
	ErrManagerUnavailable = errors.New("manager cannot be hired right now")
	ErrClubNotFound       = errors.New("club not found")
	ErrClubNotPlayable    = errors.New("club is not in a playable world")
	ErrClubOccupied       = errors.New("club already has a human manager")
	ErrNotAIClub          = errors.New("offer is not from an AI club")
	ErrClubWorldMismatch  = errors.New("club is not in the candidate's world")
	ErrNoOnboardingClub   = errors.New("no AI club is available for onboarding")
	ErrNotEmployed        = errors.New("manager has no club")
)

// JobOffer is the offer an AI club makes to an unemployed human manager.
type JobOffer struct {
	ID        uuid.UUID `json:"id"`
	WorldID   uuid.UUID `json:"world_id"`
	ClubID    uuid.UUID `json:"club_id"`
	ClubName  string    `json:"club_name"`
	ManagerID uuid.UUID `json:"manager_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ReputationEvent is one append-only reputation line (never mutated).
// WorldID nil rows are global/display career history; world-scoped rows feed
// in-world hiring logic.
type ReputationEvent struct {
	ID         uuid.UUID  `json:"id"`
	ManagerID  uuid.UUID  `json:"manager_id"`
	WorldID    *uuid.UUID `json:"world_id"`
	Category   string     `json:"category"`
	Delta      int        `json:"delta"`
	Reason     string     `json:"reason"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// CareerEntry is one entry in the manager's employment history (manager.manager_history).
type CareerEntry struct {
	ClubID         uuid.UUID  `json:"club_id"`
	ClubName       string     `json:"club_name"`
	Role           string     `json:"role"`
	StartDate      time.Time  `json:"start_date"`
	EndDate        *time.Time `json:"end_date"`
	OutcomeSummary *string    `json:"outcome_summary"`
}

// career deltas applied on assignment border events. Values are data-driven
// constants (tunable without redeploys of game rules) — the log itself stays
// append-only and neutral.
const (
	careerDeltaAcceptedJob = 5
	careerDeltaSacked      = -10
)

// Service coordinates offer lifecycles, assignment, and the reputation log.
type Service struct {
	pool *pgxpool.Pool
	bus  eventbus.EventBus
}

// NewService builds the manager service.
func NewService(pool *pgxpool.Pool, bus eventbus.EventBus) *Service {
	return &Service{pool: pool, bus: bus}
}

// CreateJobOffer lets an AI club's manager (or the club itself) offer a job to
// an unemployed human manager in the same world. The club must be AI-managed
// (is_ai_controlled) and the world must be playable.
func (s *Service) CreateJobOffer(ctx context.Context, clubID, candidateID uuid.UUID) (*JobOffer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin offer tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		candidateWorld uuid.UUID
		candidateUser  *uuid.UUID
		candidateState string
		candidateClub  *uuid.UUID
		candidateBot   bool
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, user_id, status, current_club_id, is_policy_bot
		 FROM manager.managers WHERE id = $1 FOR UPDATE`, candidateID,
	).Scan(&candidateWorld, &candidateUser, &candidateState, &candidateClub, &candidateBot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotOfferCandidate
	}
	if err != nil {
		return nil, fmt.Errorf("load candidate: %w", err)
	}
	if candidateUser == nil || candidateState != "unemployed" || candidateClub != nil || candidateBot {
		return nil, ErrManagerUnavailable
	}

	var (
		clubWorld   uuid.UUID
		clubAI      bool
		clubCurrent *uuid.UUID
		clubName    string
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, is_ai_controlled, current_manager_id, name
		 FROM club.clubs WHERE id = $1 FOR UPDATE`, clubID,
	).Scan(&clubWorld, &clubAI, &clubCurrent, &clubName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load club: %w", err)
	}
	if clubWorld != candidateWorld {
		return nil, ErrClubWorldMismatch
	}
	if !clubAI {
		return nil, ErrNotAIClub
	}
	if clubCurrent != nil {
		// an AI club normally points at its own policy-bot manager; a club with a
		// human manager (or whose manager is not AI) cannot issue new offers.
		var isBot bool
		if err := tx.QueryRow(ctx,
			`SELECT is_policy_bot FROM manager.managers WHERE id = $1`, clubCurrent,
		).Scan(&isBot); err != nil {
			return nil, fmt.Errorf("load club manager: %w", err)
		}
		if !isBot {
			return nil, ErrClubOccupied
		}
	}
	if ok, err := playable(ctx, tx, clubWorld); err != nil {
		return nil, fmt.Errorf("check world: %w", err)
	} else if !ok {
		return nil, ErrClubNotPlayable
	}

	o := &JobOffer{}
	err = tx.QueryRow(ctx, `
		INSERT INTO manager.job_offers (world_id, club_id, manager_id, offered_by_manager_id)
		VALUES ($1, $2, $3, $4) RETURNING id, status, created_at`,
		clubWorld, clubID, candidateID, clubCurrent,
	).Scan(&o.ID, &o.Status, &o.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrOfferResolved
		}
		return nil, fmt.Errorf("insert offer: %w", err)
	}
	o.WorldID, o.ClubID, o.ManagerID, o.ClubName = clubWorld, clubID, candidateID, clubName
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit offer: %w", err)
	}
	return o, nil
}

// OnboardingAIClubID deterministically picks (ORDER BY id LIMIT 1) the first
// AI club in a world that can still issue job offers: AI-controlled, whose
// current manager (if any) is its own policy bot. Registration (A13) uses this
// to auto-offer a new join their first job. Returns ErrNoOnboardingClub when
// the world has no such club (world seeded without clubs, or all go human-run).
func (s *Service) OnboardingAIClubID(ctx context.Context, worldID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT c.id
		FROM club.clubs c
		LEFT JOIN manager.managers m ON m.id = c.current_manager_id
		WHERE c.world_id = $1
		  AND c.is_ai_controlled = TRUE
		  AND (c.current_manager_id IS NULL OR m.is_policy_bot = TRUE)
		ORDER BY c.id
		LIMIT 1`, worldID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoOnboardingClub
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("pick onboarding club: %w", err)
	}
	return id, nil
}

// ListOffers returns the candidate's pending offers for a given world.
func (s *Service) ListOffers(ctx context.Context, managerID, worldID uuid.UUID) ([]*JobOffer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.world_id, o.club_id, c.name, o.manager_id, o.status, o.created_at
		FROM manager.job_offers o
		JOIN club.clubs c ON c.id = o.club_id
		WHERE o.manager_id = $1 AND o.status = 'proposed' AND (o.world_id = $2 OR $2::uuid IS NULL)
		ORDER BY o.created_at`, managerID, worldID)
	if err != nil {
		return nil, fmt.Errorf("list offers: %w", err)
	}
	defer rows.Close()

	var out []*JobOffer
	for rows.Next() {
		o := &JobOffer{}
		if err := rows.Scan(&o.ID, &o.WorldID, &o.ClubID, &o.ClubName, &o.ManagerID, &o.Status, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan offer: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// AcceptJobOffer hires the candidate at the offering club. Enforces the
// one-active-assignment invariant (database-advisory + partial-unique indexes
// in migration 0024) and opens a manager_history row.
func (s *Service) AcceptJobOffer(ctx context.Context, offerID, managerID uuid.UUID) (*JobOffer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin accept tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		clubID      uuid.UUID
		clubName    string
		worldID     uuid.UUID
		offerTarget uuid.UUID
		status      string
		createdAt   time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT club_id, world_id, manager_id, status, created_at FROM manager.job_offers WHERE id = $1 FOR UPDATE`, offerID,
	).Scan(&clubID, &worldID, &offerTarget, &status, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOfferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load offer: %w", err)
	}
	if offerTarget != managerID {
		return nil, ErrNotOfferCandidate
	}
	if status != "proposed" {
		return nil, ErrOfferResolved
	}
	if ok, err := playable(ctx, tx, worldID); err != nil {
		return nil, fmt.Errorf("check world: %w", err)
	} else if !ok {
		return nil, ErrClubNotPlayable
	}

	var (
		candUser  *uuid.UUID
		candState string
		candClub  *uuid.UUID
	)
	err = tx.QueryRow(ctx,
		`SELECT user_id, status, current_club_id FROM manager.managers WHERE id = $1 FOR UPDATE`,
		managerID,
	).Scan(&candUser, &candState, &candClub)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotOfferCandidate
	}
	if err != nil {
		return nil, fmt.Errorf("load candidate: %w", err)
	}
	if candUser == nil || candState != "unemployed" || candClub != nil {
		return nil, ErrManagerEmployed
	}

	err = tx.QueryRow(ctx, `SELECT name FROM club.clubs WHERE id = $1 FOR UPDATE`, clubID).Scan(&clubName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load club: %w", err)
	}

	// The club row is locked above. Hand over: the previous AI manager stands
	// down so the one-active-per-club index has room, then the human takes the
	// seat (PRD §45 — an AI club hands over when a real manager accepts).
	if _, err := tx.Exec(ctx, `
		UPDATE manager.managers SET current_club_id = NULL, status = 'unemployed'
		WHERE current_club_id = $1 AND id <> $2`, clubID, managerID); err != nil {
		return nil, fmt.Errorf("free incumbent manager: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE manager.managers SET status = 'active', current_club_id = $2 WHERE id = $1`, managerID, clubID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrManagerEmployed
		}
		return nil, fmt.Errorf("assign manager: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE club.clubs SET current_manager_id = $1, is_ai_controlled = FALSE WHERE id = $2`, managerID, clubID); err != nil {
		return nil, fmt.Errorf("set club manager: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE manager.job_offers SET status = 'accepted', responded_at = now() WHERE id = $1`, offerID); err != nil {
		return nil, fmt.Errorf("accept offer: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO manager.manager_history (manager_id, world_id, club_id, role, start_date)
		VALUES ($1, $2, $3, 'manager', CURRENT_DATE)`, managerID, worldID, clubID); err != nil {
		return nil, fmt.Errorf("open history: %w", err)
	}

	payload, err := payloadJSON("club_id", clubID, "offer_id", offerID)
	if err != nil {
		return nil, err
	}
	eventID, err := s.recordEvent(ctx, tx, worldID, "JOB_OFFER_ACCEPTED", "manager", managerID, payload)
	if err != nil {
		return nil, err
	}
	if err := s.appendReputation(ctx, tx, managerID, &worldID, "career", careerDeltaAcceptedJob,
		"took over", eventID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit acceptance: %w", err)
	}
	return &JobOffer{ID: offerID, WorldID: worldID, ClubID: clubID, ClubName: clubName, ManagerID: managerID, Status: "accepted", CreatedAt: createdAt}, nil
}

// DeclineJobOffer marks the offer declined. The club may not re-offer while a
// pending offer exists (unique index), but can issue a fresh one afterwards.
func (s *Service) DeclineJobOffer(ctx context.Context, offerID, managerID uuid.UUID) (*JobOffer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin decline tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		offerManager uuid.UUID
		status       string
		createdAt    time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT manager_id, status, created_at FROM manager.job_offers WHERE id = $1 FOR UPDATE`, offerID,
	).Scan(&offerManager, &status, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOfferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load offer: %w", err)
	}
	if offerManager != managerID {
		return nil, ErrNotOfferCandidate
	}
	if status != "proposed" {
		return nil, ErrOfferResolved
	}

	if _, err := tx.Exec(ctx,
		`UPDATE manager.job_offers SET status = 'declined', responded_at = now() WHERE id = $1`, offerID); err != nil {
		return nil, fmt.Errorf("decline offer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit decline: %w", err)
	}
	return &JobOffer{ID: offerID, ManagerID: managerID, Status: "declined", CreatedAt: createdAt}, nil
}

// Resign has the manager quit their current club (self-service, actor=manager).
func (s *Service) Resign(ctx context.Context, managerID uuid.UUID) error {
	return s.endAssignment(ctx, managerID, "resign", "manager")
}

// Sack terminates the manager's assignment with cause (actor=board). Wired for
// the future board/hiring engine (S06); nothing calls it over HTTP yet.
func (s *Service) Sack(ctx context.Context, managerID uuid.UUID) error {
	return s.endAssignment(ctx, managerID, "sack", "board")
}

// endAssignment closes the current assignment: manager to unemployed, club back
// to AI control, history row closed, event + reputation delta appended. The
// history row is never deleted — the career log is append-only.
func (s *Service) endAssignment(ctx context.Context, managerID uuid.UUID, reason, actorType string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin end-assignment tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		worldID uuid.UUID
		clubID  uuid.UUID
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, current_club_id FROM manager.managers
		 WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL FOR UPDATE`, managerID,
	).Scan(&worldID, &clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotEmployed
	}
	if err != nil {
		return fmt.Errorf("load assignment: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE manager.managers SET status = 'unemployed', current_club_id = NULL WHERE id = $1`, managerID); err != nil {
		return fmt.Errorf("free manager: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE club.clubs SET current_manager_id = NULL, is_ai_controlled = TRUE WHERE id = $1`, clubID); err != nil {
		return fmt.Errorf("revert club to AI control: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE manager.manager_history SET end_date = CURRENT_DATE, outcome_summary = $2
		WHERE manager_id = $1 AND end_date IS NULL`, managerID, reason); err != nil {
		return fmt.Errorf("close history: %w", err)
	}

	eventType := "MANAGER_RESIGNED"
	if reason == "sack" {
		eventType = "MANAGER_SACKED"
	}
	payload, err := payloadJSON("club_id", clubID)
	if err != nil {
		return err
	}
	eventID, err := s.recordEvent(ctx, tx, worldID, eventType, actorType, managerID, payload)
	if err != nil {
		return err
	}
	if reason == "sack" {
		if err := s.appendReputation(ctx, tx, managerID, &worldID, "career", careerDeltaSacked,
			"sacked", eventID); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit end-assignment: %w", err)
	}
	return nil
}

// recordEvent appends one world.events row inside the caller's transaction and,
// when a bus is wired, enqueues its dispatch job in the same tx (the
// transactional outbox, OPD-23). Dispatch can never be lost between a committed
// state change and a separate publish call, and publish errors are never
// silently swallowed — a failure aborts the enclosing tx.
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType, actorType string, actorID uuid.UUID, payload []byte) (*uuid.UUID, error) {
	actor := actorType
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actor,
		ActorID:   &actorID,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return nil, fmt.Errorf("record %s event: %w", eventType, err)
	}
	return &e.ID, nil
}

// ListCareerHistory returns the manager's employment history (jobs + notes),
// oldest first. Outcome text is generated at contract close by whatever system
// ended it (S04+); S02-02 records the raw reason.
func (s *Service) ListCareerHistory(ctx context.Context, managerID uuid.UUID) ([]*CareerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT h.club_id, c.name, h.role, h.start_date, h.end_date, h.outcome_summary
		FROM manager.manager_history h
		JOIN club.clubs c ON c.id = h.club_id
		WHERE h.manager_id = $1
		ORDER BY h.start_date`, managerID)
	if err != nil {
		return nil, fmt.Errorf("career history: %w", err)
	}
	defer rows.Close()

	var out []*CareerEntry
	for rows.Next() {
		e := &CareerEntry{}
		if err := rows.Scan(&e.ClubID, &e.ClubName, &e.Role, &e.StartDate, &e.EndDate, &e.OutcomeSummary); err != nil {
			return nil, fmt.Errorf("scan career: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetReputation returns the append-only reputation log for a manager (all
// worlds, chronological). Display-oriented; in-world hiring reads
// WorldReputationTotal.
func (s *Service) GetReputation(ctx context.Context, managerID uuid.UUID) ([]*ReputationEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, manager_id, world_id, category, delta, reason, occurred_at
		FROM manager.manager_reputation_events
		WHERE manager_id = $1 ORDER BY occurred_at`, managerID)
	if err != nil {
		return nil, fmt.Errorf("reputation log: %w", err)
	}
	defer rows.Close()

	var out []*ReputationEvent
	for rows.Next() {
		e := &ReputationEvent{}
		if err := rows.Scan(&e.ID, &e.ManagerID, &e.WorldID, &e.Category, &e.Delta, &e.Reason, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan reputation: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WorldReputationTotal sums the manager's world-scoped reputation deltas. This
// is what hiring/board logic reads in a specific world (S06).
func (s *Service) WorldReputationTotal(ctx context.Context, managerID, worldID uuid.UUID) (int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(delta), 0) FROM manager.manager_reputation_events
		WHERE manager_id = $1 AND world_id = $2`, managerID, worldID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("world reputation: %w", err)
	}
	return total, nil
}

// appendReputation writes one line to the append-only log (internal).
func (s *Service) appendReputation(ctx context.Context, tx pgx.Tx, managerID uuid.UUID, worldID *uuid.UUID, category string, delta int, reason string, relatedEventID *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO manager.manager_reputation_events (manager_id, world_id, category, delta, reason, related_event_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		managerID, worldID, category, delta, reason, relatedEventID); err != nil {
		return fmt.Errorf("append reputation: %w", err)
	}
	return nil
}

func playable(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (bool, error) {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM world.worlds WHERE id = $1`, worldID).Scan(&status); err != nil {
		return false, err
	}
	return status == "active" || status == "open_beta", nil
}

func payloadJSON(kv ...any) ([]byte, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("payload requires key/value pairs")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return b, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
