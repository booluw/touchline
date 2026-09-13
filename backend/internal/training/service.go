package training

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// Event types emitted to world.events by this package.
const (
	EventTrainingPlanSet = "TRAINING_PLAN_SET"
	EventTrainingWeek    = "TRAINING_WEEK"
)

// Sentinel errors surfaced to HTTP handlers.
var (
	ErrClubNotFound     = errors.New("club not found")
	ErrNotOwned         = errors.New("manager does not control this club")
	ErrWorldNotActive   = errors.New("world is not accepting gameplay operations")
	ErrInvalidArchetype = errors.New("archetype must be one of the approved five")
)

// Publishable is the event sink (may be nil in the API process; the
// world.events log is authoritative regardless and always written).
type Publishable interface {
	eventbus.Publisher
}

// Actor is the invoking principal (a manager or a club's policy bot) used for
// ownership checks and actor stamping on world.events.
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

func (a Actor) actorTypeAndID() (string, *uuid.UUID) {
	t := "manager"
	if a.IsPolicyBot {
		t = "policy_bot"
	}
	return t, &a.ManagerID
}

// Service hosts the Simple-Mode training commands and the weekly processor.
type Service struct {
	pool *pgxpool.Pool
	bus  Publishable
}

// NewService builds the training service.
func NewService(pool *pgxpool.Pool, bus Publishable) *Service {
	return &Service{pool: pool, bus: bus}
}

// PlanView is the read shape for GET /api/clubs/:id/training-plan.
type PlanView struct {
	ClubID            uuid.UUID  `json:"club_id"`
	Archetype         string     `json:"archetype"`
	EffectiveFromTick int64      `json:"effective_from_tick"`
	LastAppliedWeek   *int64     `json:"last_applied_week,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

// SubmitPlan sets a club's weekly training plan (design §3). The plan is
// effective from the next weekly WORLD_TICK (stamped effective_from_tick =
// current world tick); swapping plans mid-week gives no rebate and resets the
// applied-week stamp.
func (s *Service) SubmitPlan(ctx context.Context, actor Actor, clubID uuid.UUID, archetype string) error {
	if !IsArchetype(archetype) {
		return ErrInvalidArchetype
	}
	club, err := s.requireClub(ctx, clubID)
	if err != nil {
		return err
	}
	if err := s.requireOwnership(ctx, actor.ManagerID, clubID); err != nil {
		return err
	}

	var tick int64
	if err := s.pool.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, club.WorldID).Scan(&tick); err != nil {
		return fmt.Errorf("training plan: read world tick: %w", err)
	}

	actorType, actorID := actor.actorTypeAndID()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("training plan: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		INSERT INTO club.club_training_plans
			(club_id, archetype, effective_from_tick, last_applied_week, updated_by_actor_type, updated_by_actor_id, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $5, now())
		ON CONFLICT (club_id) DO UPDATE SET
			archetype = EXCLUDED.archetype,
			effective_from_tick = EXCLUDED.effective_from_tick,
			last_applied_week = NULL,
			updated_by_actor_type = EXCLUDED.updated_by_actor_type,
			updated_by_actor_id = EXCLUDED.updated_by_actor_id,
			updated_at = now()`,
		clubID, archetype, tick, actorType, actorID); err != nil {
		return fmt.Errorf("training plan: upsert: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{
		"club_id":   clubID.String(),
		"archetype": archetype,
	})
	if err := s.recordEvent(ctx, tx, club.WorldID, EventTrainingPlanSet, actor, payload); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("training plan: commit: %w", err)
	}
	return nil
}

