package match

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalboard "github.com/touchline/backend/internal/board"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/injury"
	"github.com/touchline/backend/internal/player"
	internalsocial "github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/matchsim"
	"github.com/touchline/backend/pkg/pitchsim"
)

// Fixture statuses (match.fixtures.status).
const (
	fixtureScheduled = "scheduled"
	fixtureLive      = "live"
	fixtureCompleted = "completed"
)

// Match statuses (match.matches.status).
const (
	MatchStatusPending    = "pending"
	MatchStatusInProgress = "in_progress"
	MatchStatusCompleted  = "completed"
)

// Event types for the world.events log emitted by PlayFixture.
const (
	EventLineupWarning = "LINEUP_WARNING"
	EventMatchPlayed   = "MATCH_PLAYED"
)

// Publishable is the event sink (may be nil; the world.events log is the
// authoritative store and is always written regardless). Only the tx-scoped
// outbox method is required (OPD-23).
type Publishable interface {
	eventbus.Publisher
}

// StandingsContext lets PlayFixture ask the competition layer about
// standings-dependent stakes. Phase 6 implements it; until then a nil value
// reports no standings (six-pointer and dead-rubber both false).
type StandingsContext interface {
	IsSixPointer(ctx context.Context, fixtureID uuid.UUID) (bool, error)
	IsDeadRubber(ctx context.Context, fixtureID uuid.UUID) (bool, error)
}

// AbsenceDelegator is the S06-05 pre-kickoff hook. The policy engine implements
// it: before a fixture is frozen the engine tallies each side's attendance and,
// for away managers, writes the delegated XI/tactics (the league's manager-only
// inputs the snapshot freezes). The interface lives in match so the policybot
// package is never imported by the simulation core (no import cycle).
type AbsenceDelegator interface {
	EnsureMatchInputs(ctx context.Context, fixtureID uuid.UUID) error
}

// Service orchestrates one deterministic matchday at a time.
type Service struct {
	pool      *pgxpool.Pool
	bus       Publishable
	squad     *squad.Store
	form      *form.Store
	standings StandingsContext
	players   *player.Service
	social    *internalsocial.Service
	board     *internalboard.Service
	absence   AbsenceDelegator
}

// NewService builds the match orchestration service.
func NewService(pool *pgxpool.Pool, bus Publishable, squadStore *squad.Store, formStore *form.Store) *Service {
	return &Service{pool: pool, bus: bus, squad: squadStore, form: formStore}
}

// WithStandingsContext installs the Phase 6 standings dependency.
func (s *Service) WithStandingsContext(st StandingsContext) { s.standings = st }

// WithPlayers installs the morale/playing-time engine, whose appearances hook
// runs inside the match-completion transaction. nil in tests disables it.
func (s *Service) WithPlayers(p *player.Service) *Service {
	s.players = p
	return s
}

// WithSocial installs the S06-04c rivalry tracker, whose per-fixture hook runs
// inside the match-completion transaction (edges + trust + RELATIONSHIP_CHANGED
// outbox event) and pushes the best-effort realtime envelope after commit.
// nil in tests disables it.
func (s *Service) WithSocial(soc *internalsocial.Service) *Service {
	s.social = soc
	return s
}

// WithBoard installs the IM33 per-match board rating, supporter reaction and
// fan-reaction news hook, which runs inside the match-completion transaction
// after the social hook. nil in tests disables it.
func (s *Service) WithBoard(b *internalboard.Service) *Service {
	s.board = b
	return s
}

// WithPolicyBot installs the S06-05 absence-delegation hook. The given
// delegator is invoked for every scheduled fixture immediately before its
// simulation snapshot is frozen. nil leaves the hook disabled.
func (s *Service) WithPolicyBot(a AbsenceDelegator) *Service {
	s.absence = a
	return s
}

