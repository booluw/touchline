// Persistence for the absence-delegation engine: the manager.policies rows,
// the manager-side absence state (away_since / away_auto /
// consecutive_missed / last_activity_at), and the per-world absence bot.
package policybot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/transfer"
)

// Store is the policybot persistence layer.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds the policybot store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrNoPolicy is returned when a manager has no saved row for a policy type.
var ErrNoPolicy = errors.New("no policy saved for this type")

// LoadAbsence reads a manager's absence state columns.
func (s *Store) LoadAbsence(ctx context.Context, managerID uuid.UUID) (AbsenceState, error) {
	var (
		row       AbsenceState
		awaySince *time.Time
		lastAct   *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT away_since, away_auto, consecutive_missed, last_activity_at
		FROM manager.managers WHERE id = $1`, managerID).
		Scan(&awaySince, &row.AwayAuto, &row.ConsecutiveMissed, &lastAct)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrManagerNotFound
	}
	if err != nil {
		return row, fmt.Errorf("load absence: %w", err)
	}
	row.AwaySince = awaySince
	row.LastActivityAt = lastAct
	return row, nil
}

// SetExplicitAway flips the manager's away mode on or off. Explicit mode is
// away_auto=FALSE; missing streak is reset on either transition so a streak
// never leaks across a manual toggle.
func (s *Store) SetExplicitAway(ctx context.Context, managerID uuid.UUID, away bool) error {
	var awaySince any
	if away {
		awaySince = time.Now()
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE manager.managers
		SET away_since = $2, away_auto = FALSE, consecutive_missed = 0
		WHERE id = $1`, managerID, awaySince)
	if err != nil {
		return fmt.Errorf("set explicit away: %w", err)
	}
	return nil
}

