// Persistence + event wiring for the injury engine (S08-03). The math stays in
// evaluate.go; this file owns player.injuries, the condition-side injury_risk
// feedback, the rekewise recovery scan, and the world.events side effects. All
// writers take the caller's pgx.Tx (or manage their own, in RecoverDue) so an
// injury, its event and its audit rows land atomically.
package injury

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// Event types emitted to world.events by this package.
const (
	EventPlayerInjured      = "PLAYER_INJURED"
	EventPlayerRecovered    = "PLAYER_RECOVERED"
	EventInjuryUpdate       = "INJURY_UPDATE"
	EventPlayerRushedReturn = "PLAYER_RUSHED_RETURN"
)

// Sentinel errors surfaced to higher layers (HTTP, services).
var (
	ErrNoOpenInjury = errors.New("player has no open injury")
	ErrPlayerGone   = errors.New("player or club context missing for injury")
)

// Candidate is one engine-attributed match injury forwarded by the match
// service. The v1.6 attribution pass has already resolved the feed's `injury`
// event to a specific player; this layer only supplies the minutes the player
// actually played (from the v1.6 rating sheet).
type Candidate struct {
	PlayerID uuid.UUID
	Minutes  int
}

// RecoverSummary reports one world-scoped recovery scan run.
type RecoverSummary struct {
	Recovered int
	Setbacks  int
}

// MedicalLevel resolves a club's medical facility level 1..10 (proposal 5 when
// the club has no medical row yet — ensure-on-demand rows are created by the
// academy upgrade command, not here).
func MedicalLevel(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (int, error) {
	var lvl int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(level), 5) FROM club.facilities
		WHERE club_id = $1 AND facility_type = 'medical'`, clubID).Scan(&lvl)
	if err != nil {
		return 5, fmt.Errorf("medical level: %w", err)
	}
	return clampLevel(lvl), nil
}

func clampLevel(lvl int) int {
	if lvl < 1 {
		return 1
	}
	if lvl > 10 {
		return 10
	}
	return lvl
}

// RecurrenceForPlayer is the stored recurrence risk of the player's most
// recent completed injury (0 for players who have never been injured). It feeds
// the next Evaluate as RecurrenceLoad, so past injuries make the next one more
// likely to be a forced recurring type.
func RecurrenceForPlayer(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) (float64, error) {
	var r float64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT recurrence_risk::float8 FROM player.injuries
		                 WHERE player_id = $1 AND actual_recovery_date IS NOT NULL
		                 ORDER BY occurred_at DESC LIMIT 1), 0)`, playerID).Scan(&r)
	if err != nil {
		return 0, fmt.Errorf("recurrence for player: %w", err)
	}
	return clamp01(r), nil
}

// playerContext is the per-player context an Evaluate call needs, loaded
// inside the caller's tx so the outcome is derived from the same snapshot that
// persists the row.
type playerContext struct {
	ClubID         uuid.UUID
	Fatigue        float64
	Susceptibility int
	RecurrenceLoad float64
	HasOpenInjury  bool
}

const playerContextQuery = `
	SELECT
		p.club_id,
		COALESCE(c.fatigue, 0)::float8,
		COALESCE(h.injury_susceptibility, 50),
		COALESCE((SELECT i.recurrence_risk::float8 FROM player.injuries i
		          WHERE i.player_id = p.id AND i.actual_recovery_date IS NOT NULL
		          ORDER BY i.occurred_at DESC LIMIT 1), 0),
		EXISTS (SELECT 1 FROM player.injuries i2
		        WHERE i2.player_id = p.id AND i2.actual_recovery_date IS NULL)
	FROM player.players p
	LEFT JOIN player.player_condition c ON c.player_id = p.id
	LEFT JOIN player.player_hidden_traits h ON h.player_id = p.id
	WHERE p.id = ANY($1)`

