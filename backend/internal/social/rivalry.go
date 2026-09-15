package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

// Rivalry auto-tracking numbers (S06-04c). Every completed fixture grows the
// club↔club rivalry edge (always) and the manager↔manager edge (only when both
// clubs' current managers are human). Proposal values are documented in
// docs/design/social-numerics.md; recalibration is a constant-only change.
const (
	// RivalBaseStrengthClub and RivalBaseStrengthManager are the per-meeting
	// baseline escalations.
	RivalBaseStrengthClub    = 10
	RivalBaseStrengthManager = 8
	// RivalBigMatchMultiplier scales base + decisiveness for a big match
	// (league fixture between same-country clubs). The predicate is parameterized
	// so cup stages can activate it once a round/cup phase exists.
	RivalBigMatchMultiplier = 2
	// RivalRepeatBonus lands every time a pair has already met (the rivalry is
	// now a standing one, not a novelty).
	RivalRepeatBonus = 3
	// Decisiveness scales with the goal margin: +2 per goal up to 5.
	RivalGoalPerDelta = 2
	RivalGoalDeltaCap = 5
	// RivalMaxStrength clamps a single edges's accumulated strength.
	RivalMaxStrength = 100
	// RivalStaleDays is the gap after which an untouched edge starts decaying
	// toward 0 (strength loses ~20% per touch once stale).
	RivalStaleDays = 45
	// RivalDecayShare is the integer share dropped per stale touch (1/5).
	RivalDecayShare = 5

	// TrustWin/TrustLoss move a human manager's trust score after a completed
	// fixture (win/loss; draws write nothing). Applies to both clubs' current
	// managers independently of the other side — human vs AI clubs included.
	TrustWin  = 5
	TrustLoss = -5

	// EventRelationshipChanged is the world.events outbox event written inside
	// the match-completion transaction for every completed fixture whose edges
	// moved. Its payload is RelationshipPush.
	EventRelationshipChanged = "RELATIONSHIP_CHANGED"

	// trust_events.reason values for the per-match trust deltas.
	TrustReasonWin  = "match_win"
	TrustReasonLoss = "match_loss"
)

// ChangedEdge is one relationship-graph edge created or updated by a completed
// fixture. It is the unit pushed on realtime.EventRelationshipChange and
// recorded in the RELATIONSHIP_CHANGED outbox event. Trust/sentiment are
// deliberately zero for rivalry edges: trust lives on social.trust_events and
// sentiment belongs to the player↔manager axis.
type ChangedEdge struct {
	EntityAType      string    `json:"entity_a_type"`
	EntityAID        uuid.UUID `json:"entity_a_id"`
	EntityBType      string    `json:"entity_b_type"`
	EntityBID        uuid.UUID `json:"entity_b_id"`
	RelationshipType string    `json:"relationship_type"`
	Strength         int       `json:"strength"`
	Trust            int       `json:"trust"`
	Sentiment        int       `json:"sentiment"`
}

// RelationshipPush is the payload shared by the RELATIONSHIP_CHANGED outbox
// event and the realtime envelope: the fixture pair plus every edge it moved.
type RelationshipPush struct {
	WorldID    uuid.UUID     `json:"world_id"`
	FixtureID  uuid.UUID     `json:"fixture_id"`
	HomeClubID uuid.UUID     `json:"home_club_id"`
	AwayClubID uuid.UUID     `json:"away_club_id"`
	Edges      []ChangedEdge `json:"edges"`
}

// clubMatchContext is the fixture-side resolution needed to grow edges: the
// club's country (big-match predicate) and its current manager's bot flag.
type clubMatchContext struct {
	country      string
	managerID    *uuid.UUID
	managerIsBot bool
}