// TouchActivity is the authenticated-request heartbeat. It records the
// manager as present (clearing any auto-away and the missed streak) but is
// throttled so a busy API session does not write every request. Explicit away
// is preserved until the manager toggles it off (decision: activity clears
// auto-away only — a manual "I'm away" stays until revoked).
func (s *Store) TouchActivity(ctx context.Context, managerID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE manager.managers
		SET last_activity_at = now(),
		    consecutive_missed = 0,
		    away_since = CASE WHEN away_auto THEN NULL ELSE away_since END,
		    away_auto = FALSE
		WHERE id = $1
		  AND (last_activity_at IS NULL OR last_activity_at < now() - make_interval(hours => $2))`,
		managerID, int(HeartbeatInterval.Hours()))
	if err != nil {
		return fmt.Errorf("touch activity: %w", err)
	}
	return nil
}

// AttendOrMiss tallies one club fixture against the streak: an attended
// fixture (the manager was active at or after the previous kickoff) resets the
// count; an unattended one increments it. When the streak crosses
// MissedFixtureThreshold and the manager is not already away, auto-away
// activates (away_auto=TRUE). Returns whether auto-away just activated.
func (s *Store) AttendOrMiss(ctx context.Context, managerID uuid.UUID, prevKickoff *time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("attend/miss: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var awaySince *time.Time
	var awayAuto bool
	var missed int
	if err := tx.QueryRow(ctx, `
		SELECT away_since, away_auto, consecutive_missed
		FROM manager.managers WHERE id = $1 FOR UPDATE`, managerID).
		Scan(&awaySince, &awayAuto, &missed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrManagerNotFound
		}
		return false, fmt.Errorf("attend/miss: load: %w", err)
	}
	// Already away (explicit or auto): delegation is active; the streak is
	// frozen. Nothing to tally — the engine's next decisions all go through
	// the away path regardless.
	if awaySince != nil {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("attend/miss: commit: %w", err)
		}
		return false, nil
	}
	// First fixture of the season for a manager with the season already
	// rolling elsewhere: prevKickoff is nil only when the club played nothing
	// before, which counts as attended (no streak can be built from nothing).
	active := false
	if prevKickoff != nil {
		var lastAct *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT last_activity_at FROM manager.managers WHERE id = $1`, managerID).Scan(&lastAct); err != nil {
			return false, fmt.Errorf("attend/miss: activity: %w", err)
		}
		active = lastAct != nil && !lastAct.Before(*prevKickoff)
	}
	if active {
		_, err = tx.Exec(ctx,
			`UPDATE manager.managers SET consecutive_missed = 0 WHERE id = $1`, managerID)
		if err != nil {
			return false, fmt.Errorf("attend/miss: reset streak: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("attend/miss: commit: %w", err)
		}
		return false, nil
	}
	missed++
	activated := missed >= MissedFixtureThreshold
	_, err = tx.Exec(ctx, `
		UPDATE manager.managers
		SET consecutive_missed = $2,
		    away_since = CASE WHEN $3 THEN now() ELSE away_since END,
		    away_auto = CASE WHEN $3 THEN TRUE ELSE away_auto END
		WHERE id = $1`, managerID, missed, activated)
	if err != nil {
		return false, fmt.Errorf("attend/miss: increment streak: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("attend/miss: commit: %w", err)
	}
	return activated, nil
}

// GetOrCreateAbsenceBot returns the world's unemployment bot that performs
// delegated acts. There is one per world (partial unique index
// uq_managers_world_policy_bot over is_policy_bot + no club), created lazily
// and idempotently.

// UpsertPolicy saves (or replaces) one manager policy. Params ride as JSONB;
// the row is actor-stamped for the audit trail.
func (s *Store) UpsertPolicy(ctx context.Context, p Policy, actorType string, actorID uuid.UUID) (Policy, error) {
	row := p
	err := s.pool.QueryRow(ctx, `
		INSERT INTO manager.policies
			(world_id, manager_id, policy_type, params, enabled,
			 updated_by_actor_type, updated_by_actor_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (manager_id, policy_type) DO UPDATE SET
			params = EXCLUDED.params,
			enabled = EXCLUDED.enabled,
			updated_by_actor_type = EXCLUDED.updated_by_actor_type,
			updated_by_actor_id = EXCLUDED.updated_by_actor_id,
			updated_at = now()
		RETURNING world_id, manager_id, policy_type, params, enabled, updated_at`,
		p.WorldID, p.ManagerID, p.Type, p.Params, p.Enabled, actorType, actorID,
	).Scan(&row.WorldID, &row.ManagerID, &row.Type, &row.Params, &row.Enabled, &row.UpdatedAt)
	if err != nil {
		return row, fmt.Errorf("upsert policy: %w", err)
	}
	return row, nil
}

// GetPolicy reads one saved policy row (ErrNoPolicy when none exists).
func (s *Store) GetPolicy(ctx context.Context, managerID uuid.UUID, policyType string) (Policy, error) {
	var p Policy
	err := s.pool.QueryRow(ctx, `
		SELECT world_id, manager_id, policy_type, params, enabled, updated_at
		FROM manager.policies WHERE manager_id = $1 AND policy_type = $2`, managerID, policyType).
		Scan(&p.WorldID, &p.ManagerID, &p.Type, &p.Params, &p.Enabled, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNoPolicy
	}
	if err != nil {
		return p, fmt.Errorf("get policy: %w", err)
	}
	return p, nil
}

// ListPolicies returns every saved policy of a manager (sorted by type).
func (s *Store) ListPolicies(ctx context.Context, managerID uuid.UUID) ([]Policy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT world_id, manager_id, policy_type, params, enabled, updated_at
		FROM manager.policies WHERE manager_id = $1 ORDER BY policy_type`, managerID)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()
	var out []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.WorldID, &p.ManagerID, &p.Type, &p.Params, &p.Enabled, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policies: %w", err)
	}
	if out == nil {
		out = []Policy{}
	}
	return out, nil
}

// DeletePolicy removes a saved policy row. Deleting an absent row is a no-op.
func (s *Store) DeletePolicy(ctx context.Context, managerID uuid.UUID, policyType string) (found bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM manager.policies WHERE manager_id = $1 AND policy_type = $2`,
		managerID, policyType)
	if err != nil {
		return false, fmt.Errorf("delete policy: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// FixtureSnapshot is the minimal fixture identity needed by the match seam.
type FixtureSnapshot struct {
	ID          uuid.UUID
	WorldID     uuid.UUID
	HomeClubID  uuid.UUID
	AwayClubID  uuid.UUID
	ScheduledAt time.Time
	Status      string
}

// LoadFixture reads a match fixture row.
func (s *Store) LoadFixture(ctx context.Context, fixtureID uuid.UUID) (FixtureSnapshot, error) {
	var f FixtureSnapshot
	err := s.pool.QueryRow(ctx, `
		SELECT id, world_id, home_club_id, away_club_id, scheduled_at, status
		FROM match.fixtures WHERE id = $1`, fixtureID).
		Scan(&f.ID, &f.WorldID, &f.HomeClubID, &f.AwayClubID, &f.ScheduledAt, &f.Status)
	if err != nil {
		return f, fmt.Errorf("load fixture %s: %w", fixtureID, err)
	}
	return f, nil
}

// PrevClubKickoff returns the kickoff time of the most recent completed
// fixture played by the club before the reference kickoff (nil when the club
// has no prior completed fixture).
func (s *Store) PrevClubKickoff(ctx context.Context, clubID uuid.UUID, before time.Time) (*time.Time, error) {
	var kickoff time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT scheduled_at FROM match.fixtures
		WHERE (home_club_id = $1 OR away_club_id = $1)
		  AND status = 'completed'
		  AND scheduled_at < $2
		ORDER BY scheduled_at DESC
		LIMIT 1`, clubID, before).Scan(&kickoff)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prev club kickoff %s: %w", clubID, err)
	}
	return &kickoff, nil
}

// NextClubFixture returns the next scheduled kickoff for the club at/after now
// (nil when none is scheduled).
func (s *Store) NextClubFixture(ctx context.Context, clubID uuid.UUID, at time.Time) (*time.Time, error) {
	var kickoff time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT scheduled_at FROM match.fixtures
		WHERE (home_club_id = $1 OR away_club_id = $1)
		  AND status = 'scheduled'
		  AND scheduled_at >= $2
		ORDER BY scheduled_at ASC
		LIMIT 1`, clubID, at).Scan(&kickoff)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next club fixture %s: %w", clubID, err)
	}
	return &kickoff, nil
}

// ManagerForClub finds the human (non-policy-bot) manager who currently runs
// the club. Returns ErrManagerNotFound when the club is AI-run or vacant.
func (s *Store) ManagerForClub(ctx context.Context, clubID uuid.UUID) (uuid.UUID, error) {
	var managerID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM manager.managers
		WHERE current_club_id = $1 AND is_policy_bot = FALSE
		LIMIT 1`, clubID).Scan(&managerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrManagerNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("manager for club %s: %w", clubID, err)
	}
	return managerID, nil
}

// ClubsForManager returns the club IDs currently managed by the manager.
func (s *Store) ClubsForManager(ctx context.Context, managerID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND current_club_id IS NOT NULL`, managerID)
	if err != nil {
		return nil, fmt.Errorf("clubs for manager %s: %w", managerID, err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var c uuid.UUID
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan club: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clubs: %w", err)
	}
	return out, nil
}

// ClubsWithAwayManagers lists the club IDs of human managers currently in away
// mode within a world (both explicit and auto).
func (s *Store) ClubsWithAwayManagers(ctx context.Context, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE world_id = $1
		  AND is_policy_bot = FALSE
		  AND away_since IS NOT NULL
		  AND current_club_id IS NOT NULL`, worldID)
	if err != nil {
		return nil, fmt.Errorf("clubs with away managers: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var c uuid.UUID
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan away club: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate away clubs: %w", err)
	}
	return out, nil
}

// PendingSellerBids lists open (negotiating) bids on the club's own listings
// — the seller-side responses the bot automates. Only bids awaiting the
// seller (status 'pending') are actionable.
func (s *Store) PendingSellerBids(ctx context.Context, clubID uuid.UUID) ([]transfer.Bid, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, world_id, listing_id, player_id, bidding_club_id, selling_club_id,
		       fee, weekly_wage, contract_length_months, signing_bonus, release_clause,
		       round, proposed_by, status, created_at
		FROM transfer.bids
		WHERE selling_club_id = $1 AND status = 'pending'`, clubID)
	if err != nil {
		return nil, fmt.Errorf("pending seller bids %s: %w", clubID, err)
	}
	defer rows.Close()
	var out []transfer.Bid
	for rows.Next() {
		var b transfer.Bid
		var contract, round int
		if err := rows.Scan(&b.ID, &b.WorldID, &b.ListingID, &b.PlayerID, &b.BiddingClubID,
			&b.SellingClubID, &b.Fee, &b.WeeklyWage, &contract, &b.SigningBonus,
			&b.ReleaseClause, &round, &b.ProposedBy, &b.Status, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bid: %w", err)
		}
		b.ContractLengthMonths = contract
		b.Round = round
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bids: %w", err)
	}
	return out, nil
}