func loadPlayerContexts(ctx context.Context, tx pgx.Tx, playerIDs []uuid.UUID) (map[uuid.UUID]playerContext, error) {
	out := make(map[uuid.UUID]playerContext, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, playerContextQuery, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("injury contexts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p playerContext
		var pid uuid.UUID
		if err := rows.Scan(&pid, &p.ClubID, &p.Fatigue, &p.Susceptibility, &p.RecurrenceLoad, &p.HasOpenInjury); err != nil {
			return nil, fmt.Errorf("injury context scan: %w", err)
		}
		out[pid] = p
	}
	return out, rows.Err()
}

// medicalLevels resolves the medical level for every club of the given set.
func medicalLevels(ctx context.Context, tx pgx.Tx, clubIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(clubIDs))
	for _, cid := range clubIDs {
		lvl, err := MedicalLevel(ctx, tx, cid)
		if err != nil {
			return nil, err
		}
		out[cid] = lvl
	}
	return out, nil
}

func bumpInjuryRisk(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, risk float64) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, injury_risk, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (player_id) DO UPDATE SET injury_risk = EXCLUDED.injury_risk, updated_at = now()`,
		playerID, clamp01(risk)); err != nil {
		return fmt.Errorf("injury risk bump: %w", err)
	}
	return nil
}

// PersistMatch persists the injuries a completed match produced: for each
// engine-attributed injury candidate it resolves type/severity/duration from
// the player's condition + susceptibility + the club's medical level, seeds the
// derivation from a stream OFF the matchsim exact digest (MatchStream), writes
// the player.injuries row, bumps the training-side injury risk and emits
// PLAYER_INJURED with the resolved Explanation. Callers own the surrounding
// transaction (its idempotency guard keeps a re-finalize a no-op).
func PersistMatch(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher, worldID uuid.UUID, tick int64,
	matchID uuid.UUID, matchSeed int64, now time.Time, candidates []Candidate,
) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	ids := make([]uuid.UUID, 0, len(candidates))
	seen := make(map[uuid.UUID]bool, len(candidates))
	for _, c := range candidates {
		if !seen[c.PlayerID] {
			seen[c.PlayerID] = true
			ids = append(ids, c.PlayerID)
		}
	}
	minutes := make(map[uuid.UUID]int, len(candidates))
	for _, c := range candidates {
		minutes[c.PlayerID] = max(minutes[c.PlayerID], c.Minutes)
	}

	ctxs, err := loadPlayerContexts(ctx, tx, ids)
	if err != nil {
		return 0, err
	}
	clubSet := make(map[uuid.UUID]bool, len(ctxs))
	for _, c := range ctxs {
		clubSet[c.ClubID] = true
	}
	clubIDs := make([]uuid.UUID, 0, len(clubSet))
	for cid := range clubSet {
		if cid != uuid.Nil {
			clubIDs = append(clubIDs, cid)
		}
	}
	meds, err := medicalLevels(ctx, tx, clubIDs)
	if err != nil {
		return 0, err
	}

	stored := 0
	for _, pid := range ids {
		c, ok := ctxs[pid]
		if !ok || c.ClubID == uuid.Nil {
			continue
		}
		if c.HasOpenInjury {
			continue // never stack a second open injury on a player
		}
		out := Evaluate(Input{
			Seed:           int64(MatchStream(matchSeed, pid)),
			Fatigue:        c.Fatigue,
			Susceptibility: c.Susceptibility,
			MedicalLevel:   meds[c.ClubID],
			Minutes:        minutes[pid],
			FoulContext:    true,
			RecurrenceLoad: c.RecurrenceLoad,
		})
		expected := now.AddDate(0, 0, out.DaysOut)
		if _, err := tx.Exec(ctx, `
			INSERT INTO player.injuries
				(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, match_id, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			pid, string(out.Type), out.Severity, expected, out.RecurrenceRisk, matchID, now); err != nil {
			return stored, fmt.Errorf("insert injury: %w", err)
		}
		if err := bumpInjuryRisk(ctx, tx, pid, out.RecurrenceRisk); err != nil {
			return stored, err
		}
		seed := matchSeed
		if _, err := emit(ctx, tx, pub, worldID, tick, "system", nil, EventPlayerInjured, map[string]any{
			"player_id":              pid.String(),
			"club_id":                c.ClubID.String(),
			"match_id":               matchID.String(),
			"injury_type":            string(out.Type),
			"severity":               out.Severity,
			"expected_recovery_date": expected.Format("2006-01-02"),
			"recurrence_risk":        out.RecurrenceRisk,
		}, out.Explanation, &seed); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}

