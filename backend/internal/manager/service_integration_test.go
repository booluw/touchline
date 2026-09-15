//go:build integration

package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/eventbus"
)

// failingBus is an EventBus whose PublishTx always fails — the OPD-23
// fault-injection stand-in: an enqueue failure inside the state tx must abort
// the whole state change, not leave a committed mutant behind.
type failingBus struct{}

func (failingBus) Publish(ctx context.Context, _ *eventbus.Event) error {
	return errors.New("bus: injectable publish failure")
}

func (failingBus) PublishTx(context.Context, pgx.Tx, *eventbus.Event) error {
	return errors.New("bus: injectable publish failure")
}

func (failingBus) Subscribe(context.Context, string, eventbus.EventHandler) error {
	return nil
}

func newTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return NewService(pool, nil), pool
}

func TestJobOfferFullLifecycle(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "Careers")
	clubID, _ := testdb.CreateClubWithAIManager(t, pool, w)
	candidate := testdb.CreateUser(t, pool, "candidate@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	managerRow := managerIDOf(t, pool, candidate, w)

	o, err := svc.CreateJobOffer(ctx, clubID, managerRow)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if o.Status != "proposed" || o.ClubName == "" {
		t.Fatalf("offer = %+v", o)
	}

	offers, err := svc.ListOffers(ctx, managerRow, w)
	if err != nil {
		t.Fatalf("list offers: %v", err)
	}
	if len(offers) != 1 || offers[0].ID != o.ID {
		t.Fatalf("pending offers = %+v, want the fresh offer", offers)
	}

	// Accept: manager active + club handed over + history opened.
	accepted, err := svc.AcceptJobOffer(ctx, o.ID, managerRow)
	if err != nil {
		t.Fatalf("accept offer: %v", err)
	}
	if accepted.Status != "accepted" {
		t.Fatalf("accepted status = %q", accepted.Status)
	}

	var mStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM manager.managers WHERE id = $1`, managerRow).Scan(&mStatus); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	if mStatus != "active" {
		t.Fatalf("manager status after accept = %q, want active", mStatus)
	}
	var (
		aiControl bool
		cManager  *uuid.UUID
	)
	if err := pool.QueryRow(ctx,
		`SELECT is_ai_controlled, current_manager_id FROM club.clubs WHERE id = $1`, clubID,
	).Scan(&aiControl, &cManager); err != nil {
		t.Fatalf("load club: %v", err)
	}
	if aiControl || cManager == nil || *cManager != managerRow {
		t.Fatalf("club after accept: ai=%v manager=%v, want handed to candidate", aiControl, cManager)
	}
	var histCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM manager.manager_history WHERE manager_id = $1 AND end_date IS NULL`, managerRow,
	).Scan(&histCount); err != nil {
		t.Fatalf("history count: %v", err)
	}
	if histCount != 1 {
		t.Fatalf("open history rows = %d, want 1", histCount)
	}

	total, err := svc.WorldReputationTotal(ctx, managerRow, w)
	if err != nil {
		t.Fatalf("world reputation: %v", err)
	}
	if total != careerDeltaAcceptedJob {
		t.Fatalf("reputation after accept = %d, want %d", total, careerDeltaAcceptedJob)
	}

	// Accepting the same offer twice is impossible.
	if _, err := svc.AcceptJobOffer(ctx, o.ID, managerRow); !errors.Is(err, ErrOfferResolved) {
		t.Fatalf("re-accept err = %v, want ErrOfferResolved", err)
	}

	// Resign: club reverts to AI control, history closed, reputation untouched.
	if err := svc.Resign(ctx, managerRow); err != nil {
		t.Fatalf("resign: %v", err)
	}
	var mStatus2 *uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT current_club_id FROM manager.managers WHERE id = $1`, managerRow).Scan(&mStatus2); err != nil {
		t.Fatalf("load manager after resign: %v", err)
	}
	if mStatus2 != nil {
		t.Fatalf("after resign club = %v, want nil", mStatus2)
	}
	if err := pool.QueryRow(ctx,
		`SELECT is_ai_controlled FROM club.clubs WHERE id = $1`, clubID).Scan(&aiControl); err != nil {
		t.Fatalf("load club after resign: %v", err)
	}
	if !aiControl {
		t.Fatalf("club not restored to AI control after resign")
	}
	var closed int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM manager.manager_history WHERE manager_id = $1 AND end_date IS NOT NULL`, managerRow,
	).Scan(&closed); err != nil {
		t.Fatalf("closed history: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed history rows = %d, want 1", closed)
	}
	total, _ = svc.WorldReputationTotal(ctx, managerRow, w)
	if total != careerDeltaAcceptedJob {
		t.Fatalf("reputation after resign = %d, want %d (no delta on resign)", total, careerDeltaAcceptedJob)
	}
}

