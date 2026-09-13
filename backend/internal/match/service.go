package match

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/matchsim"
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

// Service orchestrates one deterministic matchday at a time.
type Service struct {
	pool      *pgxpool.Pool
	bus       Publishable
	squad     *squad.Store
	form      *form.Store
	standings StandingsContext
}

// NewService builds the match orchestration service.
func NewService(pool *pgxpool.Pool, bus Publishable, squadStore *squad.Store, formStore *form.Store) *Service {
	return &Service{pool: pool, bus: bus, squad: squadStore, form: formStore}
}

// WithStandingsContext installs the Phase 6 standings dependency.
func (s *Service) WithStandingsContext(st StandingsContext) { s.standings = st }

// PlayFixture simulates and persists ONE fixture atomically. It is idempotent:
// a fixture already `completed` is a read-only no-op returning the persisted
// match. Statuses other than `scheduled` (postponed/cancelled) are errors.
func (s *Service) PlayFixture(ctx context.Context, fixtureID uuid.UUID) (*MatchResult, error) {
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

	homeClub, err := s.squad.LoadClub(ctx, f.HomeClubID)
	if err != nil {
		return nil, fmt.Errorf("play fixture: home club: %w", err)
	}
	awayClub, err := s.squad.LoadClub(ctx, f.AwayClubID)
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
		Seed:   seed,
		Home:   homePlan.team,
		Away:   awayPlan.team,
		Tuning: tuning,
	})

	matchID, now, err := persistMatch(ctx, tx, fixtureID, f.WorldID, seed, res)
	if err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
	}

	casters := map[string]*sideCaster{
		f.HomeClubID.String(): newSideCaster(seed, f.HomeClubID, homePlan.xi, homePlan.bench, homePlan.taker),
		f.AwayClubID.String(): newSideCaster(seed, f.AwayClubID, awayPlan.xi, awayPlan.bench, awayPlan.taker),
	}
	if _, err := persistEventsWithCasting(ctx, tx, matchID, res.Events, casters, 0, nil); err != nil {
		return nil, fmt.Errorf("play fixture: %w", err)
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

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("play fixture: commit: %w", err)
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

// PlayMatchday plays every scheduled fixture of a matchday, in schedule order.
// It is the Phase 6 clock hook; standings application stays in the competition
// layer.
func (s *Service) PlayMatchday(ctx context.Context, worldID uuid.UUID, matchday int) ([]*MatchResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM match.fixtures
		WHERE world_id = $1 AND matchday = $2 AND status = 'scheduled'
		ORDER BY scheduled_at`, worldID, matchday)
	if err != nil {
		return nil, fmt.Errorf("play matchday: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("play matchday: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("play matchday: iterate: %w", err)
	}

	out := make([]*MatchResult, 0, len(ids))
	for _, id := range ids {
		r, err := s.PlayFixture(ctx, id)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Persistence helpers
// ---------------------------------------------------------------------------

func loadFixture(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Fixture, error) {
	sql := `
		SELECT id, world_id, competition_id, home_club_id, away_club_id,
		       COALESCE(matchday, 0), scheduled_at, status
		FROM match.fixtures WHERE id = $1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	f := &Fixture{}
	err := tx.QueryRow(ctx, sql, id).Scan(
		&f.ID, &f.WorldID, &f.CompetitionID, &f.HomeClubID, &f.AwayClubID,
		&f.Matchday, &f.ScheduledAt, &f.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("fixture %s not found", id)
	}
	return f, err
}

// loadExisting reads a completed fixture's persisted match (idempotent retry
// path).
func loadExisting(ctx context.Context, tx pgx.Tx, fixtureID uuid.UUID) (*MatchResult, error) {
	m := &Match{}
	err := tx.QueryRow(ctx, `
		SELECT id, fixture_id, world_id, seed, engine_version, home_score, away_score, status, ended_at
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&m.ID, &m.FixtureID, &m.WorldID, &m.Seed, &m.EngineVersion,
			&m.HomeGoals, &m.AwayGoals, &m.Status, &m.EndedAt)
	if err != nil {
		return nil, err
	}
	evs, err := loadEvents(ctx, tx, m.ID)
	if err != nil {
		return nil, err
	}
	return &MatchResult{Match: m, Events: evs}, nil
}

func persistMatch(ctx context.Context, tx pgx.Tx, fixtureID, worldID uuid.UUID, seed int64, res matchsim.MatchResult) (uuid.UUID, time.Time, error) {
	var id uuid.UUID
	var now time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO match.matches
			(fixture_id, world_id, seed, engine_version, home_score, away_score, status, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'completed', $7, $7)
		RETURNING id, ended_at`,
		fixtureID, worldID, seed, matchsim.EngineVersion, res.HomeGoals, res.AwayGoals, time.Now().UTC()).
		Scan(&id, &now)
	return id, now, err
}

func persistEventsWithCasting(ctx context.Context, tx pgx.Tx, matchID uuid.UUID, evs []matchsim.MatchEvent, casters map[string]*sideCaster, startSeq int, forced map[int]map[string]forcedSub) ([]*MatchEventRow, error) {
	out := make([]*MatchEventRow, 0, len(evs))
	for i, ev := range evs {
		var playerID, relatedID *uuid.UUID
		if c, ok := casters[ev.ClubID]; ok {
			// A manager's chosen substitution replaces the caster's bench
			// draw, EXCEPT when this substitution is the injury-forced one
			// (preceded by an EventInjury for the same minute/club), which
			// always keeps the injury's pending-sub player.
			if ev.Type == matchsim.EventSubstitution {
				injDerived := i > 0 &&
					evs[i-1].Minute == ev.Minute &&
					evs[i-1].ClubID == ev.ClubID &&
					evs[i-1].Type == matchsim.EventInjury
				if f, ok := forced[ev.Minute][ev.ClubID]; ok && !injDerived {
					playerID, relatedID = c.resolveForced(ev, f.SubIn, f.SubOut)
				}
			}
			if playerID == nil && relatedID == nil {
				playerID, relatedID = c.resolve(ev)
			}
		}
		if ev.Sequence <= startSeq {
			continue // already persisted; the caster state still advanced above
		}
		var clubID *uuid.UUID
		if ev.ClubID != "" {
			if id, err := uuid.Parse(ev.ClubID); err == nil {
				clubID = &id
			}
		}
		detail, err := eventDetail(ev)
		if err != nil {
			return nil, err
		}
		rowID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO match.match_events
				(id, match_id, sequence, minute, event_type, club_id, player_id, related_player_id, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			rowID, matchID, ev.Sequence, ev.Minute, ev.Type, clubID, playerID, relatedID, detail); err != nil {
			return nil, fmt.Errorf("insert match event: %w", err)
		}
		out = append(out, &MatchEventRow{
			ID:              rowID,
			MatchID:         matchID,
			Sequence:        ev.Sequence,
			Minute:          ev.Minute,
			Type:            ev.Type,
			ClubID:          clubID,
			PlayerID:        playerID,
			RelatedPlayerID: relatedID,
			Detail:          append(json.RawMessage(nil), detail...),
		})
	}
	return out, nil
}

// eventDetail renders the engine's commentary as the match_events.detail JSONB
// (free-text commentary line, per the schema note).
func eventDetail(ev matchsim.MatchEvent) ([]byte, error) {
	m := map[string]string{"commentary": ev.Description}
	if ev.Detail != "" {
		m["detail"] = ev.Detail
	}
	return json.Marshal(m)
}

// applyForm updates both clubs' rolling form from the result vs the
// engine-mirrored expected margin (matchsim.GoalWeight incl. home advantage).
func (s *Service) applyForm(ctx context.Context, tx pgx.Tx, home, away *teamPlan, res matchsim.MatchResult, tick int64) error {
	return applyFormStates(ctx, tx, home.formState, away.formState, home.team, away.team, res, tick)
}

// applyFormStates is the snapshot-based twin of applyForm: it extends the same
// EWMA from frozen FormState values (the live path's sim_inputs snapshot) so
// a rehydrated worker applies the identical form after a restart.
func applyFormStates(ctx context.Context, tx pgx.Tx, homeFS, awayFS form.FormState, home, away matchsim.Team, res matchsim.MatchResult, tick int64) error {
	expectedHome := expectedHomeGD(home, away, matchsim.DefaultTuning())

	homeChar, awayChar := form.ResultWin, form.ResultDraw
	switch {
	case res.HomeGoals > res.AwayGoals:
		homeChar, awayChar = form.ResultWin, form.ResultLoss
	case res.HomeGoals < res.AwayGoals:
		homeChar, awayChar = form.ResultLoss, form.ResultWin
	}

	if err := updateFormState(ctx, tx, homeFS, float64(res.HomeGoals-res.AwayGoals)-expectedHome, tick, homeChar); err != nil {
		return err
	}
	return updateFormState(ctx, tx, awayFS, float64(res.AwayGoals-res.HomeGoals)+expectedHome, tick, awayChar)
}

func updateFormState(ctx context.Context, tx pgx.Tx, fs form.FormState, delta float64, tick int64, result string) error {
	next := form.Update(fs, form.ResultQualityDelta(delta), form.AlphaDefault, tick)
	next.FormString = form.FormStringFromResults(form.AppendResult(formWindow(fs.FormString), result))
	if _, err := tx.Exec(ctx, `
		INSERT INTO club.form_state (club_id, current_rating, last_updated_tick, form_string)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (club_id) DO UPDATE SET
			current_rating    = EXCLUDED.current_rating,
			last_updated_tick = EXCLUDED.last_updated_tick,
			form_string       = EXCLUDED.form_string`,
		next.ClubID, next.CurrentRating, next.LastUpdatedTick, next.FormString); err != nil {
		return fmt.Errorf("upsert form state: %w", err)
	}
	return nil
}

// formWindow parses the persisted read-model string (oldest first) back into
// a window of form.Update-style result strings.
func formWindow(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

// expectedHomeGD mirrors the engine's side.reset math: the home side's
// Attack/Defense carry the HomeAdvantageFactor; the expected margin is
// E[home scored] − E[home conceded].
func expectedHomeGD(home, away matchsim.Team, tuning matchsim.Tuning) float64 {
	homeA := float64(home.Attack) * tuning.HomeAdvantageFactor
	homeD := float64(home.Defense) * tuning.HomeAdvantageFactor
	scored := matchsim.GoalWeight(homeA, float64(away.Defense), tuning)
	conceded := matchsim.GoalWeight(float64(away.Attack), homeD, tuning)
	return scored - conceded
}

// fixtureContext assembles the stakes summary for one fixture. Six-pointer and
// dead-rubber are queried through the StandingsContext (nil → false until
// Phase 6 wires the competition service in).
func (s *Service) fixtureContext(ctx context.Context, tx pgx.Tx, f *Fixture) (squad.FixtureContext, error) {
	fc := squad.FixtureContext{LeagueTier: 10}

	var compType string
	var reputation int
	err := tx.QueryRow(ctx,
		`SELECT competition_type, reputation FROM competition.competitions WHERE id = $1`, f.CompetitionID).
		Scan(&compType, &reputation)
	if errors.Is(err, pgx.ErrNoRows) {
		compType = "custom"
	} else if err != nil {
		return fc, err
	}
	fc.IsCupTie = compType == "domestic_cup" || compType == "continental"
	if reputation > 0 {
		fc.LeagueTier = reputation
	}

	intensity, err := s.squad.LoadRivalryIntensity(ctx, f.HomeClubID, f.AwayClubID)
	if err != nil {
		return fc, err
	}
	fc.IsDerby = intensity >= squad.RivalryIntensityThreshold
	fc.DerbyIntensity = intensity

	if s.standings != nil {
		sp, err := s.standings.IsSixPointer(ctx, f.ID)
		if err != nil {
			return fc, err
		}
		dr, err := s.standings.IsDeadRubber(ctx, f.ID)
		if err != nil {
			return fc, err
		}
		fc.IsSixPointer = sp
		fc.IsDeadRubber = dr
	}
	return fc, nil
}

// ---------------------------------------------------------------------------
// Team assembly
// ---------------------------------------------------------------------------

// teamPlan is everything PlayFixture needs after building one side: the XI for
// casting, the team handed to the engine, and the pre-match state for form.
type teamPlan struct {
	club      squad.ClubRow
	dna       squad.ClubDNAInput
	xi        []squad.SquadMember
	bench     []squad.SquadMember
	taker     *squad.SquadMember
	warnings  []squad.LineupWarning
	team      matchsim.Team
	formState form.FormState
	isHome    bool
	worldTick int64
}

// buildTeam assembles one side: XI selection, the aggregation pipeline
// (morale → motivation → key players → performance factors → ratings → taker),
// and the engine Team. oppRep is the opponent's persisted reputation (drives
// the giant-killing gate in ComputeMotivation).
func (s *Service) buildTeam(ctx context.Context, f *Fixture, club squad.ClubRow, oppRep int, tick int64, fc squad.FixtureContext, seed int64, t matchsim.Tuning) (*teamPlan, error) {
	p := &teamPlan{club: club, isHome: club.ID == f.HomeClubID, worldTick: tick}

	dna, err := s.squad.LoadClubDNA(ctx, club.ID)
	if err != nil {
		return nil, err
	}
	p.dna = dna

	players, err := s.squad.LoadSquad(ctx, club.ID, f.ScheduledAt)
	if err != nil {
		return nil, err
	}

	var xi []squad.SquadMember
	if club.IsAIControlled {
		xi = squad.SelectStartersForAI(players, seed, club.ID)
	} else {
		lineup, err := s.squad.LoadLineup(ctx, club.ID)
		if err != nil {
			return nil, err
		}
		xi, err = squad.SelectStartersWithLineup(players, lineup)
		if err != nil {
			return nil, err
		}
	}
	p.xi = xi
	p.bench = benchFrom(players, xi)
	p.taker = chooseTaker(xi, penaltiesOf(players))

	squTuning := squad.ProposedTuning
	morale := squad.ComputeSquadMorale(moraleInputs(xi), squTuning)
	mot := squad.ComputeMotivation(dna, fc, oppRep-club.Reputation, seed, squTuning)

	factorMap := make(map[uuid.UUID]float64, len(xi))
	for _, in := range squad.SelectKeyPlayers(xi) {
		pf := squad.ComputePlayerPerformanceFactor(in, fc, seed, squTuning)
		factorMap[in.PlayerID] = pf.Factor
		if w, ok := squad.LineupWarningFor(in, fc, pf, seed, squTuning, 0); ok {
			p.warnings = append(p.warnings, w)
		}
	}
	att, def := squad.BuildSquadRatings(xi, squad.DefaultPositionWeights, factorMap)

	takerRate := 0.0
	if p.taker != nil {
		takerRate = squad.TakerPenaltyConversionRate(p.taker.Hidden, p.taker.CurrentSentiment)
	}

	fs, ok, err := s.form.Get(ctx, club.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		fs = form.Neutral(club.ID, tick)
	}
	p.formState = fs

	p.team = matchsim.Team{
		ID:                    club.ID.String(),
		ClubName:              club.Name,
		Attack:                att,
		Defense:               def,
		FormFactor:            fs.CurrentRating,
		MoraleFactor:          morale.Rating,
		MotivationFactor:      mot.Factor,
		Aggression:            50, // generated clubs have no DNA aggression column (engine normalises 0 the same)
		RivalryIntensity:      fc.DerbyIntensity,
		PenaltyConversionRate: takerRate,
	}
	return p, nil
}

// sortBench / helpers are in selection helpers below.

// benchFrom collects the top-5 available non-starters by attribute weight as
// the substitution pool the caster draws on (highest weight comes on first).
func benchFrom(players []squad.LoadedPlayer, xi []squad.SquadMember) []squad.SquadMember {
	inXI := make(map[uuid.UUID]bool, len(xi))
	for _, m := range xi {
		inXI[m.PlayerID] = true
	}
	var bench []squad.LoadedPlayer
	for _, p := range players {
		if p.Available && !inXI[p.PlayerID] {
			bench = append(bench, p)
		}
	}
	for i := 1; i < len(bench); i++ {
		for j := i; j > 0 && squad.MemberWeight(bench[j-1]) < squad.MemberWeight(bench[j]); j-- {
			bench[j-1], bench[j] = bench[j], bench[j-1]
		}
	}
	if len(bench) > 5 {
		bench = bench[:5]
	}
	out := make([]squad.SquadMember, 0, len(bench))
	for _, p := range bench {
		out = append(out, squad.ToSquadMember(p))
	}
	return out
}

// moraleInputs turns the XI into ComputeSquadMorale's weighted inputs.
func moraleInputs(xi []squad.SquadMember) []squad.PlayerMoraleInput {
	out := make([]squad.PlayerMoraleInput, 0, len(xi))
	for _, m := range xi {
		out = append(out, squad.PlayerMoraleInput{
			PlayerID:         m.PlayerID,
			Leadership:       m.Leadership,
			IsLikelyStarter:  true,
			CurrentSentiment: m.CurrentSentiment,
		})
	}
	return out
}

func penaltiesOf(players []squad.LoadedPlayer) map[uuid.UUID]int {
	out := make(map[uuid.UUID]int, len(players))
	for _, p := range players {
		out[p.PlayerID] = p.Penalties
	}
	return out
}

// chooseTaker designates the penalty taker: the highest penalties attribute in
// the XI, falling back to the captain, and finally nil (engine baseline 78%).
func chooseTaker(xi []squad.SquadMember, pen map[uuid.UUID]int) *squad.SquadMember {
	if len(xi) == 0 {
		return nil
	}
	best := xi[0]
	for _, m := range xi {
		if pen[m.PlayerID] > pen[best.PlayerID] {
			best = m
		}
	}
	if pen[best.PlayerID] <= 0 {
		if c := squad.PickCaptain(xi); c != nil {
			return c
		}
	}
	return &best
}

// ---------------------------------------------------------------------------
// World events
// ---------------------------------------------------------------------------

// worldEvents builds the events emitted alongside one fixture: a LINEUP_WARNING
// per flagged key player (actor = the club) and the MATCH_PLAYED summary
// (actor = system). The Quicksand path (PlayFixture) emits both together; the
// live path emits warnings at kickoff and MATCH_PLAYED at full time.
func (s *Service) worldEvents(f *Fixture, home, away *teamPlan, matchID uuid.UUID, seed int64, res matchsim.MatchResult, now time.Time) []*eventbus.Event {
	out := s.lineupWarningEvents(f, home, away, now)
	out = append(out, s.matchPlayedEvent(f.WorldID, home.worldTick, f.ID, matchID, seed, res, now))
	return out
}

// lineupWarningEvents builds one LINEUP_WARNING event per flagged key player,
// actored by the club (system when human-managed).
func (s *Service) lineupWarningEvents(f *Fixture, home, away *teamPlan, now time.Time) []*eventbus.Event {
	actorSystem := "system"
	actorAI := "ai_club"

	var out []*eventbus.Event
	for _, p := range []*teamPlan{home, away} {
		actorType := actorSystem
		var actorID *uuid.UUID
		if p.club.IsAIControlled {
			actorType = actorAI
			actorID = &p.club.ID
		}
		payload, _ := json.Marshal(map[string]string{
			"fixture_id": f.ID.String(),
			"club_id":    p.club.ID.String(),
		})
		for _, w := range p.warnings {
			expB, _ := json.Marshal(w.PolicyDecision)
			out = append(out, &eventbus.Event{
				ID:          uuid.New(),
				WorldID:     f.WorldID,
				WorldTick:   home.worldTick,
				EventType:   EventLineupWarning,
				ActorType:   &actorType,
				ActorID:     actorID,
				Payload:     payload,
				Explanation: expB,
				OccurredAt:  now,
			})
		}
	}
	return out
}

// matchPlayedEvent builds the MATCH_PLAYED summary event (actor = system).
func (s *Service) matchPlayedEvent(worldID uuid.UUID, worldTick int64, fixtureID, matchID uuid.UUID, seed int64, res matchsim.MatchResult, now time.Time) *eventbus.Event {
	actorSystem := "system"
	mpPayload, _ := json.Marshal(map[string]any{
		"fixture_id":      fixtureID.String(),
		"match_id":        matchID.String(),
		"home_score":      res.HomeGoals,
		"away_score":      res.AwayGoals,
		"home_possession": res.HomePossession,
		"engine_version":  matchsim.EngineVersion,
	})
	seedVal := seed
	return &eventbus.Event{
		ID:         uuid.New(),
		WorldID:    worldID,
		WorldTick:  worldTick,
		EventType:  EventMatchPlayed,
		ActorType:  &actorSystem,
		Payload:    mpPayload,
		RandomSeed: &seedVal,
		OccurredAt: now,
	}
}

// recordEvent appends one world.events row inside the caller's transaction and,
// when a bus is wired, enqueues its dispatch job in the same tx (the
// transactional outbox, OPD-23): a committed match can never be left
// undispatched, and publish failures abort the enclosing tx.
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, ev *eventbus.Event) error {
	return eventbus.WriteTx(ctx, s.bus, tx, ev)
}

// loadEvents reads the full typed, player-cast feed for a match.
func loadEvents(ctx context.Context, tx pgx.Tx, matchID uuid.UUID) ([]*MatchEventRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, match_id, sequence, minute, event_type, club_id, player_id, related_player_id, detail
		FROM match.match_events WHERE match_id = $1 ORDER BY sequence`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MatchEventRow
	for rows.Next() {
		e := &MatchEventRow{}
		var clubID, playerID, relatedID *uuid.UUID
		if err := rows.Scan(&e.ID, &e.MatchID, &e.Sequence, &e.Minute, &e.Type,
			&clubID, &playerID, &relatedID, &e.Detail); err != nil {
			return nil, err
		}
		e.ClubID = clubID
		e.PlayerID = playerID
		e.RelatedPlayerID = relatedID
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Read path
// ---------------------------------------------------------------------------

// GetFixture returns a fixture row for the feed/read path.
func (s *Service) GetFixture(ctx context.Context, id uuid.UUID) (*Fixture, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	f := &Fixture{}
	err = conn.QueryRow(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id,
		       COALESCE(f.matchday, 0), f.scheduled_at, f.status,
		       hc.name, ac.name
		FROM match.fixtures f
		JOIN club.clubs hc ON hc.id = f.home_club_id
		JOIN club.clubs ac ON ac.id = f.away_club_id
		WHERE f.id = $1`, id).
		Scan(&f.ID, &f.WorldID, &f.CompetitionID, &f.HomeClubID, &f.AwayClubID,
			&f.Matchday, &f.ScheduledAt, &f.Status,
			&f.HomeClubName, &f.AwayClubName)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// GetFixtureMatch aggregates the match-screen header for a fixture: the fixture
// with club names plus its match view (status, live clock, server-computed
// scoreline), or a nil Match when the fixture has not kicked off yet.
func (s *Service) GetFixtureMatch(ctx context.Context, fixtureID uuid.UUID) (*FixtureMatch, error) {
	f, err := s.GetFixture(ctx, fixtureID)
	if err != nil {
		return nil, err
	}
	out := &FixtureMatch{Fixture: f}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	mv := &MatchView{}
	err = conn.QueryRow(ctx, `
		SELECT id, status, COALESCE(current_minute, 0), COALESCE(home_score, 0), COALESCE(away_score, 0)
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&mv.ID, &mv.Status, &mv.Minute, &mv.HomeScore, &mv.AwayScore)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil // not kicked off yet
		}
		return nil, err
	}
	home, away, err := s.ScoreLine(ctx, mv.ID)
	if err != nil {
		return nil, err
	}
	mv.HomeScore = home
	mv.AwayScore = away
	out.Match = mv
	return out, nil
}

// GetMatchEvents returns a completed match's full cast feed.
func (s *Service) GetMatchEvents(ctx context.Context, matchID uuid.UUID) ([]*MatchEventRow, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	rows, err := conn.Query(ctx, `
		SELECT id, match_id, sequence, minute, event_type, club_id, player_id, related_player_id, detail
		FROM match.match_events WHERE match_id = $1 ORDER BY sequence`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MatchEventRow
	for rows.Next() {
		e := &MatchEventRow{}
		if err := rows.Scan(&e.ID, &e.MatchID, &e.Sequence, &e.Minute, &e.Type,
			&e.ClubID, &e.PlayerID, &e.RelatedPlayerID, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMatch returns a match row by id for the read/feed path.
func (s *Service) GetMatch(ctx context.Context, id uuid.UUID) (*Match, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	m := &Match{}
	err = conn.QueryRow(ctx, `
		SELECT id, fixture_id, world_id, seed, engine_version,
		       COALESCE(home_score, 0), COALESCE(away_score, 0), status, ended_at
		FROM match.matches WHERE id = $1`, id).
		Scan(&m.ID, &m.FixtureID, &m.WorldID, &m.Seed, &m.EngineVersion,
			&m.HomeGoals, &m.AwayGoals, &m.Status, &m.EndedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// CurrentMinute reads the live clock of a match (0 when never paced).
func (s *Service) CurrentMinute(ctx context.Context, matchID uuid.UUID) (int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var minute int
	if err := conn.QueryRow(ctx,
		`SELECT COALESCE(current_minute, 0) FROM match.matches WHERE id = $1`, matchID).
		Scan(&minute); err != nil {
		return 0, err
	}
	return minute, nil
}

// ScoreLine returns a match's goal tally server-side. While live it aggregates
// goal/penalty events from the same persisted feed the whole pipeline serves,
// so the client never computes outcomes; at completion the stamped match
// scorelines are returned directly. On sql.ErrNoRows the match does not exist.
func (s *Service) ScoreLine(ctx context.Context, matchID uuid.UUID) (int, int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Release()

	var status string
	var stampedHome, stampedAway int
	if err := conn.QueryRow(ctx, `
		SELECT status, COALESCE(home_score, 0), COALESCE(away_score, 0)
		FROM match.matches WHERE id = $1`, matchID).
		Scan(&status, &stampedHome, &stampedAway); err != nil {
		return 0, 0, err
	}
	if status == "completed" {
		return stampedHome, stampedAway, nil
	}

	var home, away int
	if err := conn.QueryRow(ctx, `
		SELECT
			count(me.id) FILTER (WHERE me.club_id = f.home_club_id AND me.event_type IN ('goal', 'penalty_scored')),
			count(me.id) FILTER (WHERE me.club_id = f.away_club_id AND me.event_type IN ('goal', 'penalty_scored'))
		FROM match.matches m
		JOIN match.fixtures f ON f.id = m.fixture_id
		LEFT JOIN match.match_events me ON me.match_id = m.id
		WHERE m.id = $1
		GROUP BY m.id`, matchID).Scan(&home, &away); err != nil {
		return 0, 0, err
	}
	return home, away, nil
}
