// Package tactics is the Simple-Mode command layer for club lineups and
// tactical styles (S05-01). It owns the two write paths — SetLineup and
// SetTactics — with their server-side deadline rules, and provides the read
// views the API surfaces (/api/clubs/:id/lineup, /api/clubs/:id/tactics).
// Writes are actor-stamped (manager | policy_bot) and emit world.events rows
// through the transactional outbox (OPD-23), exactly like the other command
// services.
package tactics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/matchsim"
)

// Event types emitted to world.events by this package.
const (
	EventLineupSaved = "LINEUP_SAVED"
	EventTacticSet   = "TACTIC_SET"
)

// Sentinel errors surfaced to HTTP handlers (409 on deadline hits).
var (
	ErrClubNotFound      = errors.New("club not found")
	ErrNotOwned          = errors.New("manager does not control this club")
	ErrWorldNotActive    = errors.New("world is not accepting gameplay operations")
	ErrFixtureLive       = errors.New("club has a live fixture; the change applies from the next fixture")
	ErrInvalidStyle      = errors.New("style must be one of the approved five")
	ErrInvalidFormation  = errors.New("formation is not allowed for this style")
	ErrInvalidLineup     = errors.New("lineup must name eleven distinct squad players, one per slot")
	ErrPlayerUnavailable = errors.New("lineup includes a player unavailable for selection")
)

// Publishable is the event sink (may be nil in the API process; the
// world.events log is authoritative regardless and always written).
type Publishable interface {
	eventbus.Publisher
}

// Actor is the invoking principal (a manager or a club's policy bot) used for
// ownership checks and actor stamping on the club_* rows and world.events.
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

// actorTypeAndID renders the world.events/club_* actor stamps for this actor.
func (a Actor) actorTypeAndID() (string, *uuid.UUID) {
	t := "manager"
	if a.IsPolicyBot {
		t = "policy_bot"
	}
	return t, &a.ManagerID
}

// Service hosts the Simple-Mode lineup/tactics commands.
type Service struct {
	pool  *pgxpool.Pool
	bus   Publishable
	squad *squad.Store
}

// NewService builds the tactics command service.
func NewService(pool *pgxpool.Pool, bus Publishable, squadStore *squad.Store) *Service {
	return &Service{pool: pool, bus: bus, squad: squadStore}
}

// LineupInput is one slot assignment (slot 0..10; coordinates are
// squad.FormationFor, see design §1.2).
type LineupInput struct {
	Slot     int       `json:"slot"`
	PlayerID uuid.UUID `json:"player_id"`
}

// SlotView is a serialisable slot with its formation position resolved.
type SlotView struct {
	Slot     int       `json:"slot"`
	Position string    `json:"position"`
	PlayerID uuid.UUID `json:"player_id"`
}

