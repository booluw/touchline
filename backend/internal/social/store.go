package social

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
)

// managerBaseRow is the manager row + a display name resolved from the person
// record (person_id is null for pure AI actors → empty name, caller falls back).
type managerBaseRow struct {
	worldID uuid.UUID
	status  string
	isBot   bool
	clubID  *uuid.UUID
	name    string
}

// loadManagerBase reads the manager's world, status, bot flag, active club and
// display name. Personless AI managers yield an empty name.
func (s *Service) loadManagerBase(ctx context.Context, managerID uuid.UUID) (*managerBaseRow, error) {
	m := &managerBaseRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT m.world_id, m.status, m.is_policy_bot, m.current_club_id,
		       COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), '')
		FROM manager.managers m
		LEFT JOIN person.people p ON p.id = m.person_id
		WHERE m.id = $1`, managerID,
	).Scan(&m.worldID, &m.status, &m.isBot, &m.clubID, &m.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrManagerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load manager base: %w", err)
	}
	return m, nil
}

// loadClubRef resolves the compact club identity used on profiles.
func (s *Service) loadClubRef(ctx context.Context, clubID uuid.UUID) (*ClubRef, error) {
	ref := &ClubRef{ID: clubID}
	err := s.pool.QueryRow(ctx,
		`SELECT name, country FROM club.clubs WHERE id = $1`, clubID,
	).Scan(&ref.Name, &ref.Country)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load club ref: %w", err)
	}
	return ref, nil
}

// careerSummary totals managed matches across every club in the manager's
// employment history, attributed by date overlap with completed fixtures.
func (s *Service) careerSummary(ctx context.Context, managerID uuid.UUID) (CareerSummary, error) {
	var out CareerSummary
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       COUNT(*) FILTER (WHERE (f.home_club_id = h.club_id AND f.ht_score > f.at_score)
		                           OR (f.away_club_id = h.club_id AND f.at_score > f.ht_score))::int,
		       COUNT(*) FILTER (WHERE f.ht_score = f.at_score)::int,
		       COUNT(*) FILTER (WHERE (f.home_club_id = h.club_id AND f.ht_score < f.at_score)
		                           OR (f.away_club_id = h.club_id AND f.at_score < f.ht_score))::int,
		       COALESCE(SUM(CASE WHEN f.home_club_id = h.club_id THEN f.ht_score ELSE f.at_score END), 0)::int,
		       COALESCE(SUM(CASE WHEN f.home_club_id = h.club_id THEN f.at_score ELSE f.ht_score END), 0)::int
		FROM manager.manager_history h
		JOIN match.fixtures f ON (f.home_club_id = h.club_id OR f.away_club_id = h.club_id)
		   AND f.status = 'completed' AND f.ht_score IS NOT NULL AND f.at_score IS NOT NULL
		   AND f.scheduled_at::date >= h.start_date
		   AND (h.end_date IS NULL OR f.scheduled_at::date < h.end_date)
		WHERE h.manager_id = $1`, managerID,
	).Scan(&out.Matches, &out.Wins, &out.Draws, &out.Losses, &out.GoalsFor, &out.GoalsAgainst)
	if err != nil {
		return out, fmt.Errorf("career summary: %w", err)
	}
	return out, nil
}