func TestJobOffer_SackAppendsNegativeReputation(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "Careers")
	clubID, _ := testdb.CreateClubWithAIManager(t, pool, w)
	candidate := testdb.CreateUser(t, pool, "sacked@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	managerRow := managerIDOf(t, pool, candidate, w)

	o, err := svc.CreateJobOffer(ctx, clubID, managerRow)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if _, err := svc.AcceptJobOffer(ctx, o.ID, managerRow); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := svc.Sack(ctx, managerRow, nil); err != nil {
		t.Fatalf("sack: %v", err)
	}

	total, err := svc.WorldReputationTotal(ctx, managerRow, w)
	if err != nil {
		t.Fatalf("reputation: %v", err)
	}
	want := careerDeltaAcceptedJob + careerDeltaSacked
	if total != want {
		t.Fatalf("reputation after sack = %d, want %d", total, want)
	}

	evts, err := svc.GetReputation(ctx, managerRow)
	if err != nil {
		t.Fatalf("reputation log: %v", err)
	}
	if len(evts) != 2 || evts[1].Reason != "sacked" {
		t.Fatalf("reputation log = %+v", evts)
	}

	career, err := svc.ListCareerHistory(ctx, managerRow)
	if err != nil {
		t.Fatalf("career history: %v", err)
	}
	if len(career) != 1 || career[0].EndDate == nil || career[0].OutcomeSummary == nil || *career[0].OutcomeSummary != "sack" {
		t.Fatalf("career = %+v", career)
	}

	var eventType string
	if err := pool.QueryRow(ctx, `
		SELECT event_type FROM world.events
		WHERE world_id = $1 ORDER BY occurred_at DESC LIMIT 1`, w).Scan(&eventType); err != nil {
		t.Fatalf("read sacking event: %v", err)
	}
	if eventType != "MANAGER_SACKED" {
		t.Fatalf("last event = %q, want MANAGER_SACKED", eventType)
	}
}

func TestJobOffer_Rejections(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "Rejections")
	candidate := testdb.CreateUser(t, pool, "reject@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	managerRow := managerIDOf(t, pool, candidate, w)

	// Non-AI club cannot issue offers.
	humanClub := testdb.CreateClub(t, pool, w)
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET is_ai_controlled = FALSE WHERE id = $1`, humanClub); err != nil {
		t.Fatalf("mark non-AI: %v", err)
	}
	if _, err := svc.CreateJobOffer(ctx, humanClub, managerRow); !errors.Is(err, ErrNotAIClub) {
		t.Fatalf("non-AI club err = %v, want ErrNotAIClub", err)
	}

	// Unplayable world rejects offers to its own managers.
	var provisional uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO world.worlds (name, status) VALUES ('quiescent', 'provisioning') RETURNING id`).Scan(&provisional); err != nil {
		t.Fatalf("create provisional world: %v", err)
	}
	pClub, _ := testdb.CreateClubWithAIManager(t, pool, provisional)
	provCandidate := managerIDOf(t, pool, testdb.CreateUser(t, pool, "prov@example.com", "s3cret", []testdb.Join{{WorldID: provisional}}), provisional)
	if _, err := svc.CreateJobOffer(ctx, pClub, provCandidate); !errors.Is(err, ErrClubNotPlayable) {
		t.Fatalf("unplayable world err = %v, want ErrClubNotPlayable", err)
	}

	// A different candidate cannot accept or decline.
	clubID, _ := testdb.CreateClubWithAIManager(t, pool, w)
	o, err := svc.CreateJobOffer(ctx, clubID, managerRow)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	other := managerIDOf(t, pool, testdb.CreateUser(t, pool, "other@example.com", "s3cret", []testdb.Join{{WorldID: w}}), w)
	if _, err := svc.AcceptJobOffer(ctx, o.ID, other); !errors.Is(err, ErrNotOfferCandidate) {
		t.Fatalf("other accept err = %v, want ErrNotOfferCandidate", err)
	}
	if _, err := svc.DeclineJobOffer(ctx, o.ID, other); !errors.Is(err, ErrNotOfferCandidate) {
		t.Fatalf("other decline err = %v, want ErrNotOfferCandidate", err)
	}

	// Decline resolves the offer.
	declined, err := svc.DeclineJobOffer(ctx, o.ID, managerRow)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	if declined.Status != "declined" {
		t.Fatalf("declined status = %q", declined.Status)
	}
	if _, err := svc.AcceptJobOffer(ctx, o.ID, managerRow); !errors.Is(err, ErrOfferResolved) {
		t.Fatalf("accept declined err = %v, want ErrOfferResolved", err)
	}
}

