// Orchestrator: turns away-status + policies into delegated command execution
// at each deadline. It is the glue between the store, the resolver and the
// club-scoped command cores of the tactics/training/transfer services, so the
// bot runs the same handlers a human would call.
package policybot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/training"
	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
)

// DaysPerFixture approximates how often the league kicks off, used only to
// bound "next fixture" display.
const DaysPerFixture = 7

// Service is the policybot engine.
type Service struct {
	pool        *pgxpool.Pool
	bus         eventbus.EventBus
	store       *Store
	resolver    *Resolver
	tacticsSvc  *tactics.Service
	trainingSvc *training.Service
	transferSvc *transfer.Service
	now         func() time.Time
}

// NewService wires the engine.
func NewService(pool *pgxpool.Pool, bus eventbus.EventBus, squadStore *squad.Store,
	tacticsSvc *tactics.Service, trainingSvc *training.Service, transferSvc *transfer.Service) *Service {
	return &Service{
		pool:        pool,
		bus:         bus,
		store:       NewStore(pool),
		resolver:    NewResolver(squadStore),
		tacticsSvc:  tacticsSvc,
		trainingSvc: trainingSvc,
		transferSvc: transferSvc,
		now:         time.Now,
	}
}

// ensureAway resolves a policy and executes its command sequence for one club.
// Acts with the world's absence bot as the actor.
func (s *Service) ensureAway(ctx context.Context, worldID, clubID uuid.UUID) error {
	botID, err := s.store.GetOrCreateAbsenceBot(ctx, worldID)
	if err != nil {
		return err
	}
	squadPolicy, err := s.loadSquadPolicy(ctx, clubID)
	if err != nil {
		return err
	}
	res, err := s.resolver.ResolveSquad(ctx, clubID, squadPolicy)
	if err != nil {
		return fmt.Errorf("resolve squad for %s: %w", clubID, err)
	}
	actor := tactics.Actor{ManagerID: botID, IsPolicyBot: true}
	if len(res.Slots) > 0 {
		if err := s.tacticsSvc.SetLineupForClub(ctx, actor, clubID, res.Slots); err != nil {
			return fmt.Errorf("bot lineup %s: %w", clubID, err)
		}
	}
	if res.SetTactics {
		if err := s.tacticsSvc.SetTacticsForClub(ctx, actor, clubID, res.TacticsStyle, res.TacticsFormation); err != nil {
			return fmt.Errorf("bot tactics %s: %w", clubID, err)
		}
	}
	return nil
}

// loadSquadPolicy reads the manager's saved squad policy or the assistant
// default. The bot holds no club, so it must run as manager-scoped.
func (s *Service) loadSquadPolicy(ctx context.Context, clubID uuid.UUID) (SquadPolicy, error) {
	p, err := s.policyForClub(ctx, clubID, TypeSquad)
	if err != nil {
		return DefaultSquadPolicy, err
	}
	if p.Enabled == false {
		return DefaultSquadPolicy, nil
	}
	var sp SquadPolicy
	if err := json.Unmarshal(p.Params, &sp); err != nil {
		return DefaultSquadPolicy, nil
	}
	return sp, nil
}

// policyForClub finds the human manager's saved policy row for a club type.
func (s *Service) policyForClub(ctx context.Context, clubID uuid.UUID, policyType string) (Policy, error) {
	managerID, err := s.store.ManagerForClub(ctx, clubID)
	if err != nil {
		return Policy{}, err
	}
	return s.store.GetPolicy(ctx, managerID, policyType)
}