// trophies lists club.club_history trophy rows for every club the manager ran.
func (s *Service) trophies(ctx context.Context, managerID uuid.UUID) ([]Trophy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ch.season, ch.description, c.name, ch.occurred_at
		FROM club.club_history ch
		JOIN club.clubs c ON c.id = ch.club_id
		WHERE ch.event_type = 'trophy'
		  AND ch.club_id IN (SELECT club_id FROM manager.manager_history WHERE manager_id = $1)
		ORDER BY ch.season DESC`, managerID)
	if err != nil {
		return nil, fmt.Errorf("trophies: %w", err)
	}
	defer rows.Close()

	var out []Trophy
	for rows.Next() {
		var t Trophy
		if err := rows.Scan(&t.Season, &t.Description, &t.ClubName, &t.WonAt); err != nil {
			return nil, fmt.Errorf("scan trophy: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// trustScore is the on-the-fly SUM of the manager's trust_events deltas.
func (s *Service) trustScore(ctx context.Context, managerID uuid.UUID) (int, error) {
	var score int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, managerID,
	).Scan(&score)
	if err != nil {
		return 0, fmt.Errorf("trust score: %w", err)
	}
	return score, nil
}

// h2hRecord sums completed league fixtures between the viewer's club and the
// target's club, framed from the viewer's perspective. Returns nil when the
// pair has never played.
func (s *Service) h2hRecord(ctx context.Context, viewerClubID, targetClubID uuid.UUID) (*H2HRecord, error) {
	var out H2HRecord
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       COUNT(*) FILTER (WHERE (f.home_club_id = $1 AND f.ht_score > f.at_score)
		                           OR (f.away_club_id = $1 AND f.at_score > f.ht_score))::int,
		       COUNT(*) FILTER (WHERE f.ht_score = f.at_score)::int,
		       COUNT(*) FILTER (WHERE (f.home_club_id = $1 AND f.ht_score < f.at_score)
		                           OR (f.away_club_id = $1 AND f.at_score < f.ht_score))::int,
		       COALESCE(SUM(CASE WHEN f.home_club_id = $1 THEN f.ht_score ELSE f.at_score END), 0)::int,
		       COALESCE(SUM(CASE WHEN f.home_club_id = $1 THEN f.at_score ELSE f.ht_score END), 0)::int
		FROM match.fixtures f
		WHERE f.status = 'completed' AND f.ht_score IS NOT NULL AND f.at_score IS NOT NULL
		  AND ((f.home_club_id = $1 AND f.away_club_id = $2)
		       OR (f.home_club_id = $2 AND f.away_club_id = $1))`,
		viewerClubID, targetClubID,
	).Scan(&out.Matches, &out.Wins, &out.Draws, &out.Losses, &out.GoalsFor, &out.GoalsAgainst)
	if err != nil {
		return nil, fmt.Errorf("head-to-head: %w", err)
	}
	if out.Matches == 0 {
		return nil, nil
	}
	return &out, nil
}

// rivalEdges returns the graph edges attached to one entity (manager or club),
// resolving the other side's display name. Only manager/club peers are shown —
// player sentiment lives on the S06-03 morale surface. Edges are stored
// canonically (entity_a_id <= entity_b_id by UUID order) so the peer can sit on
// either side; the query matches both and renders whichever is NOT the viewer.
func (s *Service) rivalEdges(ctx context.Context, worldID uuid.UUID, entityID uuid.UUID, entityType string) ([]RivalEdge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.relationship_type,
		       CASE WHEN r.entity_a_id = $3 AND r.entity_a_type = $2
		            THEN r.entity_b_type ELSE r.entity_a_type END,
		       CASE WHEN r.entity_a_id = $3 AND r.entity_a_type = $2
		            THEN r.entity_b_id ELSE r.entity_a_id END,
		       r.strength, r.trust, r.sentiment,
		       COALESCE(
		           CASE WHEN r.entity_a_id = $3 AND r.entity_a_type = $2
		                THEN CASE WHEN r.entity_b_type = 'manager'
		                          THEN (SELECT COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), '')
		                                FROM manager.managers m2
		                                LEFT JOIN person.people p ON p.id = m2.person_id
		                                WHERE m2.id = r.entity_b_id)
		                          WHEN r.entity_b_type = 'club'
		                          THEN (SELECT name FROM club.clubs WHERE id = r.entity_b_id)
		                          ELSE NULL END
		                ELSE CASE WHEN r.entity_a_type = 'manager'
		                          THEN (SELECT COALESCE(p.first_name || COALESCE(' ' || p.last_name, ''), '')
		                                FROM manager.managers m2
		                                LEFT JOIN person.people p ON p.id = m2.person_id
		                                WHERE m2.id = r.entity_a_id)
		                          WHEN r.entity_a_type = 'club'
		                          THEN (SELECT name FROM club.clubs WHERE id = r.entity_a_id)
		                          ELSE NULL END END,
		           '')
		FROM social.relationships r
		WHERE r.world_id = $1
		  AND ((r.entity_a_id = $3 AND r.entity_a_type = $2)
		       OR (r.entity_b_id = $3 AND r.entity_b_type = $2))
		  AND r.entity_a_type IN ('manager', 'club') AND r.entity_b_type IN ('manager', 'club')
		ORDER BY abs(r.strength) DESC, r.strength DESC`, worldID, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("rival edges: %w", err)
	}
	defer rows.Close()

	var out []RivalEdge
	for rows.Next() {
		var e RivalEdge
		if err := rows.Scan(&e.RelationshipType, &e.EntityType, &e.EntityID, &e.Strength, &e.Trust, &e.Sentiment, &e.EntityName); err != nil {
			return nil, fmt.Errorf("scan rival edge: %w", err)
		}
		e.Entity = &apiref.EntityRef{ID: e.EntityID, Name: e.EntityName, Type: e.EntityType}
		out = append(out, e)
	}
	return out, rows.Err()
}