// GetPlan returns the club's active plan. A missing row is the zero value — the
// API renders it as "no plan set".
func (s *Service) GetPlan(ctx context.Context, clubID uuid.UUID) (PlanView, error) {
	empty := PlanView{}
	var v PlanView
	var lastApplied *int64
	var updatedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT club_id, archetype, effective_from_tick, last_applied_week, updated_at
		FROM club.club_training_plans WHERE club_id = $1`, clubID).
		Scan(&v.ClubID, &v.Archetype, &v.EffectiveFromTick, &lastApplied, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("training plan: get: %w", err)
	}
	v.LastAppliedWeek = lastApplied
	v.UpdatedAt = updatedAt
	return v, nil
}

// ApplyWeekly is the worker's weekly WORLD_TICK handler (design §2): every
// club in the world with an active plan gets its weekly attribute deltas and
// condition step, applied idempotently per club in its own transaction (the
// last_applied_week stamp makes at-least-once river redelivery safe). Returns
// the number of clubs applied.
func (s *Service) ApplyWeekly(ctx context.Context, worldID uuid.UUID, weekTick int64) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.club_id, p.archetype
		FROM club.club_training_plans p
		JOIN club.clubs c ON c.id = p.club_id
		WHERE c.world_id = $1 AND (p.last_applied_week IS NULL OR p.last_applied_week < $2)
		ORDER BY p.club_id`, worldID, weekTick)
	if err != nil {
		return 0, fmt.Errorf("training weekly: list clubs: %w", err)
	}
	type target struct {
		clubID    uuid.UUID
		archetype string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.clubID, &t.archetype); err != nil {
			rows.Close()
			return 0, fmt.Errorf("training weekly: scan target: %w", err)
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("training weekly: iterate targets: %w", err)
	}

	applied := 0
	for _, t := range targets {
		ok, err := s.applyClubWeekly(ctx, worldID, t.clubID, t.archetype, weekTick)
		if err != nil {
			return applied, fmt.Errorf("training weekly: club %s: %w", t.clubID, err)
		}
		if ok {
			applied++
		}
	}
	return applied, nil
}

// playerRow is one squad member's application context.
type playerRow struct {
	id       uuid.UUID
	position string
	age      int
	injury   int // injury_susceptibility, defaults to 50
}