// PlayFixture simulates and persists ONE fixture atomically. It is idempotent:
// a fixture already `completed` is a read-only no-op returning the persisted
// match. Statuses other than `scheduled` (postponed/cancelled) are errors.
func (s *Service) PlayFixture(ctx context.Context, fixtureID uuid.UUID) (*MatchResult, error) {
	if s.absence != nil {
		if err := s.absence.EnsureMatchInputs(ctx, fixtureID); err != nil {
			return nil, fmt.Errorf("play fixture: absence inputs: %w", err)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("play fixture: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	f, err := loadFixture(ctx, tx, fixtureID, true)
	if err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	if f.Status == fixtureCompleted {
		return loadExisting(ctx, tx, fixtureID)
	}
	if f.Status != fixtureScheduled {
		return nil, fmt.Errorf("play fixture %s: not playable (status %q)", fixtureID, f.Status)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE match.fixtures SET status = $2 WHERE id = $1 AND status = $3`,
		fixtureID, fixtureLive, fixtureScheduled); err != nil {
		return nil, fmt.Errorf("play fixture: mark live: %w", err)
	}

	tick, err := s.form.WorldTick(ctx, f.WorldID)
	if err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}
	tuning := matchsim.DefaultTuning()
	seed := fixtureSeed(fixtureID)
	fc, err := s.fixtureContext(ctx, tx, f)
	if err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	homeClub, err := s.squad.LoadClub(ctx, f.HomeClub.ID)
	if err != nil {
		return nil, fmt.Errorf("play fixture: home club: %w", err)
	}
	awayClub, err := s.squad.LoadClub(ctx, f.AwayClub.ID)
	if err != nil {
		return nil, fmt.Errorf("play fixture: away club: %w", err)
	}

	homePlan, err := s.buildTeam(ctx, f, homeClub, awayClub.Reputation, tick, fc, seed, tuning)
	if err != nil {
		return nil, fmt.Errorf("play fixture: home team: %w", err)
	}
	awayPlan, err := s.buildTeam(ctx, f, awayClub, homeClub.Reputation, tick, fc, seed, tuning)
	if err != nil {
		return nil, fmt.Errorf("play fixture: away team: %w", err)
	}

	res := matchsim.Simulate(matchsim.Options{
		Seed:       seed,
		Home:       lineupsTeam(homePlan),
		Away:       lineupsTeam(awayPlan),
		Tuning:     tuning,
		GoldenGoal: fc.GoldenGoal,
	})

	// With the world's positional engine on (IM34) a quick-played match keeps
	// its kickoff snapshot and the engine's extra events, so it can be replayed
	// in 2D exactly like a live one.
	var (
		snapshot []byte
		offsets  map[int]int
		extras   []pitchsim.Extra
	)
	if s.resolveVisual(ctx, f.WorldID) {
		if snapshot, err = json.Marshal(simInputs{
			HomeTeam: homePlan.team, AwayTeam: awayPlan.team,
			HomeXI: homePlan.xi, AwayXI: awayPlan.xi,
			HomeBench: homePlan.bench, AwayBench: awayPlan.bench,
			HomeTaker: homePlan.taker, AwayTaker: awayPlan.taker,
			FormHome: homePlan.formState, FormAway: awayPlan.formState,
			WorldTick: tick, FixtureContext: fc, Visual: true,
		}); err != nil {
			return nil, fmt.Errorf("play fixture: marshal snapshot: %w", err)
		}
		evs, last := simEvents(res.Events, matchsim.GoldenGoalMaxMinute)
		_, offsets, extras = trackSlice(
			pitchsim.Generate(pitchInput(seed, homePlan.team, awayPlan.team, homePlan.xi, awayPlan.xi, evs, last)), 1, last)
	}

	matchID, now, err := persistMatch(ctx, tx, fixtureID, f.WorldID, seed, res, snapshot)
	if err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	if _, err := persistEvents(ctx, tx, matchID, res.Events, 0, offsets); err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}
	if _, err := persistExtras(ctx, tx, matchID, extras); err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	// Injuries land atomically with the result (S08-03): the engine's
	// attribution already linked each injury event to a player; the store
	// derives severity/duration deterministically from the seeded stream, the
	// player's fatigue + susceptibility and the club's medical facility.
	if _, err := injury.PersistMatch(ctx, tx, s.bus, f.WorldID, tick, matchID, seed, now, injuryCandidates(res)); err != nil {
		return nil, fmt.Errorf("play fixture: injuries: %w", err)
	}

	if err := s.applyForm(ctx, tx, homePlan, awayPlan, res, tick); err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE match.fixtures SET status = 'completed' WHERE id = $1`, fixtureID); err != nil {
		return nil, fmt.Errorf("play fixture: complete: %w", err)
	}

	evs := s.worldEvents(f, homePlan, awayPlan, matchID, seed, res, now)
	for _, ev := range evs {
		if err := s.recordEvent(ctx, tx, ev); err != nil {
			return nil, fmt.Errorf("play fixture: %w", err)
		}
	}

	// Rivalry graph + trust deltas land atomically with the result (S06-04c).
	var socialPush *internalsocial.RelationshipPush
	if s.social != nil {
		if socialPush, err = s.social.RecordCompletedMatch(ctx, tx, f.WorldID, fixtureID, f.HomeClub.ID, f.AwayClub.ID, res.HomeGoals, res.AwayGoals, now); err != nil {
			return nil, fmt.Errorf("play fixture: %w", err)
		}
	}

	// Board rating + supporter reaction + fan news (IM33). MATCH_PLAYED is the
	// last world event.
	if s.board != nil {
		if err := s.board.RecordCompletedMatch(ctx, tx, f.WorldID, fixtureID, evs[len(evs)-1].ID, f.HomeClub.ID, f.AwayClub.ID, res.HomeGoals, res.AwayGoals); err != nil {
			return nil, fmt.Errorf("play fixture: board: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("play fixture: commit: %w", err)
	}

	if socialPush != nil {
		s.social.PublishRelationshipChange(ctx, socialPush)
	}

	return &MatchResult{
		Match: &Match{
			ID: matchID, FixtureID: fixtureID, WorldID: f.WorldID,
			Seed: seed, EngineVersion: matchsim.EngineVersion,
			HomeGoals: res.HomeGoals, AwayGoals: res.AwayGoals,
			Status: fixtureCompleted, EndedAt: &now,
		},
		Events: nil, // feed consumers use GetMatchEvents
	}, nil
}