// EnsureMatchInputs is the pre-kickoff seam called by the match scheduler for
// every scheduled fixture. For each side, if the human manager is away the bot
// populates the XI + tactics; every manager (present or away) has the fixture
// tallied against their absence streak. Idempotent: a fixture already live or
// completed is a no-op.
func (s *Service) EnsureMatchInputs(ctx context.Context, fixtureID uuid.UUID) error {
	f, err := s.store.LoadFixture(ctx, fixtureID)
	if err != nil {
		return err
	}
	if f.Status != "scheduled" {
		return nil
	}
	for _, clubID := range []uuid.UUID{f.HomeClubID, f.AwayClubID} {
		prev, err := s.store.PrevClubKickoff(ctx, clubID, f.ScheduledAt)
		if err != nil {
			return err
		}
		managerID, err := s.store.ManagerForClub(ctx, clubID)
		if err != nil {
			if errors.Is(err, ErrManagerNotFound) {
				continue // AI-run or vacant club — not delegated
			}
			return err
		}
		activ, err := s.store.AttendOrMiss(ctx, managerID, prev)
		if err != nil {
			return err
		}
		if activ {
			if err := s.emitAbsence(ctx, f.WorldID, managerID, true); err != nil {
				return err
			}
		}
		state, err := s.store.LoadAbsence(ctx, managerID)
		if err != nil {
			return err
		}
		if state.IsAway() {
			if err := s.ensureAway(ctx, f.WorldID, clubID); err != nil {
				return fmt.Errorf("match inputs %s (%s): %w", fixtureID, clubID, err)
			}
		}
	}
	return nil
}

// EnsureTraining runs before the weekly training application: every away
// manager's club with no active plan gets the resolved archetype submitted for
// this week.
func (s *Service) EnsureTraining(ctx context.Context, worldID uuid.UUID) error {
	clubs, err := s.store.ClubsWithAwayManagers(ctx, worldID)
	if err != nil {
		return err
	}
	for _, clubID := range clubs {
		plan, err := s.trainingSvc.GetPlan(ctx, clubID)
		if err != nil {
			return fmt.Errorf("get plan %s: %w", clubID, err)
		}
		if plan.ClubID != uuid.Nil {
			continue
		}
		managerID, err := s.store.ManagerForClub(ctx, clubID)
		if err != nil {
			return err
		}
		pol, err := s.store.GetPolicy(ctx, managerID, TypeTraining)
		if err != nil || !pol.Enabled {
			pol = Policy{Params: mustMarshal(DefaultTrainingPolicy)}
		}
		var tp TrainingPolicy
		if err := json.Unmarshal(pol.Params, &tp); err != nil {
			tp = DefaultTrainingPolicy
		}
		arch, err := s.resolver.TrainingArchetype(ctx, clubID, tp)
		if err != nil {
			return fmt.Errorf("resolve training %s: %w", clubID, err)
		}
		botID, err := s.store.GetOrCreateAbsenceBot(ctx, worldID)
		if err != nil {
			return err
		}
		if err := s.trainingSvc.SubmitPlanForClub(ctx, training.Actor{ManagerID: botID, IsPolicyBot: true}, clubID, arch); err != nil {
			return fmt.Errorf("bot training %s: %w", clubID, err)
		}
	}
	return nil
}

// RespondToBidsForAbsent runs before the daily transfer tick: every away
// manager's club responds to its open pending bids (seller side) following the
// stored or default transfer policy.
func (s *Service) RespondToBidsForAbsent(ctx context.Context, worldID uuid.UUID) error {
	clubs, err := s.store.ClubsWithAwayManagers(ctx, worldID)
	if err != nil {
		return err
	}
	for _, clubID := range clubs {
		bids, err := s.store.PendingSellerBids(ctx, clubID)
		if err != nil {
			return err
		}
		if len(bids) == 0 {
			continue
		}
		managerID, err := s.store.ManagerForClub(ctx, clubID)
		if err != nil {
			return err
		}
		pol, err := s.store.GetPolicy(ctx, managerID, TypeTransfer)
		if err != nil || !pol.Enabled {
			pol = Policy{Params: mustMarshal(DefaultTransferPolicy)}
		}
		var tp TransferPolicy
		if err := json.Unmarshal(pol.Params, &tp); err != nil {
			tp = DefaultTransferPolicy
		}
		botID, err := s.store.GetOrCreateAbsenceBot(ctx, worldID)
		if err != nil {
			return err
		}
		actor := transfer.Actor{ManagerID: botID, IsPolicyBot: true}
		for _, b := range bids {
			attrs, err := s.store.PlayerAttrs(ctx, b.PlayerID)
			if err != nil {
				return fmt.Errorf("attrs %s: %w", b.PlayerID, err)
			}
			decision := ResolveTransfer(attrs, b, tp, nil)
			if _, _, _, err := s.transferSvc.RespondToBidForClub(ctx, actor, worldID, clubID, b.ID,
				decision.Action, decision.Terms); err != nil {
				return fmt.Errorf("bot bid %s: %w", b.ID, err)
			}
		}
	}
	return nil
}