// PersistTraining writes one weekly training injury (no match context),
// switching the same engine with the training-week stream. It returns whether
// an injury row was written.
func PersistTraining(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher, worldID uuid.UUID, tick int64,
	clubID, playerID uuid.UUID, fatigue float64, susceptibility, medicalLevel int, recurrenceLoad float64, seed uint64,
) (bool, error) {
	out := Evaluate(Input{
		Seed:           int64(seed),
		Fatigue:        fatigue,
		Susceptibility: susceptibility,
		MedicalLevel:   medicalLevel,
		RecurrenceLoad: recurrenceLoad,
	})
	now := time.Now().UTC()
	expected := now.AddDate(0, 0, out.DaysOut)
	if _, err := tx.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		playerID, string(out.Type), out.Severity, expected, out.RecurrenceRisk, now); err != nil {
		return false, fmt.Errorf("insert training injury: %w", err)
	}
	if err := bumpInjuryRisk(ctx, tx, playerID, out.RecurrenceRisk); err != nil {
		return false, err
	}
	if _, err := emit(ctx, tx, pub, worldID, tick, "system", nil, EventPlayerInjured, map[string]any{
		"player_id":              playerID.String(),
		"club_id":                clubID.String(),
		"injury_type":            string(out.Type),
		"severity":               out.Severity,
		"expected_recovery_date": expected.Format("2006-01-02"),
		"recurrence_risk":        out.RecurrenceRisk,
	}, out.Explanation, nil); err != nil {
		return false, err
	}
	return true, nil
}