// RecordCompletedMatch is the S06-04c match-completion hook. It runs inside the
// caller's open transaction tx (match.Finalize / PlayFixture pass their
// completion tx) and grows the rivalry edges + writes the trust deltas for one
// completed fixture. The caller commits; afterwards they may push the returned
// RelationshipPush through PublishRelationshipChange. Idempotent callers
// (finalize replay) must guard the fixture themselves.
func (s *Service) RecordCompletedMatch(ctx context.Context, tx pgx.Tx, worldID, fixtureID, homeClubID, awayClubID uuid.UUID, homeGoals, awayGoals int, completedAt time.Time) (*RelationshipPush, error) {
	push, err := s.recordCompletedMatch(ctx, tx, worldID, fixtureID, homeClubID, awayClubID, homeGoals, awayGoals, completedAt)
	if err != nil {
		return nil, err
	}
	return push, nil
}

// recordCompletedMatch is the transactional core shared by the match hook, the
// standalone method and the ReconcileRivalries backfill. All writes land in tx.
func (s *Service) recordCompletedMatch(ctx context.Context, tx pgx.Tx, worldID, fixtureID, homeClubID, awayClubID uuid.UUID, homeGoals, awayGoals int, completedAt time.Time) (*RelationshipPush, error) {
	home, err := s.clubMatchContext(ctx, tx, homeClubID)
	if err != nil {
		return nil, fmt.Errorf("recorded match: home club: %w", err)
	}
	away, err := s.clubMatchContext(ctx, tx, awayClubID)
	if err != nil {
		return nil, fmt.Errorf("recorded match: away club: %w", err)
	}

	bigMatch := home.country != "" && home.country == away.country && s.isLeagueFixture(ctx, tx, fixtureID)

	diff := homeGoals - awayGoals
	if diff < 0 {
		diff = -diff
	}

	clubEdge, err := s.upsertRivalryEdge(ctx, tx, worldID, homeClubID, awayClubID, "club", "club", RivalBaseStrengthClub, diff, bigMatch, completedAt)
	if err != nil {
		return nil, fmt.Errorf("recorded match: club edge: %w", err)
	}
	edges := []ChangedEdge{clubEdge}

	if home.managerID != nil && away.managerID != nil && !home.managerIsBot && !away.managerIsBot {
		managerEdge, err := s.upsertRivalryEdge(ctx, tx, worldID, *home.managerID, *away.managerID, "manager", "manager", RivalBaseStrengthManager, diff, bigMatch, completedAt)
		if err != nil {
			return nil, fmt.Errorf("recorded match: manager edge: %w", err)
		}
		edges = append(edges, managerEdge)
	}

	push := &RelationshipPush{
		WorldID:    worldID,
		FixtureID:  fixtureID,
		HomeClubID: homeClubID,
		AwayClubID: awayClubID,
		Edges:      edges,
	}
	payload, err := json.Marshal(push)
	if err != nil {
		return nil, fmt.Errorf("recorded match: marshal %s: %w", EventRelationshipChanged, err)
	}
	actorType := "system"
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: EventRelationshipChanged,
		ActorType: &actorType,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return nil, fmt.Errorf("recorded match: %s: %w", EventRelationshipChanged, err)
	}

	if err := s.applyResultTrust(ctx, tx, *home.managerID, home.managerIsBot, *away.managerID, away.managerIsBot, homeGoals, awayGoals, &e.ID); err != nil {
		return nil, fmt.Errorf("recorded match: trust: %w", err)
	}
	return push, nil
}

