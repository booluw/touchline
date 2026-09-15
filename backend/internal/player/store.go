package player

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dbtx is satisfied by both *pgxpool.Pool and pgx.Tx so store queries can run
// in or out of the caller's transaction.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// moraleVars is a player's current morale, whole-season share and the
// temperament that steers how morale moves.
type moraleVars struct {
	Current  float64
	Share    float64
	Role     string
	Pro      int
	Ambition int
	Loyalty  int
	Patience int
	Ego      int
	Volatile int
}

func (m moraleVars) personality() PlayerPersonality {
	return PlayerPersonality{
		Professionalism:     m.Pro,
		Ambition:            m.Ambition,
		Loyalty:             m.Loyalty,
		Patience:            m.Patience,
		Ego:                 m.Ego,
		EmotionalVolatility: m.Volatile,
	}
}

// loadMoraleVars reads a player's morale context, defaulting every dimension
// when the player has no condition/personality rows yet.
func loadMoraleVars(ctx context.Context, q dbtx, playerID uuid.UUID) (moraleVars, error) {
	var m moraleVars
	err := q.QueryRow(ctx, `
		SELECT COALESCE(c.morale, 0.5), COALESCE(c.playing_time_pct, 0),
		       COALESCE(cr.squad_role, 'squad_player'),
		       COALESCE(ps.professionalism, 50), COALESCE(ps.ambition, 50),
		       COALESCE(ps.loyalty, 50), COALESCE(ps.patience, 50),
		       COALESCE(ps.ego, 50), COALESCE(ps.emotional_volatility, 50)
		FROM player.players p
		LEFT JOIN player.player_condition c ON c.player_id = p.id
		LEFT JOIN player.player_personality ps ON ps.player_id = p.id
		LEFT JOIN LATERAL (
			SELECT squad_role FROM player.contracts
			WHERE player_id = p.id AND status = 'active'
			ORDER BY start_date DESC LIMIT 1
		) cr ON TRUE
		WHERE p.id = $1`, playerID,
	).Scan(&m.Current, &m.Share, &m.Role, &m.Pro, &m.Ambition,
		&m.Loyalty, &m.Patience, &m.Ego, &m.Volatile)
	if errors.Is(err, pgx.ErrNoRows) {
		return moraleVars{Current: 0.5, Role: SquadRoleSquadPlayer}, nil
	}
	if err != nil {
		return moraleVars{}, err
	}
	return m, nil
}

// refreshShare computes and persists the player's whole-season share within
// the club whose shirt they currently wear (a transferred player's prior-club
// minutes never count). Scope: completed matches only — appearances and the
// club's completed-match count only ever exist for finished matches, and in
// active play those span the current season.
func refreshShare(ctx context.Context, q dbtx, playerID uuid.UUID) (float64, error) {
	var clubID uuid.UUID
	if err := q.QueryRow(ctx, `SELECT club_id FROM player.players WHERE id = $1`, playerID).Scan(&clubID); err != nil {
		return 0, err
	}
	var minutes, matches int
	err := q.QueryRow(ctx, `
		SELECT COALESCE((
				SELECT SUM(pa.minutes) FROM player.player_appearances pa
				JOIN match.matches m1 ON m1.id = pa.match_id
				JOIN match.fixtures f1 ON f1.id = m1.fixture_id
				WHERE pa.player_id = $1 AND (f1.home_club_id = $2 OR f1.away_club_id = $2)
			), 0),
		       COUNT(DISTINCT m.id)
		FROM match.matches m
		JOIN match.fixtures f ON f.id = m.fixture_id
		WHERE m.status = 'completed' AND (f.home_club_id = $2 OR f.away_club_id = $2)`,
		playerID, clubID,
	).Scan(&minutes, &matches)
	if err != nil {
		return 0, err
	}
	share := PlayingTimeRatio(minutes, matches)
	_, err = q.Exec(ctx, `
		UPDATE player.player_condition SET playing_time_pct = $2, updated_at = now()
		WHERE player_id = $1`, playerID, share)
	if err != nil {
		return 0, err
	}
	return share, nil
}

