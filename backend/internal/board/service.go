package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/manager"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// Event types emitted by this package (S06-02; naming mirrors transfer/finance).
const (
	EventBoardReviewed     = "BOARD_REVIEWED"
	EventMandateMet        = "BOARD_MANDATE_MET"
	EventMandateBroken     = "BOARD_MANDATE_BROKEN"
	EventMandateNegotiated = "BOARD_MANDATE_NEGOTIATED"
)

// Sentinel errors; handlers map these to HTTP status codes, everything else
// surfaces as 500.
var (
	ErrNotEmployed              = errors.New("manager has no active club")
	ErrWorldMismatch            = errors.New("mandate belongs to a different world")
	ErrMandateNotFound          = errors.New("mandate not found")
	ErrNotMandateManager        = errors.New("not your mandate")
	ErrMandateResolved          = errors.New("mandate already resolved")
	ErrMandateTypeNotNegotiable = errors.New("this mandate type is not negotiable")
	ErrMandateValueInvalid      = errors.New("off-window target value")
	ErrNegotiationRejected      = errors.New("board rejected the proposal")
)

// Service is the board engine. Review runs on the monthly day step of the
// world clock (IM02; once per calendar.days_per_month days); BoardView and
// NegotiateMandate serve the human manager. All writes ride the transactional
// outbox via eventbus.WriteTx inside a caller-scoped or local transaction; bus
// may be nil in tests (falls back to RecordTx).
type Service struct {
	pool     *pgxpool.Pool
	bus      eventbus.Publisher
	store    *Store
	managers *manager.Service
}

// NewService wires the board engine; managers is the career engine used for
// the sacking guard (ignored when the club is AI-run).
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher, managers *manager.Service) *Service {
	return &Service{pool: pool, bus: bus, store: NewStore(pool), managers: managers}
}

// Review scores every active manager with a club in the world and applies the
// sacking guard to human-managed clubs. It is the monthly board review (IM02)
// but its formulas are unchanged — it grades by season progress, not elapsed
// days. The per-manager transaction is idempotent on (manager_id, world_tick)
// so a mid-month retry never double-writes.
func (s *Service) Review(ctx context.Context, worldID uuid.UUID, tick int64) (reviewed, sacked int, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id
		FROM manager.managers m
		WHERE m.world_id = $1 AND m.status = 'active' AND m.current_club_id IS NOT NULL
		ORDER BY m.id`, worldID)
	if err != nil {
		return 0, 0, fmt.Errorf("review managers: %w", err)
	}
	var managerIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan review manager: %w", err)
		}
		managerIDs = append(managerIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("review managers: %w", err)
	}

	for _, mid := range managerIDs {
		didSack, err := s.reviewManager(ctx, worldID, mid, tick)
		if err != nil {
			return reviewed, sacked, err
		}
		reviewed++
		if didSack {
			sacked++
		}
	}
	return reviewed, sacked, nil
}

// reviewManager runs one club review and, when the guard fires, sacks the
// manager outside the review transaction (the ending event + reputation delta
// are the career engine's own transaction).
func (s *Service) reviewManager(ctx context.Context, worldID, managerID uuid.UUID, tick int64) (sacked bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin review tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		clubID      uuid.UUID
		isPolicyBot bool
	)
	err = tx.QueryRow(ctx, `
		SELECT m.current_club_id, m.is_policy_bot
		FROM manager.managers m
		WHERE m.id = $1 AND m.world_id = $2 AND m.status = 'active' AND m.current_club_id IS NOT NULL
		FOR UPDATE`, managerID, worldID).Scan(&clubID, &isPolicyBot)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock manager review: %w", err)
	}
	if exists, err := s.store.snapshotExists(ctx, tx, managerID, tick); err != nil || exists {
		if closeErr := tx.Commit(ctx); err == nil {
			err = closeErr
		}
		return false, err
	}

	season, err := s.store.seasonYear(ctx, tx, worldID)
	if err != nil {
		return false, err
	}

	in, err := s.store.loadReview(ctx, tx, worldID, clubID, managerID, season)
	if err != nil {
		return false, err
	}

	finish := expectedFinish(in.ambition, in.patience)
	points := expectedPoints(finish)

	score, exp, err := s.evaluateAndRecord(ctx, tx, worldID, in, finish, points, tick)
	if err != nil {
		return false, err
	}

	sentiment := supporterBlend(in.sentiment, score.Performance)
	if err := s.store.updateSentiment(ctx, tx, clubID, sentiment); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit review: %w", err)
	}

	if !isPolicyBot && score.Total <= SackThresholdTotal {
		if s.managers != nil {
			if err := s.managers.Sack(ctx, managerID, exp); err != nil && !errors.Is(err, manager.ErrNotEmployed) {
				return false, fmt.Errorf("sack %s: %w", managerID, err)
			}
			return true, nil
		}
	}
	return false, nil
}

// BoardView returns the manager-facing board read model for their club.
// It lazily ensures the current season's mandate set and refreshes the current
// tick's snapshot (idempotent) so a manager mid-month always sees a fresh
// confidence number without waiting for the next board review.
func (s *Service) BoardView(ctx context.Context, worldID, managerID uuid.UUID) (*View, error) {
	tick, err := s.store.currentTick(ctx, s.pool, worldID)
	if err != nil {
		return nil, err
	}
	if _, err := s.reviewManager(ctx, worldID, managerID, tick); err != nil {
		return nil, err
	}
	snap, err := s.store.latestSnapshot(ctx, s.pool, managerID)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, nil
	}
	season, err := s.store.seasonYear(ctx, s.pool, worldID)
	if err != nil {
		return nil, err
	}
	mandates, err := s.store.listMandates(ctx, s.pool, snap.ClubID, managerID, season)
	if err != nil {
		return nil, err
	}
	v := &View{Confidence: snap.Scores.Total, Snapshot: snap, Mandates: mandates}
	_ = json.Unmarshal(snap.Explanation, &v.Explanation)

	var clubName string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM club.clubs WHERE id = $1`, snap.ClubID).Scan(&clubName); err != nil {
		return nil, fmt.Errorf("board view: club name: %w", err)
	}
	snap.Club = &apiref.ClubRef{ID: snap.ClubID, Name: clubName}
	for i := range mandates {
		mandates[i].Club = &apiref.ClubRef{ID: mandates[i].ClubID, Name: clubName}
	}
	return v, nil
}

