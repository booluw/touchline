//go:build integration

package board_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalboard "github.com/touchline/backend/internal/board"
	"github.com/touchline/backend/internal/manager"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfertest"
)

const (
	seasonYear = 2026 // world launched date drives the review season
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.New(t)
}

func newTestService(t *testing.T, pool *pgxpool.Pool) *internalboard.Service {
	return internalboard.NewService(pool, nil, manager.NewService(pool, nil))
}

func TestReviewWritesSnapshotsAndEvents(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	w := transfertest.Provision(t, pool, "Board Weekly", "weekly@example.com")
	svc := newTestService(t, pool)

	reviewed, sacked, err := svc.Review(ctx, w.WorldID, 7)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if reviewed != 3 {
		t.Errorf("reviewed = %d, want 3", reviewed)
	}
	if sacked != 0 {
		t.Errorf("sacked = %d, want 0 (neutral scenario)", sacked)
	}

	var snapshotCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM manager.job_security_snapshots`).Scan(&snapshotCount); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if snapshotCount != 3 {
		t.Errorf("snapshots = %d, want 3", snapshotCount)
	}

	var reviewEvents int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM world.events WHERE event_type = 'BOARD_REVIEWED'`).
		Scan(&reviewEvents); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if reviewEvents != 3 {
		t.Errorf("BOARD_REVIEWED events = %d, want 3", reviewEvents)
	}

	// Every snapshot's explanation factors must sum to its total (the exact
	// weighted-sum contract), and every score must stay in [0,100].
	rows, err := pool.Query(ctx, `
		SELECT total_score, performance_score, expectations_score, financial_score,
		       board_relationship_score, club_dna_alignment_score,
		       supporter_sentiment_score, alternatives_score, explanation
		FROM manager.job_security_snapshots`)
	if err != nil {
		t.Fatalf("load snapshots: %v", err)
	}
	defer rows.Close()
	type scores struct {
		Performance  int
		Expectations int
		Financial    int
		Relationship int
		DNA          int
		Supporter    int
		Alternatives int
	}
	for rows.Next() {
		var total int
		var raw []byte
		var s scores
		if err := rows.Scan(&total, &s.Performance, &s.Expectations, &s.Financial,
			&s.Relationship, &s.DNA, &s.Supporter, &s.Alternatives, &raw); err != nil {
			t.Fatalf("scan snapshot: %v", err)
		}
		for _, v := range []int{s.Performance, s.Expectations, s.Financial, s.Relationship,
			s.DNA, s.Supporter, s.Alternatives} {
			if v < 0 || v > 100 {
				t.Errorf("factor %d out of range", v)
			}
		}
		var exp struct {
			Score   int               `json:"score"`
			Factors []json.RawMessage `json:"factors"`
		}
		if err := json.Unmarshal(raw, &exp); err != nil {
			t.Fatalf("unmarshal explanation: %v", err)
		}
		var sum int
		for _, f := range exp.Factors {
			var factor struct {
				Delta int `json:"delta"`
			}
			if err := json.Unmarshal(f, &factor); err != nil {
				t.Fatalf("unmarshal factor: %v", err)
			}
			sum += factor.Delta
		}
		if total != exp.Score || sum != exp.Score {
			t.Errorf("explanation total %d, factors %d, stored %d — must all agree", total, exp.Score, sum)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate snapshots: %v", err)
	}

	// Idempotent for the same tick: a re-run writes nothing new.
	if _, _, err := svc.Review(ctx, w.WorldID, 7); err != nil {
		t.Fatalf("review replay: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM manager.job_security_snapshots`).
		Scan(&snapshotCount); err != nil {
		t.Fatalf("count snapshots after replay: %v", err)
	}
	if snapshotCount != 3 {
		t.Errorf("snapshots after replay = %d, want 3 (idempotent)", snapshotCount)
	}
}

func TestBoardMandatesGeneratedOnFirstView(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	w := transfertest.Provision(t, pool, "Board Mandates", "mandates@example.com")
	svc := newTestService(t, pool)

	view, err := svc.BoardView(ctx, w.WorldID, w.HumanMgr)
	if err != nil {
		t.Fatalf("board view: %v", err)
	}
	if view == nil {
		t.Fatal("board view nil")
	}
	if len(view.Mandates) != 4 {
		t.Fatalf("mandates = %d, want 4", len(view.Mandates))
	}
	want := []string{"primary", "secondary", "strategic", "financial"}
	for i, m := range view.Mandates {
		if m.Category != want[i] {
			t.Errorf("mandate %d category = %q, want %q", i, m.Category, want[i])
		}
		if m.Status != internalboard.MandatePending {
			t.Errorf("fresh mandate %d status = %q, want pending", i, m.Status)
		}
	}
	if view.Snapshot == nil {
		t.Fatal("board view snapshot missing")
	}
	if view.Confidence != view.Snapshot.Scores.Total {
		t.Errorf("confidence %d != snapshot total %d", view.Confidence, view.Snapshot.Scores.Total)
	}
}

func TestNegotiateMandateLifecycle(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	w := transfertest.Provision(t, pool, "Board Negotiate", "negotiate@example.com")
	svc := newTestService(t, pool)

	view, err := svc.BoardView(ctx, w.WorldID, w.HumanMgr)
	if err != nil {
		t.Fatalf("board view: %v", err)
	}
	var finish *internalboard.Mandate
	var strategic *internalboard.Mandate
	for i := range view.Mandates {
		switch view.Mandates[i].TargetType {
		case internalboard.TargetLeagueFinish:
			finish = &view.Mandates[i]
		case internalboard.TargetOperatingBank:
			strategic = &view.Mandates[i]
		}
	}
	if finish == nil || strategic == nil {
		t.Fatal("expected league_finish and operating_balance mandates")
	}

	// A demanding board rejects a within-window softening of the finish target.
	if _, err := pool.Exec(ctx,
		`UPDATE club.boards SET personality_type = 'demanding_owner' WHERE club_id = $1`, w.HumanClub); err != nil {
		t.Fatalf("force persona: %v", err)
	}
	newTarget := finish.TargetValue + "2" // +2 places is in-window but beyond tolerance 0
	_, _, err = svc.NegotiateMandate(ctx, w.WorldID, w.HumanMgr,
		internalboard.NegotiateInput{MandateID: finish.ID, TargetValue: newTarget})
	if !errors.Is(err, internalboard.ErrNegotiationRejected) {
		t.Fatalf("decode target err = %v, want ErrNegotiationRejected", err)
	}

	// An off-window proposal is rejected as invalid.
	_, _, err = svc.NegotiateMandate(ctx, w.WorldID, w.HumanMgr,
		internalboard.NegotiateInput{MandateID: finish.ID, TargetValue: "18"})
	if !errors.Is(err, internalboard.ErrMandateValueInvalid) {
		t.Fatalf("off-window err = %v, want ErrMandateValueInvalid", err)
	}

	// Strategic/financial mandates are not negotiable in this slice.
	_, _, err = svc.NegotiateMandate(ctx, w.WorldID, w.HumanMgr,
		internalboard.NegotiateInput{MandateID: strategic.ID, TargetValue: "1"})
	if !errors.Is(err, internalboard.ErrMandateTypeNotNegotiable) {
		t.Fatalf("strategic err = %v, want ErrMandateTypeNotNegotiable", err)
	}

	// A patient board accepts the same +2 proposal.
	if _, err := pool.Exec(ctx,
		`UPDATE club.boards SET personality_type = 'patient_owner' WHERE club_id = $1`, w.HumanClub); err != nil {
		t.Fatalf("reset persona: %v", err)
	}
	updated, exp, err := svc.NegotiateMandate(ctx, w.WorldID, w.HumanMgr,
		internalboard.NegotiateInput{MandateID: finish.ID, TargetValue: newTarget})
	if err != nil {
		t.Fatalf("negotiate accepted: %v", err)
	}
	if updated.Status != internalboard.MandateAgreed || updated.TargetValue != newTarget {
		t.Errorf("accepted mandate = %+v, want agreed + %s", updated, newTarget)
	}
	if exp == nil || exp.Subject != "board_mandate_negotiation" {
		t.Errorf("negotiation explanation = %+v", exp)
	}
	var evCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE event_type = 'BOARD_MANDATE_NEGOTIATED' AND actor_id = $1`,
		w.HumanMgr).Scan(&evCount); err != nil {
		t.Fatalf("count negotiate events: %v", err)
	}
	if evCount != 1 {
		t.Errorf("negotiation events = %d, want 1", evCount)
	}

	// Another manager's mandate is not negotiable by this caller.
	if _, _, err := svc.Review(ctx, w.WorldID, 8); err != nil {
		t.Fatalf("review: %v", err)
	}
	var otherMandate uuid.UUID
	var otherMgr uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT bm.id, bm.manager_id FROM club.board_mandates bm
		WHERE bm.manager_id <> $1 LIMIT 1`, w.HumanMgr).Scan(&otherMandate, &otherMgr); err != nil {
		t.Fatalf("load other manager mandate: %v", err)
	}
	_ = otherMgr
	if _, _, err := svc.NegotiateMandate(ctx, w.WorldID, w.HumanMgr,
		internalboard.NegotiateInput{MandateID: otherMandate, TargetValue: "5"}); !errors.Is(err, internalboard.ErrNotMandateManager) {
		t.Fatalf("other-manager err = %v, want ErrNotMandateManager", err)
	}
}

func TestReviewSacksUnderperformingHumanManager(t *testing.T) {
	pool := newTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	w := transfertest.Provision(t, pool, "Board Sack", "sack@example.com")
	svc := newTestService(t, pool)

	forceLowConfidence(t, pool, w)

	reviewed, sacked, err := svc.Review(ctx, w.WorldID, 9)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if reviewed != 3 {
		t.Errorf("reviewed = %d, want 3", reviewed)
	}
	if sacked != 1 {
		t.Errorf("sacked = %d, want 1", sacked)
	}

	// The human manager is unemployed; the club is back under AI control.
	var status string
	var clubID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT status, current_club_id FROM manager.managers WHERE id = $1`, w.HumanMgr).
		Scan(&status, &clubID); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	if status != "unemployed" || clubID != nil {
		t.Errorf("after sack manager = %s club=%v, want unemployed nil", status, clubID)
	}
	var aiControlled bool
	if err := pool.QueryRow(ctx, `SELECT is_ai_controlled FROM club.clubs WHERE id = $1`, w.HumanClub).
		Scan(&aiControlled); err != nil {
		t.Fatalf("load club: %v", err)
	}
	if !aiControlled {
		t.Error("human club should be AI-controlled after sack")
	}

	// The sacking event carries the board's structured reasoning, and the
	// manager's club stays palmed off (AI managers untouched).
	var evCount int
	var explanationJSON []byte
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE((SELECT explanation FROM world.events WHERE actor_id = $1 LIMIT 1), '{}')
		FROM world.events WHERE event_type = 'MANAGER_SACKED'`).Scan(&evCount, &explanationJSON); err != nil {
		t.Fatalf("sack events: %v", err)
	}
	if evCount != 1 {
		t.Errorf("MANAGER_SACKED events = %d, want 1", evCount)
	}
	var exp struct {
		Subject string `json:"subject"`
		Score   int    `json:"score"`
	}
	if err := json.Unmarshal(explanationJSON, &exp); err != nil {
		t.Fatalf("unmarshal sack explanation: %v", err)
	}
	if exp.Subject != "board_confidence" || exp.Score > 25 {
		t.Errorf("sack explanation = %+v, want board_confidence <= 25", exp)
	}

	for _, aiMgr := range []uuid.UUID{w.AIOneMgr, w.AITwoMgr} {
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM manager.managers WHERE id = $1`, aiMgr).Scan(&st); err != nil {
			t.Fatalf("load ai manager: %v", err)
		}
		if st != "active" {
			t.Errorf("AI manager %s should stay active, got %s", aiMgr, st)
		}
	}
}

// forceLowConfidence pushes a fixture club into a sacking scenario: terrible
// league position, no points, minimal wage budget against committed wages, a
// negative operating half-year, pessimistic supporters, and a documented
// reputation hole. Constructed so the resulting confidence sits far below the
// sack threshold for every board persona.
func forceLowConfidence(t *testing.T, pool *pgxpool.Pool, w transfertest.World) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		UPDATE club.club_dna SET competitive_ambition = 80, patience = 20 WHERE club_id = $1`, w.HumanClub); err != nil {
		t.Fatalf("force dna: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE club.supporter_groups SET current_sentiment = 15 WHERE club_id = $1`, w.HumanClub); err != nil {
		t.Fatalf("force sentiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO manager.manager_reputation_events (manager_id, world_id, category, delta, reason)
		VALUES ($1, $2, 'media_presence', -100, 'test drought')`, w.HumanMgr, w.WorldID); err != nil {
		t.Fatalf("force reputation: %v", err)
	}
	// Tiny wage budget against a full drafted squad's committed wages.
	if _, err := pool.Exec(ctx, `
		UPDATE finance.budgets SET allocated_amount = 1000
		WHERE club_id = $1 AND season = $2 AND budget_type = 'wage'`, w.HumanClub, seasonYear); err != nil {
		t.Fatalf("force wage budget: %v", err)
	}
	// A single big drain this season keeps the operating balance in the red.
	var accountID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM finance.accounts WHERE club_id = $1`, w.HumanClub).Scan(&accountID); err != nil {
		t.Fatalf("load account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO finance.ledger_entries (account_id, entry_type, category, amount, description, occurred_at)
		VALUES ($1, 'debit', 'facilities', 900000, 'test drain', '2026-03-01T00:00:00Z')`, accountID); err != nil {
		t.Fatalf("force drain: %v", err)
	}

	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation)
		VALUES ($1, 'Regional Test League', 'league', 10) RETURNING id`, w.WorldID).Scan(&compID); err != nil {
		t.Fatalf("create league: %v", err)
	}
	var seasonID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.seasons (world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, '2026/27', 1, '2026-01-01', 'in_progress') RETURNING id`, w.WorldID, compID).Scan(&seasonID); err != nil {
		t.Fatalf("create season: %v", err)
	}

	clubs := []uuid.UUID{w.HumanClub, w.AIOneClub, w.AITwoClub}
	for i := 0; i < 8; i++ {
		fake := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO club.clubs (world_id, name, short_name, country, is_ai_controlled)
			VALUES ($1, $2, $3, 'england', TRUE)`,
			w.WorldID, "Filler "+fake.String()[0:6], "FLL"+fake.String()[0:3]); err != nil {
			t.Fatalf("insert filler club: %v", err)
		}
		clubs = append(clubs, fake)
	}
	for _, c := range clubs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.competition_entries (season_id, club_id) VALUES ($1, $2)`, seasonID, c); err != nil {
			t.Fatalf("entry: %v", err)
		}
		points := 0
		if c == w.HumanClub {
			points = 0
		} else {
			points = 60
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.standings (season_id, club_id, played, won, drawn, lost, goals_for, goals_against, points)
			VALUES ($1, $2, 20, $3, 0, $4, 40, 1, $5)`,
			seasonID, c, points/3, 20-points/3, points); err != nil {
			t.Fatalf("standings: %v", err)
		}
	}
}
