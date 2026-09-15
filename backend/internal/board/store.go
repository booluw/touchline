package board

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier is the read/write surface shared by the pool and transaction paths.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// reviewInputs is everything the scoring engine reads for one club+manager.
type reviewInputs struct {
	clubID         uuid.UUID
	managerID      uuid.UUID
	season         int
	persona        Persona
	ambition       int
	patience       int
	sentiment      int
	reputation     int
	hasLeague      bool
	position       *int
	points         *int
	played         int
	totalMatches   int
	seasonComplete bool

	operatingProfit int64
	wageBudget      int64
	committedAnnual int64

	mandates []Mandate
}

// Store reads board state and writes snapshots/mandates. It references only
// existing tables; nothing in this package mutates non-board state directly.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a board store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// seasonYear returns the calendar year of the world's current in-game date.
func (s *Store) seasonYear(ctx context.Context, q querier, worldID uuid.UUID) (int, error) {
	var y int
	err := q.QueryRow(ctx, `
		SELECT EXTRACT(YEAR FROM COALESCE(launched_at, created_at) + current_day * INTERVAL '1 day')::int
		FROM world.worlds WHERE id = $1`, worldID).Scan(&y)
	if err != nil {
		return 0, fmt.Errorf("season year: %w", err)
	}
	return y, nil
}

// managerClub resolves the manager's current assignment (world, club, bot flag).
func (s *Store) managerClub(ctx context.Context, q querier, managerID uuid.UUID) (worldID, clubID uuid.UUID, bot bool, err error) {
	err = q.QueryRow(ctx, `
		SELECT world_id, current_club_id, is_policy_bot
		FROM manager.managers WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`,
		managerID).Scan(&worldID, &clubID, &bot)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, false, ErrNotEmployed
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, fmt.Errorf("manager club: %w", err)
	}
	return worldID, clubID, bot, nil
}