// applyClubWeekly applies one club's plan for a week inside its own
// transaction. Returns false without writing when the plan was already applied.
func (s *Service) applyClubWeekly(ctx context.Context, worldID uuid.UUID, clubID uuid.UUID, archetype string, weekTick int64) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var lastApplied *int64
	if err := tx.QueryRow(ctx, `
		SELECT last_applied_week FROM club.club_training_plans WHERE club_id = $1 FOR UPDATE`,
		clubID).Scan(&lastApplied); err != nil {
		return false, fmt.Errorf("lock plan: %w", err)
	}
	if lastApplied != nil && *lastApplied >= weekTick {
		return false, nil // already applied this week; redelivery is safe
	}

	a, ok := archetypeFor(archetype)
	if !ok {
		return false, ErrInvalidArchetype
	}

	players, err := loadPlayers(ctx, tx, clubID)
	if err != nil {
		return false, err
	}
	attrs, err := loadAttributes(ctx, tx, clubID)
	if err != nil {
		return false, err
	}
	pidList := make([]uuid.UUID, 0, len(players))
	for _, p := range players {
		pidList = append(pidList, p.id)
	}
	conds, err := loadConditions(ctx, tx, pidList)
	if err != nil {
		return false, err
	}
	// Recovery detrain: physical decay when this is recovery AND the three
	// immediately-preceding applied weeks were also recovery (§2.1).
	consecutiveRecovery := 0
	if a.Recovery {
		consecutiveRecovery, err = consecutiveRecoveryWeeks(ctx, tx, worldID, clubID)
		if err != nil {
			return false, err
		}
	}

	for _, p := range players {
		attr := attrs[p.id]
		if attr == nil {
			attr = make(map[string]int)
		}
		// Deterministic per (week, player, key): redelivery replays the same
		// binary rounding, and the idempotency stamp prevents double-applying.
		c := conds[p.id]
		if c.PlayerID == uuid.Nil {
			c = defaultCondition(p.id, p.injury)
		}

		for _, key := range distinctKeys(a) {
			d := attrDelta(a, p.age, key)
			if d == 0 {
				continue
			}
			rng := rngFor(weekTick, p.id, key)
			next := applyDelta(attr[key], d, rng)
			if next == attr[key] {
				continue
			}
			cat := playergen.CategoryForKey(key)
			if cat == "" {
				continue // unknown key: catalogue changed; never write garbage
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (player_id, attribute_category, attribute_key) DO UPDATE SET value = EXCLUDED.value`,
				p.id, cat, key, next); err != nil {
				return false, fmt.Errorf("attribute %s: %w", key, err)
			}
		}

		// Recovery detrain decay (independent of age scaling, §2.1).
		if consecutiveRecovery >= 3 {
			for key, d := range map[string]float64{"pace": -0.05, "stamina": -0.05} {
				rng := rngFor(weekTick, p.id, key+"-detrain")
				next := applyDelta(attr[key], d, rng)
				if next == attr[key] {
					continue
				}
				cat := playergen.CategoryForKey(key)
				if cat == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
					VALUES ($1, $2, $3, $4)
					ON CONFLICT (player_id, attribute_category, attribute_key) DO UPDATE SET value = EXCLUDED.value`,
					p.id, cat, key, next); err != nil {
					return false, fmt.Errorf("detrain %s: %w", key, err)
				}
			}
		}

		// Veteran deceleration: 30+ players lose pace/stamina weekly unless the
		// plan is physical (§2.3).
		for key, d := range veteranDecay(p.age, archetype) {
			rng := rngFor(weekTick, p.id, key+"-age")
			next := applyDelta(attr[key], d, rng)
			if next == attr[key] {
				continue
			}
			cat := playergen.CategoryForKey(key)
			if cat == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (player_id, attribute_category, attribute_key) DO UPDATE SET value = EXCLUDED.value`,
				p.id, cat, key, next); err != nil {
				return false, fmt.Errorf("veteran decay %s: %w", key, err)
			}
		}

		c = applyCondition(c, a, p.position)
		if _, err := tx.Exec(ctx, `
			INSERT INTO player.player_condition
				(player_id, fatigue, fitness, sharpness, injury_risk, tactical_familiarity, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (player_id) DO UPDATE SET
				fatigue = EXCLUDED.fatigue,
				fitness = EXCLUDED.fitness,
				sharpness = EXCLUDED.sharpness,
				injury_risk = EXCLUDED.injury_risk,
				tactical_familiarity = EXCLUDED.tactical_familiarity,
				updated_at = now()`,
			p.id, c.Fatigue, c.Fitness, c.Sharpness, c.InjuryRisk, c.TacticalFamiliarity); err != nil {
			return false, fmt.Errorf("condition: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE club.club_training_plans SET last_applied_week = $2, updated_at = now()
		WHERE club_id = $1`, clubID, weekTick); err != nil {
		return false, fmt.Errorf("stamp week: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"club_id":   clubID.String(),
		"archetype": archetype,
	})
	if err := s.recordSystemEvent(ctx, tx, worldID, weekTick, EventTrainingWeek, payload); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// distinctKeys is the ordered set of attribute keys this plan touches: growth
// keys then decay keys (veteran decay is applied separately).
func distinctKeys(a Archetype) []string {
	seen := make(map[string]bool)
	var out []string
	for key := range a.Growth {
		seen[key] = true
		out = append(out, key)
	}
	for key := range a.Decay {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func loadPlayers(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) ([]playerRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.primary_position,
		       COALESCE(EXTRACT(YEAR FROM age(pe.date_of_birth)), 25)::int AS age,
		       COALESCE(h.injury_susceptibility, 50)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_hidden_traits h ON h.player_id = p.id
		WHERE p.club_id = $1 AND p.status IN ('active', 'injured', 'suspended')`, clubID)
	if err != nil {
		return nil, fmt.Errorf("load players: %w", err)
	}
	defer rows.Close()
	var out []playerRow
	for rows.Next() {
		var p playerRow
		if err := rows.Scan(&p.id, &p.position, &p.age, &p.injury); err != nil {
			return nil, fmt.Errorf("scan player: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadAttributes(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (map[uuid.UUID]map[string]int, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.player_id, a.attribute_key, a.value
		FROM player.player_attributes a
		JOIN player.players p ON p.id = a.player_id
		WHERE p.club_id = $1`, clubID)
	if err != nil {
		return nil, fmt.Errorf("load attributes: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]map[string]int)
	for rows.Next() {
		var pid uuid.UUID
		var key string
		var val int
		if err := rows.Scan(&pid, &key, &val); err != nil {
			return nil, fmt.Errorf("scan attribute: %w", err)
		}
		if out[pid] == nil {
			out[pid] = make(map[string]int)
		}
		out[pid][key] = val
	}
	return out, rows.Err()
}

func loadConditions(ctx context.Context, tx pgx.Tx, playerIDs []uuid.UUID) (map[uuid.UUID]squad.PlayerCondition, error) {
	out := make(map[uuid.UUID]squad.PlayerCondition, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT player_id, fatigue, fitness, sharpness, injury_risk, tactical_familiarity
		FROM player.player_condition WHERE player_id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("load conditions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c squad.PlayerCondition
		if err := rows.Scan(&c.PlayerID, &c.Fatigue, &c.Fitness, &c.Sharpness,
			&c.InjuryRisk, &c.TacticalFamiliarity); err != nil {
			return nil, fmt.Errorf("scan condition: %w", err)
		}
		out[c.PlayerID] = c
	}
	return out, rows.Err()
}

// consecutiveRecoveryWeeks counts the consecutive TRAINING_WEEK recovery rows
// immediately preceding this week (latest first, back to 4).
func consecutiveRecoveryWeeks(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT payload->>'archetype' FROM world.events
		WHERE event_type = $1 AND world_id = $2 AND payload->>'club_id' = $3
		ORDER BY world_tick DESC LIMIT 4`,
		EventTrainingWeek, worldID, clubID.String())
	if err != nil {
		return 0, fmt.Errorf("recovery history: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var archetype string
		if err := rows.Scan(&archetype); err != nil {
			return 0, fmt.Errorf("recovery history scan: %w", err)
		}
		if archetype != ArchetypeRecovery {
			break
		}
		count++
	}
	return count, rows.Err()
}

// rngFor is the deterministic binary-rounding stream for one (week, player,
// key): the same inputs always produce the same delta outcomes, so a
// redelivered weekly tick cannot drift from its first application.
func rngFor(weekTick int64, pid uuid.UUID, key string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte("training-apply:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s:%s", weekTick, pid.String(), key)))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// requireClub verifies the club exists in a playable world and returns its
// world reference for event stamping.
func (s *Service) requireClub(ctx context.Context, clubID uuid.UUID) (worldRef, error) {
	var ref worldRef
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT c.world_id, w.status
		 FROM club.clubs c JOIN world.worlds w ON w.id = c.world_id
		 WHERE c.id = $1`, clubID,
	).Scan(&ref.WorldID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, ErrClubNotFound
	}
	if err != nil {
		return ref, fmt.Errorf("training: load club: %w", err)
	}
	if !world.Playable(status) {
		return ref, ErrWorldNotActive
	}
	return ref, nil
}

// worldRef is the world-scoped context of a club write.
type worldRef struct {
	WorldID uuid.UUID
}

// requireOwnership enforces that the actor currently manages the club.
func (s *Service) requireOwnership(ctx context.Context, managerID, clubID uuid.UUID) error {
	var current uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID,
	).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || current != clubID {
		return ErrNotOwned
	}
	if err != nil {
		return fmt.Errorf("training: ownership: %w", err)
	}
	return nil
}

// recordEvent appends one world.events row inside the caller's transaction,
// stamping actor_type from the invoking actor (manager | policy_bot).
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, actor Actor, payload []byte) error {
	actorType, actorID := actor.actorTypeAndID()
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actorType,
		ActorID:   actorID,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// recordSystemEvent appends an automated world.events row (system actor) — the
// weekly TRAINING_WEEK stamp.
func (s *Service) recordSystemEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, eventType string, payload []byte) error {
	actorSystem := "system"
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: worldTick,
		EventType: eventType,
		ActorType: &actorSystem,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}