// RecoverDue is the world-scoped weekly recovery scan (called from the player
// weekly pass). In one tx it: (1) rolls setbacks for open injuries past 55% of
// their planned recuperation (appending to injury_setbacks — idempotent under
// redelivery), pushing expected_recovery_date later; (2) closes injuries whose
// expected date has arrived, emits PLAYER_RECOVERED, writes the injury_return
// history row and resets the training-side injury risk. Returns the scan
// counts.
func RecoverDue(ctx context.Context, pool *pgxpool.Pool, pub eventbus.Publisher, worldID uuid.UUID, tick int64, today time.Time) (RecoverSummary, error) {
	var sum RecoverSummary
	tx, err := pool.Begin(ctx)
	if err != nil {
		return sum, fmt.Errorf("recover: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT i.id, i.player_id, i.injury_type, i.expected_recovery_date, i.occurred_at
		FROM player.injuries i
		JOIN player.players p ON p.id = i.player_id
		JOIN club.clubs c ON c.id = p.club_id
		WHERE c.world_id = $1 AND i.actual_recovery_date IS NULL`, worldID)
	if err != nil {
		return sum, fmt.Errorf("recover: list open injuries: %w", err)
	}
	type openRow struct {
		id       uuid.UUID
		player   uuid.UUID
		typ      string
		expected time.Time
		occurred time.Time
	}
	var open []openRow
	for rows.Next() {
		var r openRow
		if err := rows.Scan(&r.id, &r.player, &r.typ, &r.expected, &r.occurred); err != nil {
			return sum, fmt.Errorf("recover: scan: %w", err)
		}
		open = append(open, r)
	}
	if err := rows.Err(); err != nil {
		return sum, err
	}

	todayDate := truncd(today)
	for _, r := range open {
		// 1. Setback stage — only while still inside the planned window.
		if r.expected.After(todayDate) && !r.occurred.IsZero() {
			total := daysBetween(r.occurred, r.expected)
			elapsed := daysBetween(r.occurred, todayDate)
			if total > 0 && float64(elapsed)/float64(total) >= SetbackWindowStart {
				add := Setback(SetbackStream(r.id, tick))
				if add > 0 {
					tag, err := tx.Exec(ctx, `
						INSERT INTO player.injury_setbacks (injury_id, week, days_added)
						VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, r.id, tick, add)
					if err != nil {
						return sum, fmt.Errorf("recover: setback insert: %w", err)
					}
					if tag.RowsAffected() > 0 {
						if _, err := tx.Exec(ctx, `
							UPDATE player.injuries SET expected_recovery_date = expected_recovery_date + $2::int
							WHERE id = $1`, r.id, add); err != nil {
							return sum, fmt.Errorf("recover: setback extend: %w", err)
						}
						if _, err := emit(ctx, tx, pub, worldID, tick, "system", nil, EventInjuryUpdate, map[string]any{
							"player_id": r.player.String(),
							"injury_id": r.id.String(),
							"kind":      "setback",
							"days":      add,
						}, setbackExplanation(tick, r.id, add), nil); err != nil {
							return sum, err
						}
						sum.Setbacks++
					}
				}
			}
		}
	}

	// 2. Recovery stage — due injuries close in deterministic order.
	sort.Slice(open, func(i, j int) bool { return open[i].id.String() < open[j].id.String() })
	for _, r := range open {
		var due bool
		err := tx.QueryRow(ctx, `
			SELECT (expected_recovery_date <= $2::timestamp)
			FROM player.injuries WHERE id = $1 AND actual_recovery_date IS NULL`, r.id, todayDate).Scan(&due)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // closed since the scan (e.g. a rushed return earlier this week)
		}
		if err != nil {
			return sum, fmt.Errorf("recover: due scan: %w", err)
		}
		if !due {
			continue
		}
		var expected time.Time
		err = tx.QueryRow(ctx, `
			UPDATE player.injuries SET actual_recovery_date = $2
			WHERE id = $1 AND actual_recovery_date IS NULL
			RETURNING expected_recovery_date`, r.id, todayDate).Scan(&expected)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return sum, fmt.Errorf("recover: close: %w", err)
		}
		season, err := seasonFor(ctx, tx, worldID)
		if err != nil {
			return sum, err
		}
		var clubID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT club_id FROM player.players WHERE id = $1`, r.player).Scan(&clubID); err != nil {
			return sum, err
		}
		var baseline float64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(injury_susceptibility, 50) FROM player.player_hidden_traits WHERE player_id = $1`,
			r.player).Scan(&baseline); err != nil {
			return sum, fmt.Errorf("recover: susceptibility: %w", err)
		}
		if err := bumpInjuryRisk(ctx, tx, r.player, baseline/200); err != nil {
			return sum, err
		}
		eventID, err := emit(ctx, tx, pub, worldID, tick, "system", nil, EventPlayerRecovered, map[string]any{
			"player_id":              r.player.String(),
			"injury_id":              r.id.String(),
			"injury_type":            r.typ,
			"recovery_date":          todayDate.Format("2006-01-02"),
			"expected_recovery_date": expected.Format("2006-01-02"),
		}, recoveryExplanation(todayDate, expected), nil)
		if err != nil {
			return sum, err
		}
		if _, err := tx.Exec(ctx, `
				INSERT INTO player.player_history (player_id, world_id, season, club_id, event_type, description, related_event_id, occurred_at)
				VALUES ($1, $2, $3, $4, 'injury_return', $5, $6, $7)`,
			r.player, worldID, season, clubID,
			fmt.Sprintf("Returned from %s injury on %s", r.typ, todayDate.Format("2006-01-02")),
			eventID, time.Now().UTC()); err != nil {
			return sum, fmt.Errorf("recover: history: %w", err)
		}
		sum.Recovered++
	}

	if err := tx.Commit(ctx); err != nil {
		return sum, fmt.Errorf("recover: commit: %w", err)
	}
	return sum, nil
}