// loadReview collects the determinants for one club+manager into
// reviewInputs. Missing optional rows resolve to documented neutral defaults.
func (s *Store) loadReview(ctx context.Context, q querier, worldID, clubID, managerID uuid.UUID, season int) (reviewInputs, error) {
	in := reviewInputs{clubID: clubID, managerID: managerID, season: season, ambition: 50, patience: 50, sentiment: 50, persona: PersonaPatientOwner}

	if err := q.QueryRow(ctx, `
		SELECT competitive_ambition, patience FROM club.club_dna WHERE club_id = $1`, clubID).
		Scan(&in.ambition, &in.patience); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, fmt.Errorf("club dna: %w", err)
	}
	if err := q.QueryRow(ctx, `
		SELECT personality_type FROM club.boards WHERE club_id = $1`, clubID).
		Scan(&in.persona); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, fmt.Errorf("board persona: %w", err)
	}
	if err := q.QueryRow(ctx, `
		SELECT current_sentiment FROM club.supporter_groups WHERE club_id = $1`, clubID).
		Scan(&in.sentiment); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, fmt.Errorf("supporter sentiment: %w", err)
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(delta), 0) FROM manager.manager_reputation_events
		WHERE manager_id = $1 AND world_id = $2`, managerID, worldID).Scan(&in.reputation); err != nil {
		return in, fmt.Errorf("world reputation: %w", err)
	}

	if err := s.loadLeague(ctx, q, clubID, &in); err != nil {
		return in, err
	}
	if err := s.loadFinances(ctx, q, clubID, season, &in); err != nil {
		return in, err
	}
	mandates, err := s.listMandates(ctx, q, clubID, managerID, season)
	if err != nil {
		return in, err
	}
	in.mandates = mandates
	return in, nil
}

// loadLeague populates the standings slice of the inputs; a club with no
// domestic league yet is marked hasLeague=false (neutral performance).
func (s *Store) loadLeague(ctx context.Context, q querier, clubID uuid.UUID, in *reviewInputs) error {
	var (
		played, points, teamCount int
		seasonStatus              string
	)
	err := q.QueryRow(ctx, `
		SELECT st.played, st.points,
		       (SELECT COUNT(*) FROM competition.standings s2
		         WHERE s2.season_id = ss.id
		           AND (s2.points > st.points OR (s2.points = st.points AND s2.id < st.id))) + 1 AS position,
		       (SELECT COUNT(*) FROM competition.competition_entries e WHERE e.season_id = ss.id) AS team_count,
		       ss.status
		FROM competition.standings st
		JOIN competition.seasons ss ON ss.id = st.season_id
		JOIN competition.competitions c ON c.id = ss.competition_id AND c.competition_type = 'league'
		WHERE st.club_id = $1 AND ss.status IN ('in_progress', 'completed')
		ORDER BY CASE ss.status WHEN 'in_progress' THEN 0 ELSE 1 END, st.played DESC
		LIMIT 1`, clubID).Scan(&played, &points, &in.position, &teamCount, &seasonStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // no league yet
	}
	if err != nil {
		return fmt.Errorf("league standings: %w", err)
	}
	pos := *in.position
	in.hasLeague = true
	in.points = &points
	in.played = played
	in.seasonComplete = seasonStatus == "completed"
	if teamCount > 1 {
		in.totalMatches = teamCount * (teamCount - 1)
	} else {
		in.totalMatches = played
	}
	if in.totalMatches < played {
		in.totalMatches = played
	}
	_ = pos
	return nil
}

// loadFinances populates the operating result and wage discipline inputs.
func (s *Store) loadFinances(ctx context.Context, q querier, clubID uuid.UUID, season int, in *reviewInputs) error {
	if season > 0 {
		start := time.Date(season, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := q.QueryRow(ctx, `
			SELECT COALESCE(
				(SELECT SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint
				 FROM finance.ledger_entries l
				 JOIN finance.accounts a ON a.id = l.account_id
				 WHERE a.club_id = $1 AND l.occurred_at >= $2
				   AND (l.dedup_key IS NULL OR l.dedup_key <> 'genesis:opening_capital')), 0)`,
			clubID, start).Scan(&in.operatingProfit); err != nil {
			return fmt.Errorf("operating profit: %w", err)
		}
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE((SELECT allocated_amount::bigint FROM finance.budgets
		                 WHERE club_id = $1 AND season = $2 AND budget_type = 'wage'), 0)`,
		clubID, season).Scan(&in.wageBudget); err != nil {
		return fmt.Errorf("wage budget: %w", err)
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE((SELECT SUM(w.weekly_wage)::bigint * 52
		                 FROM finance.wage_commitments w
		                 JOIN player.contracts c ON c.id = w.contract_id
		                 WHERE w.club_id = $1 AND c.status = 'active' AND w.end_date >= CURRENT_DATE), 0)`,
		clubID).Scan(&in.committedAnnual); err != nil {
		return fmt.Errorf("committed wage: %w", err)
	}
	return nil
}

// listMandates returns the mandates for a (club, manager, season), open first
// then by category; manager_id NULL rows (pre-assignment scaffolds) are
// excluded.
func (s *Store) listMandates(ctx context.Context, q querier, clubID, managerID uuid.UUID, season int) ([]Mandate, error) {
	rows, err := q.Query(ctx, `
		SELECT id, club_id, manager_id, season, category, description,
		       target_type, target_value, status, created_at, resolved_at
		FROM club.board_mandates
		WHERE club_id = $1 AND manager_id = $2 AND season = $3
		ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'agreed' THEN 1 ELSE 2 END,
		         CASE category WHEN 'primary' THEN 0 WHEN 'secondary' THEN 1
		                       WHEN 'strategic' THEN 2 ELSE 3 END, created_at`, clubID, managerID, season)
	if err != nil {
		return nil, fmt.Errorf("list mandates: %w", err)
	}
	defer rows.Close()
	var out []Mandate
	for rows.Next() {
		var m Mandate
		if err := rows.Scan(&m.ID, &m.ClubID, &m.ManagerID, &m.Season, &m.Category,
			&m.Description, &m.TargetType, &m.TargetValue, &m.Status, &m.CreatedAt, &m.ResolvedAt); err != nil {
			return nil, fmt.Errorf("scan mandate: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// loadMandateByID loads one mandate row across clubs/managers.
func (s *Store) loadMandateByID(ctx context.Context, q querier, id uuid.UUID) (Mandate, error) {
	var m Mandate
	err := q.QueryRow(ctx, `
		SELECT id, club_id, manager_id, season, category, description,
		       target_type, target_value, status, created_at, resolved_at
		FROM club.board_mandates WHERE id = $1`, id).
		Scan(&m.ID, &m.ClubID, &m.ManagerID, &m.Season, &m.Category,
			&m.Description, &m.TargetType, &m.TargetValue, &m.Status, &m.CreatedAt, &m.ResolvedAt)
	if err != nil {
		return m, err
	}
	return m, nil
}

// currentTick reads the world's current_tick.
func (s *Store) currentTick(ctx context.Context, q querier, worldID uuid.UUID) (int64, error) {
	var tick int64
	if err := q.QueryRow(ctx, `SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&tick); err != nil {
		return 0, fmt.Errorf("current tick: %w", err)
	}
	return tick, nil
}