// --- Public API used by the HTTP layer -----------------------------------

// UpsertPolicy saves or replaces a manager policy.
func (s *Service) UpsertPolicy(ctx context.Context, actor Actor, worldID, managerID uuid.UUID, policyType string, params json.RawMessage) (Policy, error) {
	saved, err := s.store.UpsertPolicy(ctx, Policy{
		WorldID: worldID, ManagerID: managerID, Type: policyType, Params: params, Enabled: true,
	}, "manager", managerID)
	if err != nil {
		return saved, err
	}
	if err := s.emit(ctx, worldID, EventPolicySaved, nil, actor); err != nil {
		return saved, err
	}
	return saved, nil
}

// DeletePolicy removes a manager policy row.
func (s *Service) DeletePolicy(ctx context.Context, actor Actor, worldID, managerID uuid.UUID, policyType string) (bool, error) {
	found, err := s.store.DeletePolicy(ctx, managerID, policyType)
	if err != nil {
		return false, err
	}
	if err := s.emit(ctx, worldID, EventPolicyDeleted, nil, actor); err != nil {
		return found, err
	}
	return found, nil
}

// ListPolicies returns a manager's saved policies.
func (s *Service) ListPolicies(ctx context.Context, managerID uuid.UUID) ([]Policy, error) {
	return s.store.ListPolicies(ctx, managerID)
}

// GetPolicy returns one saved policy (ErrNoPolicy when absent).
func (s *Service) GetPolicy(ctx context.Context, managerID uuid.UUID, policyType string) (Policy, error) {
	return s.store.GetPolicy(ctx, managerID, policyType)
}

// AbsenceView is the manager-facing absence state plus the world/club context
// needed by the UI. The manager id stays flat (self-path echo); the club ids
// nest into refs.
type AbsenceView struct {
	ManagerID        uuid.UUID         `json:"manager_id"`
	WorldID          uuid.UUID         `json:"world_id"`
	AbsenceState     AbsenceState      `json:"absence_state"`
	ClubIDs          []uuid.UUID       `json:"-"`
	Clubs            []*apiref.ClubRef `json:"clubs"`
	NextFixtureAt    *time.Time        `json:"next_fixture_at,omitempty"`
	DelegationActive bool              `json:"delegation_active"`
}

// GetAbsence assembles the manager's absence state with their clubs and next
// scheduled fixture.
func (s *Service) GetAbsence(ctx context.Context, worldID, managerID uuid.UUID) (AbsenceView, error) {
	state, err := s.store.LoadAbsence(ctx, managerID)
	if err != nil {
		return AbsenceView{}, err
	}
	clubIDs, err := s.store.ClubsForManager(ctx, managerID)
	if err != nil {
		return AbsenceView{}, err
	}
	var next *time.Time
	now := s.now()
	for _, clubID := range clubIDs {
		fixtureAt, err := s.store.NextClubFixture(ctx, clubID, now)
		if err == nil && fixtureAt != nil {
			if next == nil || fixtureAt.Before(*next) {
				next = fixtureAt
			}
		}
	}
	clubs := make([]*apiref.ClubRef, 0, len(clubIDs))
	if len(clubIDs) > 0 {
		rows, err := s.store.ClubNames(ctx, clubIDs)
		if err != nil {
			return AbsenceView{}, err
		}
		for _, id := range clubIDs {
			clubs = append(clubs, &apiref.ClubRef{ID: id, Name: rows[id]})
		}
	}
	return AbsenceView{
		ManagerID:        managerID,
		WorldID:          worldID,
		AbsenceState:     state,
		ClubIDs:          clubIDs,
		Clubs:            clubs,
		NextFixtureAt:    next,
		DelegationActive: state.IsAway(),
	}, nil
}