func TestAcceptJobOffer_AlreadyEmployedRejected(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "Double")
	clubA, _ := testdb.CreateClubWithAIManager(t, pool, w)
	clubB, _ := testdb.CreateClubWithAIManager(t, pool, w)
	candidate := testdb.CreateUser(t, pool, "double@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	managerRow := managerIDOf(t, pool, candidate, w)

	// Employ the candidate through the sanctioned path.
	o, err := svc.CreateJobOffer(ctx, clubA, managerRow)
	if err != nil {
		t.Fatalf("offer A: %v", err)
	}
	if _, err := svc.AcceptJobOffer(ctx, o.ID, managerRow); err != nil {
		t.Fatalf("accept A: %v", err)
	}

	// An employed candidate cannot receive offers (CreateJobOffer rejects first,
	// and a straight-through AcceptJobOffer attempt is also rejected — a
	// directly-inserted offer hits the one-active-assignment unique index).
	if _, err := svc.CreateJobOffer(ctx, clubB, managerRow); !errors.Is(err, ErrManagerUnavailable) {
		t.Fatalf("offer B after employed err = %v, want ErrManagerUnavailable", err)
	}
	o2 := bypassOffer(t, pool, w, clubB, managerRow)
	if _, err := svc.AcceptJobOffer(ctx, o2, managerRow); !errors.Is(err, ErrManagerEmployed) {
		t.Fatalf("accept B err = %v, want ErrManagerEmployed", err)
	}
}

// bypassOffer inserts a job offer row directly, bypassing the CreateJobOffer
// guards, to exercise AcceptJobOffer's own invariant checks.
func bypassOffer(t *testing.T, pool *pgxpool.Pool, worldID, clubID, managerID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO manager.job_offers (world_id, club_id, manager_id)
		VALUES ($1, $2, $3) RETURNING id`, worldID, clubID, managerID).Scan(&id); err != nil {
		t.Fatalf("bypass offer: %v", err)
	}
	return id
}

func managerIDOf(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, worldID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1 AND world_id = $2`, userID, worldID).Scan(&id); err != nil {
		t.Fatalf("load manager id: %v", err)
	}
	return id
}

// TestAcceptJobOfferEventFailureRollsBackState proves the producer-level
// atomicity contract: when the JOB_OFFER_ACCEPTED enqueue fails, the whole
// acceptance rolls back — no manager assignment, no club handover, no history
// row, offer still pending, and no event row left behind.
func TestAcceptJobOfferEventFailureRollsBackState(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := NewService(pool, failingBus{})

	w := testdb.CreateWorld(t, pool, "atomic-careers")
	clubID, _ := testdb.CreateClubWithAIManager(t, pool, w)
	candidate := testdb.CreateUser(t, pool, "atomic@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	managerRow := managerIDOf(t, pool, candidate, w)

	o, err := svc.CreateJobOffer(ctx, clubID, managerRow)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}

	if _, err := svc.AcceptJobOffer(ctx, o.ID, managerRow); err == nil {
		t.Fatal("accept must fail when the event enqueue fails")
	}

	var mStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM manager.managers WHERE id = $1`, managerRow).Scan(&mStatus); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	if mStatus == "active" {
		t.Fatal("manager became active despite the acceptance rolling back")
	}
	var (
		aiControl bool
		cManager  *uuid.UUID
	)
	if err := pool.QueryRow(ctx,
		`SELECT is_ai_controlled, current_manager_id FROM club.clubs WHERE id = $1`, clubID,
	).Scan(&aiControl, &cManager); err != nil {
		t.Fatalf("load club: %v", err)
	}
	if !aiControl || cManager == nil {
		t.Fatal("club lost AI control despite the acceptance rolling back")
	}
	var offerStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM manager.job_offers WHERE id = $1`, o.ID).Scan(&offerStatus); err != nil {
		t.Fatalf("load offer: %v", err)
	}
	if offerStatus != "proposed" {
		t.Fatalf("offer status = %q, want still proposed", offerStatus)
	}
	var hist int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM manager.manager_history WHERE manager_id = $1`, managerRow).Scan(&hist); err != nil {
		t.Fatalf("history count: %v", err)
	}
	if hist != 0 {
		t.Fatalf("history rows = %d, want 0 after rolled-back acceptance", hist)
	}
	var events int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM world.events WHERE world_id = $1 AND event_type = 'JOB_OFFER_ACCEPTED'`, w).Scan(&events); err != nil {
		t.Fatalf("event count: %v", err)
	}
	if events != 0 {
		t.Fatalf("JOB_OFFER_ACCEPTED events = %d, want 0 after rollback", events)
	}
}