// TacticsView is the read shape for GET /api/clubs/:id/tactics.
type TacticsView struct {
	ClubID    uuid.UUID  `json:"club_id"`
	Style     string     `json:"style"`
	Formation string     `json:"formation"`
	Allowed   []string   `json:"allowed_formations"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// LineupView is the read shape returned by GetLineup. Formation/positions are
// resolved via the club's current tactics (a lineup is formation-relative).
type LineupView struct {
	ClubID    uuid.UUID  `json:"club_id"`
	Style     string     `json:"style"`
	Formation string     `json:"formation"`
	Slots     []SlotView `json:"slots"`
}

// worldRef is the world-scoped context of a club write.
type worldRef struct {
	WorldID uuid.UUID
}

// SetLineup replaces a club's preferred XI. Rules (design §3):
//   - the actor must manage the club, in an active world;
//   - deadline: rejected while the club has a live fixture (the change applies
//     from the next fixture);
//   - exactly eleven distinct squad players, one per slot (0..10); the
//     UNIQUE(club_id, player_id) constraint is validated before writing.
func (s *Service) SetLineup(ctx context.Context, actor Actor, clubID uuid.UUID, slots []LineupInput) error {
	if err := validateLineupSlots(slots); err != nil {
		return err
	}
	if err := s.requireOwnership(ctx, actor.ManagerID, clubID); err != nil {
		return err
	}
	return s.setLineup(ctx, actor, clubID, slots)
}

// SetLineupForClub writes a club's XI on behalf of a delegated actor (the
// absence policy bot). Same validation and transactional rules as SetLineup,
// minus the ownership gate: the bot has no current_club_id, so ownership is
// established by the caller (the policy engine only ever targets a club it is
// authorised to delegate for).
func (s *Service) SetLineupForClub(ctx context.Context, actor Actor, clubID uuid.UUID, slots []LineupInput) error {
	if err := validateLineupSlots(slots); err != nil {
		return err
	}
	return s.setLineup(ctx, actor, clubID, slots)
}

// validateLineupSlots enforces the twelve-distinct-entries shape invariants.
func validateLineupSlots(slots []LineupInput) error {
	if len(slots) != 11 {
		return ErrInvalidLineup
	}
	slotsByPos := make(map[int]uuid.UUID, len(slots))
	seen := make(map[uuid.UUID]bool, len(slots))
	for _, in := range slots {
		if in.Slot < 0 || in.Slot > 10 {
			return ErrInvalidLineup
		}
		if _, dup := slotsByPos[in.Slot]; dup {
			return ErrInvalidLineup
		}
		if seen[in.PlayerID] {
			return ErrInvalidLineup
		}
		seen[in.PlayerID] = true
		slotsByPos[in.Slot] = in.PlayerID
	}
	return nil
}

// setLineup is the shared lineup write core: deadline check + transactional
// replace + event. The caller has already established authorisation.
func (s *Service) setLineup(ctx context.Context, actor Actor, clubID uuid.UUID, slots []LineupInput) error {
	slotsByPos := make(map[int]uuid.UUID, len(slots))
	playerIDs := make([]uuid.UUID, 0, len(slots))
	for _, in := range slots {
		slotsByPos[in.Slot] = in.PlayerID
		playerIDs = append(playerIDs, in.PlayerID)
	}

	if _, err := s.requireClub(ctx, clubID); err != nil {
		return err
	}

	players, err := s.squad.LoadSquad(ctx, clubID, time.Now())
	if err != nil {
		return fmt.Errorf("lineup: load squad: %w", err)
	}
	byID := make(map[uuid.UUID]squad.LoadedPlayer, len(players))
	for _, p := range players {
		byID[p.PlayerID] = p
	}
	for _, pid := range playerIDs {
		p, ok := byID[pid]
		if !ok {
			return ErrInvalidLineup
		}
		// A07 gate (S08-03): an injured, suspended, contract-less or
		// underage-street player cannot be named in the XI. The whole lineup
		// is rejected; nothing is written.
		if !p.Available {
			return ErrPlayerUnavailable
		}
	}

	club, _ := s.requireClub(ctx, clubID)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("lineup: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := s.checkDeadline(ctx, tx, clubID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM club.club_lineups WHERE club_id = $1`, clubID); err != nil {
		return fmt.Errorf("lineup: clear: %w", err)
	}
	for slot := 0; slot < 11; slot++ {
		if _, err := tx.Exec(ctx, `
			INSERT INTO club.club_lineups (slot, club_id, player_id)
			VALUES ($1, $2, $3)`, slot, clubID, slotsByPos[slot]); err != nil {
			return fmt.Errorf("lineup: insert slot %d: %w", slot, err)
		}
	}
	if err := s.recordEvent(ctx, tx, club.WorldID, EventLineupSaved, actor, clubID, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("lineup: commit: %w", err)
	}
	return nil
}

// GetLineup returns the club's persisted XI resolved to formation positions
// (missing slots show as nil player ids — the deterministic fallback fills
// them at kickoff).
func (s *Service) GetLineup(ctx context.Context, clubID uuid.UUID) (LineupView, error) {
	empty := LineupView{}
	tactics, err := s.squad.LoadTactics(ctx, clubID)
	if err != nil {
		return empty, err
	}
	style, order := squad.ResolveTactics(tactics)
	lineup, err := s.squad.LoadLineup(ctx, clubID)
	if err != nil {
		return empty, err
	}
	view := LineupView{
		ClubID:    clubID,
		Style:     style,
		Formation: formationName(order),
		Slots:     make([]SlotView, 0, 11),
	}
	for slot, pos := range order {
		pid, ok := lineup[slot]
		if !ok {
			pid = uuid.Nil
		}
		view.Slots = append(view.Slots, SlotView{
			Slot:     slot,
			Position: pos,
			PlayerID: pid,
		})
	}
	return view, nil
}