// OpenForPlayer returns the player's open injury row, or nil.
func OpenForPlayer(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) (id uuid.UUID, typ Type, expected time.Time, ok bool, err error) {
	err = tx.QueryRow(ctx, `
		SELECT id, injury_type, expected_recovery_date
		FROM player.injuries
		WHERE player_id = $1 AND actual_recovery_date IS NULL
		ORDER BY occurred_at DESC LIMIT 1`, playerID).Scan(&id, &typ, &expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, typ, expected, false, nil
	}
	if err != nil {
		return id, typ, expected, false, err
	}
	return id, typ, expected, true, nil
}

// RushClose ends an open injury early on manager instruction, stamping the
// rushed-return recurrence floor and emitting PLAYER_RUSHED_RETURN.
func RushClose(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher, worldID uuid.UUID, tick int64,
	clubID, playerID, injuryID uuid.UUID, now time.Time,
) error {
	var expected time.Time
	var newRisk float64
	err := tx.QueryRow(ctx, `
		UPDATE player.injuries
		SET actual_recovery_date = $2, recurrence_risk = GREATEST(recurrence_risk, $3)
		WHERE id = $1 AND actual_recovery_date IS NULL
		RETURNING expected_recovery_date, recurrence_risk`, injuryID, truncd(now), RushRecurrenceFloor).
		Scan(&expected, &newRisk)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoOpenInjury
	}
	if err != nil {
		return fmt.Errorf("rush close: %w", err)
	}
	exp := explanation.New("player_rushed_return", 0).
		Add("returned before expected recovery", -daysBetween(truncd(now), expected)).
		Add("recurrence risk raised", int(newRisk*100))
	if _, err := emit(ctx, tx, pub, worldID, tick, "manager", &clubID, EventPlayerRushedReturn, map[string]any{
		"player_id":              playerID.String(),
		"injury_id":              injuryID.String(),
		"expected_recovery_date": expected.Format("2006-01-02"),
		"actual_recovery_date":   truncd(now).Format("2006-01-02"),
		"recurrence_risk":        newRisk,
	}, exp, nil); err != nil {
		return err
	}
	return nil
}

// emit writes one world.events row atomically with the caller's tx (record-only
// when pub is nil), mirroring the player package's event helper.
func emit(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher, worldID uuid.UUID, tick int64,
	actorType string, actorID *uuid.UUID, eventType string, payload any, exp *explanation.Explanation, seed *int64,
) (uuid.UUID, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	e := eventbus.Event{
		ID:         uuid.New(),
		WorldID:    worldID,
		WorldTick:  tick,
		EventType:  eventType,
		Payload:    payloadJSON,
		RandomSeed: seed,
		OccurredAt: time.Now().UTC(),
	}
	if actorType != "" {
		at := actorType
		e.ActorType = &at
		e.ActorID = actorID
	}
	if exp != nil {
		b, err := json.Marshal(exp)
		if err != nil {
			return uuid.Nil, err
		}
		e.Explanation = b
	}
	if err := eventbus.WriteTx(ctx, pub, tx, &e); err != nil {
		return uuid.Nil, err
	}
	return e.ID, nil
}

func setbackExplanation(tick int64, injuryID uuid.UUID, days int) *explanation.Explanation {
	return explanation.New("injury_setback", days).
		Add("recovery setback", days).
		Add("week", int(tick))
}

func recoveryExplanation(today, expected time.Time) *explanation.Explanation {
	return explanation.New("player_recovered", 0).
		Add("recovered", daysBetween(today, expected))
}

func seasonFor(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (int, error) {
	var s int
	if err := tx.QueryRow(ctx,
		`SELECT current_season FROM world.worlds WHERE id = $1`, worldID).Scan(&s); err != nil {
		return 0, fmt.Errorf("season: %w", err)
	}
	return s, nil
}

func truncd(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func daysBetween(a, b time.Time) int {
	return int(truncd(b).Sub(truncd(a)) / (24 * time.Hour))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