// NegotiateMandate proposes a bounded new target for one sporting mandate.
// Accepted proposals flip the status to agreed and record the negotiation.
func (s *Service) NegotiateMandate(ctx context.Context, worldID, managerID uuid.UUID, in NegotiateInput) (*Mandate, *explanation.Explanation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin negotiate tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	mandate, err := s.store.loadMandateByID(ctx, tx, in.MandateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrMandateNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load mandate: %w", err)
	}
	if mandate.ManagerID != managerID {
		return nil, nil, ErrNotMandateManager
	}

	var worldOf uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT current_club_id FROM manager.managers WHERE id = $1`, managerID).
		Scan(&worldOf); err != nil {
		return nil, nil, ErrNotEmployed
	}
	if worldOf != mandate.ClubID {
		return nil, nil, ErrWorldMismatch
	}
	if mandate.Status != MandatePending && mandate.Status != MandateAgreed {
		return nil, nil, ErrMandateResolved
	}

	persona, _, _, err := s.fetchPersona(ctx, tx, mandate.ClubID)
	if err != nil {
		return nil, nil, err
	}
	cur, ok := parseInt(mandate.TargetValue)
	if !ok {
		return nil, nil, ErrMandateValueInvalid
	}
	proposal, ok := parseInt(in.TargetValue)
	if !ok {
		return nil, nil, ErrMandateValueInvalid
	}
	switch mandate.TargetType {
	case TargetLeagueFinish, TargetPointsTarget:
	default:
		return nil, nil, ErrMandateTypeNotNegotiable
	}
	if !withinNegotiationWindow(mandate.TargetType, cur, proposal) {
		return nil, nil, ErrMandateValueInvalid
	}
	delta := negotiationDelta(mandate.TargetType, cur, proposal)
	if delta > 0 && delta > negotiationTolerance(persona) {
		return nil, nil, ErrNegotiationRejected
	}
	if err := s.store.setMandateTarget(ctx, tx, mandate.ID, in.TargetValue); err != nil {
		return nil, nil, err
	}

	mandate.TargetValue = in.TargetValue
	mandate.Status = MandateAgreed
	var clubName string
	if err := tx.QueryRow(ctx, `SELECT name FROM club.clubs WHERE id = $1`, mandate.ClubID).Scan(&clubName); err != nil {
		return nil, nil, fmt.Errorf("negotiate: club name: %w", err)
	}
	mandate.Club = &apiref.ClubRef{ID: mandate.ClubID, Name: clubName}
	exp := explanation.New("board_mandate_negotiation", 0).
		Add(fmt.Sprintf("%s target changed to %s", mandate.TargetType, in.TargetValue), 0)
	if err := s.emitNegotiation(ctx, tx, worldOf, &mandate, exp); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit negotiation: %w", err)
	}
	return &mandate, exp, nil
}