// AbsenceSummary is the aggregated state shown on the manager's dashboard.
type AbsenceSummary struct {
	Away              bool       `json:"away"`
	AwayAuto          bool       `json:"away_auto"`
	AwaySince         *time.Time `json:"away_since"`
	ConsecutiveMissed int        `json:"consecutive_missed"`
	ActivePolicies    []string   `json:"active_policies"`
	NextFixtureAt     *time.Time `json:"next_fixture_at,omitempty"`
	Delegatable       bool       `json:"delegatable"`
}

// GetAbsenceSummary aggregates delegation status for the dashboard.
func (s *Service) GetAbsenceSummary(ctx context.Context, worldID, managerID uuid.UUID) (AbsenceSummary, error) {
	state, err := s.store.LoadAbsence(ctx, managerID)
	if err != nil {
		return AbsenceSummary{}, err
	}
	policies, err := s.store.ListPolicies(ctx, managerID)
	if err != nil {
		return AbsenceSummary{}, err
	}
	active := []string{}
	for _, p := range policies {
		if p.Enabled {
			active = append(active, p.Type)
		}
	}
	clubCount := 0
	clubIDs, err := s.store.ClubsForManager(ctx, managerID)
	if err != nil {
		return AbsenceSummary{}, err
	}
	clubCount = len(clubIDs)
	var next *time.Time
	now := s.now()
	for _, clubID := range clubIDs {
		fixtureAt, err := s.store.NextClubFixture(ctx, clubID, now)
		if err == nil && fixtureAt != nil {
			if next == nil || fixtureAt.Before(*next) {
				next = fixtureAt
			}
		}
	}
	return AbsenceSummary{
		Away:              state.IsAway(),
		AwayAuto:          state.AwayAuto,
		AwaySince:         state.AwaySince,
		ConsecutiveMissed: state.ConsecutiveMissed,
		ActivePolicies:    active,
		NextFixtureAt:     next,
		Delegatable:       clubCount > 0,
	}, nil
}

// SetAway toggles the manager's explicit away mode (away_auto=FALSE). Toggling
// off clears the away state without touching any activity signal; toggling on
// also resets the missed streak so a fresh spell starts clean.
func (s *Service) SetAway(ctx context.Context, worldID, managerID uuid.UUID, away bool) error {
	if err := s.store.SetExplicitAway(ctx, managerID, away); err != nil {
		return err
	}
	return s.emitAbsence(ctx, worldID, managerID, away)
}

// TouchActivity is the authenticated-request heartbeat.
func (s *Service) TouchActivity(ctx context.Context, managerID uuid.UUID) error {
	return s.store.TouchActivity(ctx, managerID)
}

// --- events --------------------------------------------------------------

func (s *Service) emitAbsence(ctx context.Context, worldID, managerID uuid.UUID, away bool) error {
	eventType := EventAbsenceSet
	if !away {
		eventType = EventAbsenceCleared
	}
	return s.emit(ctx, worldID, eventType, nil, Actor{ManagerID: managerID})
}

func (s *Service) emit(ctx context.Context, worldID uuid.UUID, eventType string, payload []byte, actor Actor) error {
	if s.bus == nil {
		return nil
	}
	actorType, actorID := actor.actorTypeAndID()
	return s.bus.Publish(ctx, &eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actorType,
		ActorID:   actorID,
		Payload:   payload,
	})
}

// --- helpers -------------------------------------------------------------

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