// clubMatchContext resolves one fixture club's country and current manager
// human/bot flag. A club with no current manager never gets a personal edge.
func (s *Service) clubMatchContext(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (clubMatchContext, error) {
	var c clubMatchContext
	err := tx.QueryRow(ctx, `
		SELECT c.country, c.current_manager_id, COALESCE(m.is_policy_bot, TRUE)
		FROM club.clubs c
		LEFT JOIN manager.managers m ON m.id = c.current_manager_id
		WHERE c.id = $1`, clubID,
	).Scan(&c.country, &c.managerID, &c.managerIsBot)
	if err != nil {
		return c, err
	}
	return c, nil
}

// isLeagueFixture reports whether the fixture belongs to a league competition.
// A fixture without a resolved competition is treated as non-big-match rather
// than failing the match completion.
func (s *Service) isLeagueFixture(ctx context.Context, tx pgx.Tx, fixtureID uuid.UUID) bool {
	var compType string
	err := tx.QueryRow(ctx, `
		SELECT c.competition_type
		FROM competition.competitions c
		JOIN match.fixtures f ON f.competition_id = c.id
		WHERE f.id = $1`, fixtureID,
	).Scan(&compType)
	if err != nil {
		return false
	}
	return compType == "league"
}

// upsertRivalryEdge grows the canonical (entity_a_id <= entity_b_id by UUID
// order) rivalry edge between two same-type entities and returns its new state.
// Existing edges first decay toward 0 when stale, then accumulate the meeting:
//
//	delta = (base + 2*min(gd,5)) × bigMatchMult [+ RepeatBonus when repeated]
//
// clamped to ±RivalMaxStrength, with last_interaction_at stamped to completedAt.
func (s *Service) upsertRivalryEdge(ctx context.Context, tx pgx.Tx, worldID, aID, bID uuid.UUID, aType, bType string, baseStrength, goalDiff int, bigMatch bool, completedAt time.Time) (ChangedEdge, error) {
	lo, hi := orderPair(aID, bID)

	var strength int
	var lastInteraction *time.Time
	err := tx.QueryRow(ctx, `
		SELECT strength, last_interaction_at FROM social.relationships
		WHERE world_id = $1 AND entity_a_id = $2 AND entity_a_type = $3
		  AND entity_b_id = $4 AND entity_b_type = $5
		  AND relationship_type = 'rivalry'
		FOR UPDATE`,
		worldID, lo, aType, hi, bType,
	).Scan(&strength, &lastInteraction)
	existed := err == nil
	if err != nil {
		if err != pgx.ErrNoRows {
			return ChangedEdge{}, err
		}
	}

	mult := 1
	if bigMatch {
		mult = RivalBigMatchMultiplier
	}
	if goalDiff > RivalGoalDeltaCap {
		goalDiff = RivalGoalDeltaCap
	}
	delta := (baseStrength + goalDiff*RivalGoalPerDelta) * mult

	if existed {
		if lastInteraction != nil && completedAt.Sub(*lastInteraction) > RivalStaleDays*24*time.Hour {
			strength -= strength / RivalDecayShare
		}
		strength += delta + RivalRepeatBonus
	} else {
		strength = delta
	}
	if strength > RivalMaxStrength {
		strength = RivalMaxStrength
	} else if strength < -RivalMaxStrength {
		strength = -RivalMaxStrength
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
			 relationship_type, strength, trust, sentiment, last_interaction_at)
		VALUES ($1, $2, $3, $4, $5, 'rivalry', $6, 0, 0, $7)
		ON CONFLICT (entity_a_id, entity_b_id, relationship_type)
		DO UPDATE SET
			strength = GREATEST(-100, LEAST(100, EXCLUDED.strength)),
			last_interaction_at = EXCLUDED.last_interaction_at`,
		worldID, lo, aType, hi, bType, strength, completedAt)
	if err != nil {
		return ChangedEdge{}, err
	}

	return ChangedEdge{
		EntityAType:      aType,
		EntityAID:        lo,
		EntityBType:      bType,
		EntityBID:        hi,
		RelationshipType: "rivalry",
		Strength:         strength,
	}, nil
}

// orderPair canonicalizes same-type edge orientation by UUID so the partial
// uniqueness (entity_a_id, entity_b_id, relationship_type) never stores both
// directions of one club pair as two rows.
func orderPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return a, b
	}
	return b, a
}

// applyResultTrust writes the winner/loser trust deltas for the fixture's human
// current managers (nil/bot managers are skipped; draws write nothing). Each
// human side is judged independently against the result.
func (s *Service) applyResultTrust(ctx context.Context, tx pgx.Tx, homeMgrID uuid.UUID, homeBot bool, awayMgrID uuid.UUID, awayBot bool, homeGoals, awayGoals int, relatedEventID *uuid.UUID) error {
	wID, wBot := homeMgrID, homeBot
	lID, lBot := awayMgrID, awayBot
	if awayGoals > homeGoals {
		wID, wBot = awayMgrID, awayBot
		lID, lBot = homeMgrID, homeBot
	} else if awayGoals == homeGoals {
		return nil
	}
	if wID != uuid.Nil && !wBot {
		if err := insertTrustEvent(ctx, tx, wID, TrustWin, TrustReasonWin, relatedEventID); err != nil {
			return err
		}
	}
	if lID != uuid.Nil && !lBot {
		if err := insertTrustEvent(ctx, tx, lID, TrustLoss, TrustReasonLoss, relatedEventID); err != nil {
			return err
		}
	}
	return nil
}

// insertTrustEvent appends one row to the append-only trust journal.
func insertTrustEvent(ctx context.Context, tx pgx.Tx, managerID uuid.UUID, delta int, reason string, relatedEventID *uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO social.trust_events (manager_id, delta, reason, related_event_id)
		VALUES ($1, $2, $3, $4)`, managerID, delta, reason, relatedEventID)
	return err
}