// SetTactics stores a club's Simple-Mode style and optional formation
// (design §3: style in the approved five; formation in the style's allowed
// set, default first when unset). Same ownership/deadline rules as SetLineup.
func (s *Service) SetTactics(ctx context.Context, actor Actor, clubID uuid.UUID, style, formation string) error {
	if err := s.requireOwnership(ctx, actor.ManagerID, clubID); err != nil {
		return err
	}
	return s.setTactics(ctx, actor, clubID, style, formation)
}

// SetTacticsForClub writes a club's style/formation on behalf of a delegated
// actor (the absence policy bot). Same validation and transactional rules as
// SetTactics without the ownership gate — the caller (policy engine) has
// already established delegated authorisation.
func (s *Service) SetTacticsForClub(ctx context.Context, actor Actor, clubID uuid.UUID, style, formation string) error {
	return s.setTactics(ctx, actor, clubID, style, formation)
}

// setTactics is the shared style/formation write core: style validation,
// formation normalisation, club check, transactional upsert + event.
func (s *Service) setTactics(ctx context.Context, actor Actor, clubID uuid.UUID, style, formation string) error {
	if !matchsim.IsStyle(style) {
		return ErrInvalidStyle
	}
	allowed := squad.AllowedFormations(style)
	if formation != "" {
		ok := false
		for _, f := range allowed {
			if f == formation {
				ok = true
				break
			}
		}
		if !ok {
			return ErrInvalidFormation
		}
	} else {
		formation = allowed[0]
	}

	club, err := s.requireClub(ctx, clubID)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tactics: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := s.checkDeadline(ctx, tx, clubID); err != nil {
		return err
	}

	actorType, actorID := actor.actorTypeAndID()
	if _, err := tx.Exec(ctx, `
		INSERT INTO club.club_tactics (club_id, style, formation, updated_by_actor_type, updated_by_actor_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (club_id) DO UPDATE SET
			style = EXCLUDED.style,
			formation = EXCLUDED.formation,
			updated_by_actor_type = EXCLUDED.updated_by_actor_type,
			updated_by_actor_id = EXCLUDED.updated_by_actor_id,
			updated_at = now()`,
		clubID, style, formation, actorType, actorID); err != nil {
		return fmt.Errorf("tactics: upsert: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{
		"club_id":   clubID.String(),
		"style":     style,
		"formation": formation,
	})
	if err := s.recordEvent(ctx, tx, club.WorldID, EventTacticSet, actor, clubID, payload); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tactics: commit: %w", err)
	}
	return nil
}

// GetTactics returns the club's saved setup, normalised to the approved
// defaults (balanced/4-3-3) when no row exists, plus the style's allowed
// formations and the last write time when a row exists.
func (s *Service) GetTactics(ctx context.Context, clubID uuid.UUID) (TacticsView, error) {
	empty := TacticsView{}
	t, err := s.squad.LoadTactics(ctx, clubID)
	if err != nil {
		return empty, err
	}
	style, order := squad.ResolveTactics(t)
	view := TacticsView{
		ClubID:    clubID,
		Style:     style,
		Formation: formationName(order),
		Allowed:   squad.AllowedFormations(style),
	}
	if t.UpdatedAt != nil {
		view.UpdatedAt = t.UpdatedAt
	}
	return view, nil
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
		return ref, fmt.Errorf("tactics: load club: %w", err)
	}
	if !world.Playable(status) {
		return ref, ErrWorldNotActive
	}
	return ref, nil
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
		return fmt.Errorf("tactics: ownership: %w", err)
	}
	return nil
}

// checkDeadline enforces the server-side deadline (design §3): a change is
// rejected while the club has a live fixture. Runs inside the caller's tx so
// the check and the write are atomic.
func (s *Service) checkDeadline(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) error {
	var live bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM match.fixtures
			WHERE status = 'live' AND (home_club_id = $1 OR away_club_id = $1)
		)`, clubID).Scan(&live); err != nil {
		return fmt.Errorf("tactics: deadline: %w", err)
	}
	if live {
		return ErrFixtureLive
	}
	return nil
}

// recordEvent appends one world.events row inside the caller's transaction,
// stamping actor_type from the invoking actor (manager | policy_bot).
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, actor Actor, clubID uuid.UUID, payload []byte) error {
	if payload == nil {
		b, _ := json.Marshal(map[string]string{"club_id": clubID.String()})
		payload = b
	}
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

// formationName maps a slot order back to its canonical formation key.
func formationName(order []string) string {
	for _, f := range squad.ValidFormations {
		if equalStrings(squad.FormationFor(f), order) {
			return f
		}
	}
	return squad.ValidFormations[0]
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