// insertAppearance records one player's appearance in a completed match.
func insertAppearance(ctx context.Context, q dbtx, matchID uuid.UUID, a Appearance) error {
	_, err := q.Exec(ctx, `
		INSERT INTO player.player_appearances (player_id, match_id, started, minutes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (player_id, match_id) DO NOTHING`,
		a.PlayerID, matchID, a.Started, a.Minutes)
	return err
}

// upsertMorale writes the player's tallied morale + share into condition,
// leaving cooldowns untouched.
func upsertMorale(ctx context.Context, q dbtx, playerID uuid.UUID, morale float64) error {
	_, err := q.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, morale, playing_time_pct, updated_at)
		VALUES ($1, $2, COALESCE((SELECT playing_time_pct FROM player.player_condition WHERE player_id = $1), 0), now())
		ON CONFLICT (player_id) DO UPDATE
		SET morale = EXCLUDED.morale, updated_at = now()`,
		playerID, morale)
	return err
}

// ---------- roster read models ----------

// squadRowsQuery lists a club's active players with their morale/role share.
func squadRows(ctx context.Context, q dbtx, clubID uuid.UUID) ([]PlayerMoraleRow, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, pe.first_name, pe.last_name, pe.display_name, p.primary_position, p.squad_number,
		       COALESCE(cr.squad_role, 'squad_player'),
		       COALESCE(c.morale, 0.5), COALESCE(c.playing_time_pct, 0)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_condition c ON c.player_id = p.id
		LEFT JOIN LATERAL (
			SELECT squad_role FROM player.contracts
			WHERE player_id = p.id AND status = 'active'
			ORDER BY start_date DESC LIMIT 1
		) cr ON TRUE
		WHERE p.club_id = $1 AND p.status = 'active'
		ORDER BY pe.display_name`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerMoraleRow
	for rows.Next() {
		var r PlayerMoraleRow
		if err := rows.Scan(&r.PlayerID, &r.FirstName, &r.LastName, &r.DisplayName,
			&r.Position, &r.SquadNumber, &r.SquadRole, &r.Morale, &r.PlayingTimePct); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// relationshipEvents returns a player's durable player↔manager memory.
func relationshipEvents(ctx context.Context, q dbtx, playerID uuid.UUID) ([]RelationshipEventRow, error) {
	rows, err := q.Query(ctx, `
		SELECT event_type, sentiment_delta, created_at
		FROM social.relationship_events
		WHERE player_id = $1
		ORDER BY created_at DESC, id DESC`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RelationshipEventRow
	for rows.Next() {
		var r RelationshipEventRow
		if err := rows.Scan(&r.EventType, &r.SentimentDelta, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// latestTransferRequest returns the most recent request for a player.
func latestTransferRequest(ctx context.Context, q dbtx, playerID uuid.UUID) (*TransferRequest, error) {
	row := q.QueryRow(ctx, `
		SELECT id, player_id, club_id, manager_id, status, reason, created_at, resolved_at, reassured_until
		FROM player.player_transfer_requests
		WHERE player_id = $1
		ORDER BY created_at DESC LIMIT 1`, playerID)
	var r TransferRequest
	if err := row.Scan(&r.ID, &r.PlayerID, &r.ClubID, &r.ManagerID, &r.Status, &r.Reason,
		&r.CreatedAt, &r.ResolvedAt, &r.ReassuredUntil); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &r, nil
}

// openTransferRequest returns the pending request for a player, if any.
func openTransferRequest(ctx context.Context, q dbtx, playerID uuid.UUID) (*TransferRequest, error) {
	row := q.QueryRow(ctx, `
		SELECT id, player_id, club_id, manager_id, status, reason, created_at, resolved_at, reassured_until
		FROM player.player_transfer_requests
		WHERE player_id = $1 AND status = 'pending'`, playerID)
	var r TransferRequest
	if err := row.Scan(&r.ID, &r.PlayerID, &r.ClubID, &r.ManagerID, &r.Status, &r.Reason,
		&r.CreatedAt, &r.ResolvedAt, &r.ReassuredUntil); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &r, nil
}

// transferRequestByID loads one request by id, optionally FOR UPDATE.
func transferRequestByID(ctx context.Context, q dbtx, id uuid.UUID, forUpdate bool) (*TransferRequest, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	row := q.QueryRow(ctx, `
		SELECT id, player_id, club_id, manager_id, status, reason, created_at, resolved_at, reassured_until
		FROM player.player_transfer_requests WHERE id = $1`+lock, id)
	var r TransferRequest
	if err := row.Scan(&r.ID, &r.PlayerID, &r.ClubID, &r.ManagerID, &r.Status, &r.Reason,
		&r.CreatedAt, &r.ResolvedAt, &r.ReassuredUntil); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRequestNotFound
	} else if err != nil {
		return nil, err
	}
	return &r, nil
}

func insertRequest(ctx context.Context, q dbtx, r *TransferRequest) error {
	return q.QueryRow(ctx, `
		INSERT INTO player.player_transfer_requests
			(player_id, club_id, manager_id, status, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		r.PlayerID, r.ClubID, r.ManagerID, r.Status, r.Reason, r.CreatedAt,
	).Scan(&r.ID)
}

// resolveRequest flips an open request's status (and resolves it today).
func resolveRequest(ctx context.Context, q dbtx, id uuid.UUID, status string) error {
	_, err := q.Exec(ctx, `
		UPDATE player.player_transfer_requests
		SET status = $2, resolved_at = now()
		WHERE id = $1 AND status = 'pending'`, id, status)
	return err
}

// reassureRequest resolves an open request as reassured with its honour window.
func reassureRequest(ctx context.Context, q dbtx, id uuid.UUID, reassureDays int) error {
	_, err := q.Exec(ctx, `
		UPDATE player.player_transfer_requests
		SET status = 'reassured',
		    resolved_at = now(),
		    reassured_until = now() + make_interval(days => $2)
		WHERE id = $1 AND status = 'pending'`, id, reassureDays)
	return err
}

func setCooldown(ctx context.Context, q dbtx, playerID uuid.UUID, days int, unset bool) error {
	if unset {
		_, err := q.Exec(ctx, `
			INSERT INTO player.player_condition (player_id, transfer_request_cooldown_until, updated_at)
			VALUES ($1, $2, now())
			ON CONFLICT (player_id) DO UPDATE SET transfer_request_cooldown_until = NULL, updated_at = now()`,
			playerID, nil)
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, transfer_request_cooldown_until, updated_at)
		VALUES ($1, now() + make_interval(days => $2), now())
		ON CONFLICT (player_id) DO UPDATE
		SET transfer_request_cooldown_until = EXCLUDED.transfer_request_cooldown_until, updated_at = now()`,
		playerID, days)
	return err
}

// cooldownPast reports whether the transfer-request cooldown has lapsed.
func cooldownPast(ctx context.Context, q dbtx, playerID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT transfer_request_cooldown_until IS NULL OR transfer_request_cooldown_until <= now()
			FROM player.player_condition WHERE player_id = $1), TRUE)`, playerID).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// recordRelationshipEvent appends to the journal and accumulates onto the
// canonical player↔manager sentiment row (history + memory).
func recordRelationshipEvent(ctx context.Context, q dbtx, worldID, playerID, managerID uuid.UUID, eventType string, delta int, relatedEventID *uuid.UUID) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO social.relationship_events
			(world_id, player_id, manager_id, event_type, sentiment_delta, related_event_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		worldID, playerID, managerID, eventType, delta, relatedEventID); err != nil {
		return err
	}
	typeName := "professional_respect"
	if delta < 0 {
		typeName = "dislike"
	}
	_, err := q.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
			 relationship_type, strength, trust, sentiment, last_interaction_at)
		VALUES ($1, $2, 'player', $3, 'manager', $4, $5, 0, $5, now())
		ON CONFLICT (entity_a_id, entity_b_id)
		WHERE entity_a_type = 'player' AND entity_b_type = 'manager'
		DO UPDATE SET
			sentiment = GREATEST(-100, LEAST(100, social.relationships.sentiment + EXCLUDED.sentiment)),
			relationship_type = CASE
				WHEN social.relationships.sentiment + EXCLUDED.sentiment < 0 THEN 'dislike'
				ELSE 'professional_respect' END,
			last_interaction_at = EXCLUDED.last_interaction_at`,
		worldID, playerID, managerID, typeName, delta)
	return err
}

// gameRow is one roster player for the weekly pass.
type gameRow struct {
	PlayerID uuid.UUID
	Role     string
	Morale   float64
	Share    float64
	Pro      int
}

func weeklyRoster(ctx context.Context, q dbtx, clubID uuid.UUID) ([]gameRow, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id,
		       COALESCE(cr.squad_role, 'squad_player'),
		       COALESCE(c.morale, 0.5), COALESCE(c.playing_time_pct, 0),
		       COALESCE(ps.professionalism, 50)
		FROM player.players p
		LEFT JOIN player.player_condition c ON c.player_id = p.id
		LEFT JOIN LATERAL (
			SELECT squad_role FROM player.contracts
			WHERE player_id = p.id AND status = 'active'
			ORDER BY start_date DESC LIMIT 1
		) cr ON TRUE
		LEFT JOIN player.player_personality ps ON ps.player_id = p.id
		WHERE p.club_id = $1 AND p.status = 'active'
		ORDER BY p.id`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []gameRow
	for rows.Next() {
		var r gameRow
		if err := rows.Scan(&r.PlayerID, &r.Role, &r.Morale, &r.Share, &r.Pro); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// unresolvedRoles returns active contracts whose squad_role is still NULL.
func unresolvedRoles(ctx context.Context, q dbtx, clubID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT player_id FROM player.contracts
		WHERE club_id = $1 AND status = 'active' AND squad_role IS NULL
		ORDER BY player_id`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var p uuid.UUID
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// expectationRole maps a player's playing_time_expectation preference to the
// squad role they are entitled to ("" when nothing matches).
func expectationRole(ctx context.Context, q dbtx, playerID uuid.UUID) (string, error) {
	var v string
	err := q.QueryRow(ctx, `
		SELECT preference_value FROM player.player_preferences
		WHERE player_id = $1 AND preference_type = 'playing_time_expectation'
		ORDER BY created_at DESC LIMIT 1`, playerID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func setSquadRole(ctx context.Context, q dbtx, playerID uuid.UUID, role string) error {
	_, err := q.Exec(ctx, `
		UPDATE player.contracts SET squad_role = $2
		WHERE player_id = $1 AND status = 'active' AND squad_role IS NULL`, playerID, role)
	return err
}

// promiseRow is one open increase_playing_time promise with the context the
// weekly pass needs to grade it.
type promiseRow struct {
	PlayerID  uuid.UUID
	ManagerID uuid.UUID
	Created   time.Time
	Role      string
	Share     float64
}

func pendingPlayingTimePromises(ctx context.Context, q dbtx, clubID uuid.UUID) ([]promiseRow, error) {
	rows, err := q.Query(ctx, `
		SELECT pr.player_id, pr.manager_id, pr.created_at,
		       COALESCE(cr.squad_role, 'squad_player'), COALESCE(c.playing_time_pct, 0)
		FROM social.promises pr
		JOIN player.players p ON p.id = pr.player_id
		LEFT JOIN player.player_condition c ON c.player_id = pr.player_id
		LEFT JOIN LATERAL (
			SELECT squad_role FROM player.contracts
			WHERE player_id = p.id AND status = 'active'
			ORDER BY start_date DESC LIMIT 1
		) cr ON TRUE
		WHERE p.club_id = $1 AND pr.promise_type = 'increase_playing_time' AND pr.status = 'pending'`,
		clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []promiseRow
	for rows.Next() {
		var r promiseRow
		if err := rows.Scan(&r.PlayerID, &r.ManagerID, &r.Created, &r.Role, &r.Share); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func resolvePromises(ctx context.Context, q dbtx, playerID uuid.UUID, status string) error {
	_, err := q.Exec(ctx, `
		UPDATE social.promises SET status = $2, resolved_at = now()
		WHERE player_id = $1 AND promise_type = 'increase_playing_time' AND status = 'pending'`,
		playerID, status)
	return err
}

// stalePendingRequests returns requests open beyond the auto-list horizon.
func stalePendingRequests(ctx context.Context, q dbtx) ([]TransferRequest, error) {
	rows, err := q.Query(ctx, `
		SELECT id, player_id, club_id, manager_id, status, reason, created_at, resolved_at, reassured_until
		FROM player.player_transfer_requests
		WHERE status = 'pending' AND created_at <= now() - make_interval(days => $1)
		ORDER BY created_at`, AutoListTTLWorldDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TransferRequest
	for rows.Next() {
		var r TransferRequest
		if err := rows.Scan(&r.ID, &r.PlayerID, &r.ClubID, &r.ManagerID, &r.Status, &r.Reason,
			&r.CreatedAt, &r.ResolvedAt, &r.ReassuredUntil); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func marketValue(ctx context.Context, q dbtx, playerID uuid.UUID) (int64, error) {
	var v int64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(market_value, 0) FROM player.players WHERE id = $1`, playerID).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func cancelOpenRequest(ctx context.Context, q dbtx, playerID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE player.player_transfer_requests
		SET status = 'withdrawn', resolved_at = now()
		WHERE player_id = $1 AND status = 'pending'`, playerID)
	return err
}

// openRequestsByClub returns pending requests from any of the club's players.
func openRequestsByClub(ctx context.Context, q dbtx, clubID uuid.UUID) ([]TransferRequest, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.player_id, r.club_id, r.manager_id, r.status, r.reason,
		       r.created_at, r.resolved_at, r.reassured_until
		FROM player.player_transfer_requests r
		JOIN player.players p ON p.id = r.player_id
		WHERE p.club_id = $1 AND r.status = 'pending'`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TransferRequest
	for rows.Next() {
		var r TransferRequest
		if err := rows.Scan(&r.ID, &r.PlayerID, &r.ClubID, &r.ManagerID, &r.Status, &r.Reason,
			&r.CreatedAt, &r.ResolvedAt, &r.ReassuredUntil); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// playerProfile returns a player's identity fields for the detail read.
func playerProfile(ctx context.Context, q dbtx, playerID uuid.UUID) (p Player, err error) {
	err = q.QueryRow(ctx, `
		SELECT p.id, p.world_id, p.club_id, p.person_id, p.primary_position, p.squad_number,
		       pe.first_name, pe.last_name, pe.display_name, pe.date_of_birth::text, pe.nationality_code
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		WHERE p.id = $1`, playerID,
	).Scan(&p.ID, &p.WorldID, &p.ClubID, &p.PersonID, &p.PrimaryPosition, &p.SquadNumber,
		&p.FirstName, &p.LastName, &p.DisplayName, &p.DateOfBirth, &p.Nationality)
	return p, err
}

func worldClubs(ctx context.Context, q dbtx, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx,
		`SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY id`, worldID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var c uuid.UUID
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// clubManager returns the club's current manager plus whether the club is
// human-run (a pc_controlled/feature flag lives on club.clubs.is_ai_controlled).
func clubManager(ctx context.Context, q dbtx, clubID uuid.UUID) (managerID uuid.UUID, ai bool, err error) {
	var mgr uuid.UUID
	err = q.QueryRow(ctx,
		`SELECT COALESCE(current_manager_id, '00000000-0000-0000-0000-000000000000'),
		        is_ai_controlled FROM club.clubs WHERE id = $1`, clubID,
	).Scan(&mgr, &ai)
	if err != nil {
		return uuid.Nil, false, err
	}
	return mgr, ai, nil
}

func insertPromise(ctx context.Context, q dbtx, worldID, playerID, managerID uuid.UUID, deadlineDays int) error {
	_, err := q.Exec(ctx, `
		INSERT INTO social.promises
			(world_id, player_id, manager_id, promise_type, explicitness, deadline,
			 confidence, importance, status)
		VALUES ($1, $2, $3, 'increase_playing_time', 'explicit',
		        (now() + make_interval(days => $4))::date, 70, 90, 'pending')`,
		worldID, playerID, managerID, deadlineDays)
	return err
}