// PlayerAttrs loads the transfer valuation inputs for one player, mirroring
// the transfer package's canonical attrs query so the bot's accept/reject math
// uses the same numbers the human-facing market does.
func (s *Store) PlayerAttrs(ctx context.Context, playerID uuid.UUID) (transfer.PlayerAttrs, error) {
	var a transfer.PlayerAttrs
	err := s.pool.QueryRow(ctx, `
		SELECT pl.id, pl.primary_position, pl.market_value,
		       (CURRENT_DATE - pp.date_of_birth) / 365,
		       COALESCE((SELECT (ct.end_date - CURRENT_DATE) FROM player.contracts ct
		                  WHERE ct.player_id = pl.id AND ct.status = 'active'
		                  ORDER BY ct.start_date DESC LIMIT 1), 0),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'technical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'physical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'mental'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'tactical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'goalkeeping'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'positional'), 50),
		       COALESCE((SELECT w.weekly_wage::bigint FROM finance.wage_commitments w
		                  JOIN player.contracts ct ON ct.id = w.contract_id AND ct.status = 'active'
		                  WHERE ct.player_id = pl.id ORDER BY ct.start_date DESC LIMIT 1), 0)
		FROM player.players pl
		JOIN person.people pp ON pp.id = pl.person_id
		WHERE pl.id = $1`, playerID,
	).Scan(
		&a.PlayerID, &a.Position, &a.MarketValue, &a.Age, &a.ContractEndDays,
		&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
		&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
		&a.Wage,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, transfer.ErrPlayerNotFound
	}
	if err != nil {
		return a, fmt.Errorf("player attrs %s: %w", playerID, err)
	}
	return a, nil
}
func (s *Store) GetOrCreateAbsenceBot(ctx context.Context, worldID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO manager.managers (world_id, status, coaching_ability, risk_tolerance, is_policy_bot)
		VALUES ($1, 'unemployed', 50, 50, TRUE)
		ON CONFLICT (world_id) WHERE is_policy_bot = TRUE AND current_club_id IS NULL
		DO NOTHING
		RETURNING id`, worldID).Scan(&id)
	if err != nil {
		// Conflict path: someone else created it; fetch the existing bot.
		if errors.Is(err, pgx.ErrNoRows) {
			if err := s.pool.QueryRow(ctx, `
				SELECT id FROM manager.managers
				WHERE world_id = $1 AND is_policy_bot = TRUE AND current_club_id IS NULL
				LIMIT 1`, worldID).Scan(&id); err != nil {
				return uuid.Nil, fmt.Errorf("absence bot lookup: %w", err)
			}
			return id, nil
		}
		return uuid.Nil, fmt.Errorf("create absence bot: %w", err)
	}
	return id, nil
}