// PublishRelationshipChange is the best-effort realtime fan-out a match caller
// invokes after its completion commit. A missing or failing broker never fails
// the match: the graph (profile / GET /api/relationships) stays authoritative.
func (s *Service) PublishRelationshipChange(ctx context.Context, push *RelationshipPush) {
	if s.rt == nil || push == nil {
		return
	}
	ev, err := realtime.NewEvent(realtime.EventRelationshipChange, push.WorldID, push)
	if err != nil {
		return
	}
	_ = s.rt.Publish(ctx, ev)
}

// ReconcileRivalries backfills rivalries for completed fixtures that predate
// the S06-04c hook (or were raced past it), rebuilding each missed fixture in
// its own transaction. It is idempotent: a fixture whose club edge already
// carries last_interaction_at >= ended_at is never replayed (double-counting
// strength) — so once a pair has met, later manager changes are tracked from
// that manager's next fixture forward, not retroactively.
func (s *Service) ReconcileRivalries(ctx context.Context, worldID uuid.UUID) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.home_club_id, f.away_club_id, m.home_score, m.away_score, m.ended_at
		FROM match.fixtures f
		JOIN match.matches m ON m.fixture_id = f.id
		WHERE f.world_id = $1 AND f.status = 'completed' AND m.status = 'completed'
		  AND NOT EXISTS (
			SELECT 1 FROM social.relationships r
			WHERE r.world_id = f.world_id AND r.relationship_type = 'rivalry'
			  AND r.entity_a_type = 'club' AND r.entity_b_type = 'club'
			  AND ((r.entity_a_id = f.home_club_id AND r.entity_b_id = f.away_club_id)
			       OR (r.entity_a_id = f.away_club_id AND r.entity_b_id = f.home_club_id))
			  AND r.last_interaction_at >= m.ended_at
		  )`, worldID)
	if err != nil {
		return 0, fmt.Errorf("reconcile rivalries: %w", err)
	}
	defer rows.Close()

	type stale struct {
		id, home, away       uuid.UUID
		homeGoals, awayGoals int
		endedAt              time.Time
	}
	var out []stale
	for rows.Next() {
		var f stale
		var endedAt *time.Time
		if err := rows.Scan(&f.id, &f.home, &f.away, &f.homeGoals, &f.awayGoals, &endedAt); err != nil {
			return 0, fmt.Errorf("reconcile rivalries: scan: %w", err)
		}
		if endedAt == nil {
			continue
		}
		f.endedAt = *endedAt
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("reconcile rivalries: iterate: %w", err)
	}

	for _, f := range out {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return 0, fmt.Errorf("reconcile rivalries: begin: %w", err)
		}
		if _, err := s.recordCompletedMatch(ctx, tx, worldID, f.id, f.home, f.away, f.homeGoals, f.awayGoals, f.endedAt); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return 0, fmt.Errorf("reconcile rivalries: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("reconcile rivalries: commit: %w", err)
		}
	}
	return len(out), nil
}