// insertMandates writes a fresh mandate set for a (club, manager, season).
func (s *Store) insertMandates(ctx context.Context, q querier, clubID, managerID uuid.UUID, season int, seeds []mandateSeed) error {
	for _, sd := range seeds {
		if _, err := q.Exec(ctx, `
			INSERT INTO club.board_mandates
				(club_id, manager_id, season, category, description, target_type, target_value, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending')
			ON CONFLICT DO NOTHING`,
			clubID, managerID, season, sd.Category, sd.Description, sd.TargetType, sd.TargetValue); err != nil {
			return fmt.Errorf("insert mandate %s: %w", sd.Category, err)
		}
	}
	return nil
}

// resolveMandate marks a mandate met/broken with its resolution timestamp.
func (s *Store) resolveMandate(ctx context.Context, q querier, id uuid.UUID, status string) error {
	if _, err := q.Exec(ctx, `
		UPDATE club.board_mandates SET status = $2, resolved_at = now()
		WHERE id = $1 AND status IN ('pending', 'agreed')`, id, status); err != nil {
		return fmt.Errorf("resolve mandate: %w", err)
	}
	return nil
}

// setMandateTarget records an accepted negotiated target (kept open → agreed).
func (s *Store) setMandateTarget(ctx context.Context, q querier, id uuid.UUID, targetValue string) error {
	if _, err := q.Exec(ctx, `
		UPDATE club.board_mandates SET target_value = $2, status = 'agreed'
		WHERE id = $1 AND status IN ('pending', 'agreed')`, id, targetValue); err != nil {
		return fmt.Errorf("set mandate target: %w", err)
	}
	return nil
}

// updateSentiment persists the blended supporter sentiment.
func (s *Store) updateSentiment(ctx context.Context, q querier, clubID uuid.UUID, sentiment int) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO club.supporter_groups (club_id, patience, ambition, loyalty, identity,
		                                   rivalry_intensity_base, financial_sensitivity, current_sentiment)
		VALUES ($1, 60, 60, 70, 'local', 0, 50, $2)
		ON CONFLICT (club_id) DO UPDATE SET current_sentiment = EXCLUDED.current_sentiment`,
		clubID, sentiment); err != nil {
		return fmt.Errorf("update sentiment: %w", err)
	}
	return nil
}

// snapshotExists reports whether a snapshot already exists for the tick
// (idempotent weekly replay + read-triggered reviews).
func (s *Store) snapshotExists(ctx context.Context, q querier, managerID uuid.UUID, tick int64) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM manager.job_security_snapshots
		               WHERE manager_id = $1 AND world_tick = $2)`, managerID, tick).Scan(&exists); err != nil {
		return false, fmt.Errorf("snapshot exists: %w", err)
	}
	return exists, nil
}

// writeSnapshot inserts one job-security snapshot row.
func (s *Store) writeSnapshot(ctx context.Context, q querier, snap Snapshot) error {
	_, err := q.Exec(ctx, `
		INSERT INTO manager.job_security_snapshots
			(manager_id, club_id, world_tick,
			 performance_score, expectations_score, financial_score,
			 board_relationship_score, club_dna_alignment_score,
			 supporter_sentiment_score, alternatives_score, total_score, explanation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		snap.ManagerID, snap.ClubID, snap.WorldTick,
		snap.Scores.Performance, snap.Scores.Expectations, snap.Scores.Financial,
		snap.Scores.BoardRelationship, snap.Scores.ClubDNAAlignment,
		snap.Scores.SupporterSentiment, snap.Scores.Alternatives, snap.Scores.Total,
		snap.Explanation)
	if err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// latestSnapshot returns the most recent snapshot row for a manager, or nil.
func (s *Store) latestSnapshot(ctx context.Context, q querier, managerID uuid.UUID) (*Snapshot, error) {
	var snap Snapshot
	var rawJSON []byte
	err := q.QueryRow(ctx, `
		SELECT manager_id, club_id, world_tick,
		       performance_score, expectations_score, financial_score,
		       board_relationship_score, club_dna_alignment_score,
		       supporter_sentiment_score, alternatives_score, total_score, explanation
		FROM manager.job_security_snapshots
		WHERE manager_id = $1 ORDER BY world_tick DESC, created_at DESC LIMIT 1`, managerID).
		Scan(&snap.ManagerID, &snap.ClubID, &snap.WorldTick,
			&snap.Scores.Performance, &snap.Scores.Expectations, &snap.Scores.Financial,
			&snap.Scores.BoardRelationship, &snap.Scores.ClubDNAAlignment,
			&snap.Scores.SupporterSentiment, &snap.Scores.Alternatives, &snap.Scores.Total,
			&rawJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest snapshot: %w", err)
	}
	snap.Explanation = rawJSON
	return &snap, nil
}
