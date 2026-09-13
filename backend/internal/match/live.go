// Live match execution for the S04-02 milestone (OPD-21): a match that kicks
// off goes 'live' and is paced in real time by a per-world goroutine, fully
// decoupled from the world clock. The pacing goroutine re-runs the pure,
// seeded engine one minute at a time — Simulate(seed + ordered live inputs) —
// persists only the events of the new minute, and sleeps the world's
// tick.match_cadence between minutes. Because the entire simulation input is
// frozen (migration 0031 sim_inputs snapshot) plus the ordered match.match_inputs
// feed, any worker can rehydrate an in-progress match and resume it to the
// identical outcome after a crash.
//
// pkg/matchsim stays pure; every database read/write and bus emission lives
// in this file.
package match

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/matchsim"
)

// Sentinel errors for manager live-input ingestion (S05-01 surfaces them).
var (
	ErrMatchNotLive       = errors.New("match is not in progress")
	ErrManagerNotInvolved = errors.New("manager does not control either club of this match")
	ErrNotAtSubWindow     = errors.New("substitutions are only accepted at the canonical windows")
	ErrMinuteClosed       = errors.New("input minute is before the current match minute")
	ErrPlayerNotOnPitch   = errors.New("player is not on the pitch")
	ErrPlayerNotOnBench   = errors.New("player is not on the bench")
	ErrDuplicateWindowSub = errors.New("a substitution for this window is already set")
	ErrInvalidTacticStyle = errors.New("tactic change must name one of the approved styles")
)

// liveCadenceKey is the world_config key carrying the per-minute pacing. It is
// seeded as a Go duration string ("20s") at launch; a malformed or cron-form
// value falls back to matchsim.DefaultTuning().LivePacingSecondsPerMinute. The world clock
// ignores it (OPD-17(2)); only the live match runner consumes it.
const liveCadenceKey = "tick.match_cadence"

// simInputs is the fully frozen simulation input persisted on kickoff
// (match.matches.sim_inputs). It captures everything the engine reads from the
// live database at kickoff time so the live stream and a rehydrated worker
// replay the identical match regardless of later squad/form changes.
type simInputs struct {
	HomeTeam       matchsim.Team        `json:"home_team"`
	AwayTeam       matchsim.Team        `json:"away_team"`
	HomeXI         []squad.SquadMember  `json:"home_xi"`
	AwayXI         []squad.SquadMember  `json:"away_xi"`
	HomeBench      []squad.SquadMember  `json:"home_bench"`
	AwayBench      []squad.SquadMember  `json:"away_bench"`
	HomeTaker      *squad.SquadMember   `json:"home_taker,omitempty"`
	AwayTaker      *squad.SquadMember   `json:"away_taker,omitempty"`
	FormHome       form.FormState       `json:"form_home"`
	FormAway       form.FormState       `json:"form_away"`
	WorldTick      int64                `json:"world_tick"`
	FixtureContext squad.FixtureContext `json:"fixture_context"`
}

// LiveSession is one in-progress live match bound to a pacing goroutine.
// nextMinute is the next simulated minute to produce (1..90); a value above 90
// means full time was reached and Finalize is pending.
type LiveSession struct {
	MatchID     uuid.UUID
	FixtureID   uuid.UUID
	WorldID     uuid.UUID
	HomeClubID  uuid.UUID
	AwayClubID  uuid.UUID
	ScheduledAt time.Time
	Seed        int64
	Home        matchsim.Team
	Away        matchsim.Team
	homeXI      []squad.SquadMember
	awayXI      []squad.SquadMember
	homeBench   []squad.SquadMember
	awayBench   []squad.SquadMember
	homeTaker   *squad.SquadMember
	awayTaker   *squad.SquadMember
	formHome    form.FormState
	formAway    form.FormState
	fc          squad.FixtureContext
	worldTick   int64
	pacing      time.Duration
	nextMinute  int
}

// NextMinute reports the next simulated minute (1..90; >90 = full time).
func (s *LiveSession) NextMinute() int { return s.nextMinute }

// Pacing reports the world cadence resolved at kickoff (per simulated minute).
func (s *LiveSession) Pacing() time.Duration { return s.pacing }

// MatchFinalized is the result of a live match at full time, ready for
// standings application by the matchday runner.
type MatchFinalized struct {
	FixtureID      uuid.UUID
	MatchID        uuid.UUID
	WorldID        uuid.UUID
	Seed           int64
	HomeGoals      int
	AwayGoals      int
	HomePossession float64
	EndedAt        time.Time
}

// KickoffMatchday marks every still-scheduled fixture of a matchday 'live',
// freezes its simulation input snapshot, and returns its sessions. It is
// idempotent: fixtures already 'live' or 'completed' (redelivered ticks) are
// skipped. The returned sessions feed the real-time pacing goroutine.
func (s *Service) KickoffMatchday(ctx context.Context, worldID uuid.UUID, matchday int) ([]*LiveSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM match.fixtures
		WHERE world_id = $1 AND matchday = $2 AND status = 'scheduled'
		ORDER BY scheduled_at`, worldID, matchday)
	if err != nil {
		return nil, fmt.Errorf("kickoff matchday: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("kickoff matchday: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kickoff matchday: iterate: %w", err)
	}

	out := make([]*LiveSession, 0, len(ids))
	for _, id := range ids {
		sess, err := s.kickoffFixture(ctx, id)
		if err != nil {
			return out, err
		}
		if sess != nil {
			out = append(out, sess)
		}
	}
	return out, nil
}

// kickoffFixture freezes one fixture by id: marks it live, builds + persists
// the simulation snapshot, and books home/away lineup warnings. The fixture
// row is locked FOR UPDATE, so two concurrent kickoffs serialize and the
// second becomes an idempotent no-op (returns nil).
func (s *Service) kickoffFixture(ctx context.Context, fixtureID uuid.UUID) (*LiveSession, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	f, err := loadFixture(ctx, tx, fixtureID, true)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: %w", err)
	}
	switch f.Status {
	case fixtureLive, fixtureCompleted:
		// Concurrent/redelivered kickoff: already handled.
		return nil, nil
	case fixtureScheduled:
	default:
		return nil, fmt.Errorf("kickoff fixture %s: not kickable (status %q)", fixtureID, f.Status)
	}

	tick, err := s.form.WorldTick(ctx, f.WorldID)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: %w", err)
	}
	tuning := matchsim.DefaultTuning()
	seed := fixtureSeed(fixtureID)
	fc, err := s.fixtureContext(ctx, tx, f)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: %w", err)
	}

	homeClub, err := s.squad.LoadClub(ctx, f.HomeClubID)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: home club: %w", err)
	}
	awayClub, err := s.squad.LoadClub(ctx, f.AwayClubID)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: away club: %w", err)
	}

	homePlan, err := s.buildTeam(ctx, f, homeClub, awayClub.Reputation, tick, fc, seed, tuning)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: home team: %w", err)
	}
	awayPlan, err := s.buildTeam(ctx, f, awayClub, homeClub.Reputation, tick, fc, seed, tuning)
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: away team: %w", err)
	}

	pacing := s.resolveMatchPacing(ctx, f.WorldID)
	raw, err := json.Marshal(simInputs{
		HomeTeam:       homePlan.team,
		AwayTeam:       awayPlan.team,
		HomeXI:         homePlan.xi,
		AwayXI:         awayPlan.xi,
		HomeBench:      homePlan.bench,
		AwayBench:      awayPlan.bench,
		HomeTaker:      homePlan.taker,
		AwayTaker:      awayPlan.taker,
		FormHome:       homePlan.formState,
		FormAway:       awayPlan.formState,
		WorldTick:      tick,
		FixtureContext: fc,
	})
	if err != nil {
		return nil, fmt.Errorf("kickoff fixture: marshal snapshot: %w", err)
	}

	var matchID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO match.matches
			(fixture_id, world_id, seed, engine_version, status, started_at, sim_inputs, pacing_millis)
		VALUES ($1, $2, $3, $4, 'in_progress', $5, $6, $7)
		RETURNING id`,
		fixtureID, f.WorldID, seed, matchsim.EngineVersion, time.Now().UTC(), raw, int(pacing.Milliseconds()),
	).Scan(&matchID); err != nil {
		return nil, fmt.Errorf("kickoff fixture: insert match: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE match.fixtures SET status = 'live' WHERE id = $1 AND status = 'scheduled'`, fixtureID); err != nil {
		return nil, fmt.Errorf("kickoff fixture: mark live: %w", err)
	}

	now := time.Now().UTC()
	warnings := s.lineupWarningEvents(f, homePlan, awayPlan, now)
	for _, ev := range warnings {
		if err := s.recordEvent(ctx, tx, ev); err != nil {
			return nil, fmt.Errorf("kickoff fixture: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("kickoff fixture: commit: %w", err)
	}

	return &LiveSession{
		MatchID:     matchID,
		FixtureID:   fixtureID,
		WorldID:     f.WorldID,
		HomeClubID:  f.HomeClubID,
		AwayClubID:  f.AwayClubID,
		ScheduledAt: f.ScheduledAt,
		Seed:        seed,
		Home:        homePlan.team,
		Away:        awayPlan.team,
		homeXI:      homePlan.xi,
		awayXI:      awayPlan.xi,
		homeBench:   homePlan.bench,
		awayBench:   awayPlan.bench,
		homeTaker:   homePlan.taker,
		awayTaker:   awayPlan.taker,
		formHome:    homePlan.formState,
		formAway:    awayPlan.formState,
		fc:          fc,
		worldTick:   tick,
		pacing:      pacing,
		nextMinute:  1,
	}, nil
}

// resolveMatchPacing reads the world's tick.match_cadence as a Go duration.
// Anything malformed, non-duration, or absent falls back to
// matchsim.DefaultTuning().LivePacingSecondsPerMinute (the 20s proposal).
func (s *Service) resolveMatchPacing(ctx context.Context, worldID uuid.UUID) time.Duration {
	p := matchsim.DefaultTuning().LivePacingSecondsPerMinute
	var raw []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT config_value FROM world.world_config
		WHERE world_id = $1 AND config_key = $2`, worldID, liveCadenceKey).Scan(&raw); err != nil {
		return p
	}
	var spec string
	if err := json.Unmarshal(raw, &spec); err != nil {
		return p
	}
	d, err := time.ParseDuration(spec)
	if err != nil || d <= 0 {
		return p
	}
	return d
}

// PaceMinute runs the pure engine up to one simulated minute and persists
// exactly the events of that minute: Simulate(seed + ordered inputs with
// minute <= m). The casters are rebuilt from scratch every call and only the
// tail beyond the last persisted sequence is written, so a crash can never
// persist half a minute (one minute per tx) and the identical match resumes
// from seed + snapshot + inputs. The returned rows are exactly the persisted
// feed for this minute (S04-03 publishes them as the live match_tick
// envelope). finished reports that full time was reached.
func (s *Service) PaceMinute(ctx context.Context, sess *LiveSession) ([]*MatchEventRow, bool, error) {
	if sess.nextMinute > 90 {
		return nil, true, nil
	}
	m := sess.nextMinute

	inputs, err := s.loadMatchInputs(ctx, sess.MatchID)
	if err != nil {
		return nil, false, err
	}
	res := matchsim.Simulate(matchsim.Options{
		Seed:       sess.Seed,
		Home:       sess.Home,
		Away:       sess.Away,
		Tuning:     matchsim.DefaultTuning(),
		LiveInputs: inputsUpTo(inputs, m),
	})

	// Persist only this minute's events: everything up to m is already on
	// disk (sequences < the tail), so filter keeps one minute per tx and the
	// identical match resumes from seed + snapshot + inputs.
	minuteEvents := make([]matchsim.MatchEvent, 0, 8)
	for _, e := range res.Events {
		if e.Minute == m {
			minuteEvents = append(minuteEvents, e)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("pace minute: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Serialize pacing of this match: lock the row, then read the last
	// persisted sequence, so two goroutines can never write the same sequence.
	var lockID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM match.matches WHERE id = $1 FOR UPDATE`, sess.MatchID).Scan(&lockID); err != nil {
		return nil, false, fmt.Errorf("pace minute: lock match: %w", err)
	}
	var lastSeq int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(sequence), 0) FROM match.match_events WHERE match_id = $1`, sess.MatchID).
		Scan(&lastSeq); err != nil {
		return nil, false, fmt.Errorf("pace minute: last sequence: %w", err)
	}

	casters := map[string]*sideCaster{
		sess.HomeClubID.String(): newSideCaster(sess.Seed, sess.HomeClubID, sess.homeXI, sess.homeBench, sess.homeTaker),
		sess.AwayClubID.String(): newSideCaster(sess.Seed, sess.AwayClubID, sess.awayXI, sess.awayBench, sess.awayTaker),
	}
	rows, err := persistEventsWithCasting(ctx, tx, sess.MatchID, minuteEvents, casters, lastSeq, forcedSubs(inputsUpTo(inputs, m)))
	if err != nil {
		return nil, false, fmt.Errorf("pace minute: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE match.matches SET current_minute = $2 WHERE id = $1`, sess.MatchID, m); err != nil {
		return nil, false, fmt.Errorf("pace minute: advance clock: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("pace minute: commit: %w", err)
	}

	sess.nextMinute = m + 1
	return rows, sess.nextMinute > 90, nil
}

// Finalize completes a fully-paced live match: it re-runs the engine over the
// complete persisted input stream, stamps the final scorelines on the match
// and fixture, updates both clubs' rolling form, and records MATCH_PLAYED.
// Idempotent: a match already 'completed' (redelivery) loads its persisted
// result instead of writing again.
func (s *Service) Finalize(ctx context.Context, sess *LiveSession) (*MatchFinalized, error) {
	if sess.nextMinute <= 90 {
		return nil, fmt.Errorf("finalize %s: match not at full time (next minute %d)", sess.MatchID, sess.nextMinute)
	}

	inputs, err := s.loadMatchInputs(ctx, sess.MatchID)
	if err != nil {
		return nil, err
	}
	res := matchsim.Simulate(matchsim.Options{
		Seed:       sess.Seed,
		Home:       sess.Home,
		Away:       sess.Away,
		Tuning:     matchsim.DefaultTuning(),
		LiveInputs: inputs,
	})
	now := time.Now().UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("finalize: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM match.matches WHERE id = $1 FOR UPDATE`, sess.MatchID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("finalize: match %s not found", sess.MatchID)
		}
		return nil, fmt.Errorf("finalize: load match: %w", err)
	}
	if status == "completed" {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("finalize: commit: %w", err)
		}
		m := &Match{}
		if err := s.pool.QueryRow(ctx, `
			SELECT id, fixture_id, world_id, seed, home_score, away_score, ended_at
			FROM match.matches WHERE id = $1`, sess.MatchID).
			Scan(&m.ID, &m.FixtureID, &m.WorldID, &m.Seed, &m.HomeGoals, &m.AwayGoals, &m.EndedAt); err != nil {
			return nil, fmt.Errorf("finalize: load completed: %w", err)
		}
		return &MatchFinalized{
			FixtureID: m.FixtureID, MatchID: m.ID, WorldID: m.WorldID, Seed: m.Seed,
			HomeGoals: m.HomeGoals, AwayGoals: m.AwayGoals, EndedAt: *m.EndedAt,
		}, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE match.matches SET status = 'completed', home_score = $2, away_score = $3, ended_at = $4
		WHERE id = $1`, sess.MatchID, res.HomeGoals, res.AwayGoals, now); err != nil {
		return nil, fmt.Errorf("finalize: complete match: %w", err)
	}
	if err := applyFormStates(ctx, tx, sess.formHome, sess.formAway, sess.Home, sess.Away, res, sess.worldTick); err != nil {
		return nil, fmt.Errorf("finalize: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE match.fixtures SET status = 'completed' WHERE id = $1 AND status = 'live'`, sess.FixtureID); err != nil {
		return nil, fmt.Errorf("finalize: complete fixture: %w", err)
	}

	ev := s.matchPlayedEvent(sess.WorldID, sess.worldTick, sess.FixtureID, sess.MatchID, sess.Seed, res, now)
	if err := s.recordEvent(ctx, tx, ev); err != nil {
		return nil, fmt.Errorf("finalize: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("finalize: commit: %w", err)
	}

	return &MatchFinalized{
		FixtureID: sess.FixtureID, MatchID: sess.MatchID, WorldID: sess.WorldID, Seed: sess.Seed,
		HomeGoals: res.HomeGoals, AwayGoals: res.AwayGoals, HomePossession: res.HomePossession, EndedAt: now,
	}, nil
}

// LoadLiveSessions rehydrates every in-progress live match of a world from its
// frozen snapshot: the worker resumes pacing from the last persisted minute.
// It is the startup-sweep / redelivery entry point.
func (s *Service) LoadLiveSessions(ctx context.Context, worldID uuid.UUID) ([]*LiveSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.home_club_id, f.away_club_id, f.scheduled_at,
		       m.id, m.seed, m.sim_inputs, COALESCE(m.pacing_millis, 0), m.current_minute
		FROM match.fixtures f
		JOIN match.matches m ON m.fixture_id = f.id
		WHERE f.world_id = $1 AND f.status = 'live' AND m.status = 'in_progress'
		ORDER BY f.scheduled_at, f.id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load live sessions: %w", err)
	}
	defer rows.Close()

	var out []*LiveSession
	for rows.Next() {
		var (
			fixtureID, matchID, homeClubID, awayClubID, worldIDX uuid.UUID
			scheduledAt                                          time.Time
			seed                                                 int64
			raw                                                  []byte
			pacingMillis, currentMinute                          int
		)
		if err := rows.Scan(&fixtureID, &worldIDX, &homeClubID, &awayClubID, &scheduledAt,
			&matchID, &seed, &raw, &pacingMillis, &currentMinute); err != nil {
			return nil, fmt.Errorf("load live sessions: scan: %w", err)
		}
		snap := &simInputs{}
		if err := json.Unmarshal(raw, snap); err != nil {
			return nil, fmt.Errorf("load live sessions: snapshot: %w", err)
		}
		pacing := matchsim.DefaultTuning().LivePacingSecondsPerMinute
		if pacingMillis > 0 {
			pacing = time.Duration(pacingMillis) * time.Millisecond
		}
		out = append(out, &LiveSession{
			MatchID:     matchID,
			FixtureID:   fixtureID,
			WorldID:     worldIDX,
			HomeClubID:  homeClubID,
			AwayClubID:  awayClubID,
			ScheduledAt: scheduledAt,
			Seed:        seed,
			Home:        snap.HomeTeam,
			Away:        snap.AwayTeam,
			homeXI:      snap.HomeXI,
			awayXI:      snap.AwayXI,
			homeBench:   snap.HomeBench,
			awayBench:   snap.AwayBench,
			homeTaker:   snap.HomeTaker,
			awayTaker:   snap.AwayTaker,
			formHome:    snap.FormHome,
			formAway:    snap.FormAway,
			fc:          snap.FixtureContext,
			worldTick:   snap.WorldTick,
			pacing:      pacing,
			nextMinute:  currentMinute + 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load live sessions: iterate: %w", err)
	}
	return out, nil
}

// Substitute records one manager substitution into a live match's ordered
// input stream. Validation enforces: the match is in progress, the manager
// controls one of the two clubs, the minute is a canonical window that has not
// closed yet, the outgoing player is on the pitch, the incoming player is on
// the bench, and no substitution is already set for that window/side. On the
// next PaceMinute reaching that minute the pure engine consumes the input and
// the caster forces the exact chosen players.
func (s *Service) Substitute(ctx context.Context, matchID, managerID uuid.UUID, minute int, playerOut, playerIn uuid.UUID) error {
	if playerOut == playerIn || playerOut == uuid.Nil || playerIn == uuid.Nil {
		return fmt.Errorf("substitute: players must be distinct and set")
	}
	live, err := s.liveMatchContext(ctx, matchID, managerID)
	if err != nil {
		return err
	}
	if !containsInt(matchsim.DefaultTuning().SubWindows, minute) {
		return ErrNotAtSubWindow
	}
	if minute < live.currentMinute+1 || minute < 1 || minute > 90 {
		return ErrMinuteClosed
	}

	xi, bench := live.snap.HomeXI, live.snap.HomeBench
	if !live.isHome {
		xi, bench = live.snap.AwayXI, live.snap.AwayBench
	}
	var onPitch, onBench bool
	for _, m := range xi {
		if m.PlayerID == playerOut {
			onPitch = true
		}
		if m.PlayerID == playerIn {
			return ErrPlayerNotOnBench // an XI player is not a legal sub
		}
	}
	for _, m := range bench {
		if m.PlayerID == playerIn {
			onBench = true
		}
	}
	if !onPitch {
		return ErrPlayerNotOnPitch
	}
	if !onBench {
		return ErrPlayerNotOnBench
	}

	var dup bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM match.match_inputs
			WHERE match_id = $1 AND minute = $2 AND kind = 'substitution' AND payload->>'club_id' = $3
		)`, matchID, minute, live.clubID.String()).Scan(&dup); err != nil {
		return fmt.Errorf("substitute: duplicate check: %w", err)
	}
	if dup {
		return ErrDuplicateWindowSub
	}

	seq, err := s.nextInputSeq(ctx, matchID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"club_id":    live.clubID.String(),
		"player_in":  playerIn.String(),
		"player_out": playerOut.String(),
	})
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO match.match_inputs (match_id, world_id, sequence, minute, kind, payload, created_by_manager_id)
		VALUES ($1, $2, $3, $4, 'substitution', $5, $6)`,
		matchID, live.worldID, seq, minute, payload, managerID); err != nil {
		return fmt.Errorf("substitute: insert input: %w", err)
	}
	return nil
}

// TacticChange records one manager tactic change into the ordered input
// stream. The engine consumes tactic_change inputs for replay and switches the
// side's style from that minute (v1.5). The stored payload is normalised to
// {"club_id", "style"}; the kind is always written as tactic_change —
// through the API the client submits "tactical_change" (spec S05-01 §2.2) and
// this service normalises it on the way in. Validation mirrors Substitute
// minus the window restriction, plus a style-key check.
func (s *Service) TacticChange(ctx context.Context, matchID, managerID uuid.UUID, minute int, tactic map[string]any) error {
	if tactic == nil {
		return fmt.Errorf("tactic change: tactic required")
	}
	style, ok := tactic["style"].(string)
	if !ok || !matchsim.IsStyle(style) {
		return ErrInvalidTacticStyle
	}
	live, err := s.liveMatchContext(ctx, matchID, managerID)
	if err != nil {
		return err
	}
	if minute < live.currentMinute+1 || minute < 1 || minute > 90 {
		return ErrMinuteClosed
	}

	seq, err := s.nextInputSeq(ctx, matchID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"club_id": live.clubID.String(),
		"style":   style,
	})
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO match.match_inputs (match_id, world_id, sequence, minute, kind, payload, created_by_manager_id)
		VALUES ($1, $2, $3, $4, 'tactic_change', $5, $6)`,
		matchID, live.worldID, seq, minute, payload, managerID); err != nil {
		return fmt.Errorf("tactic change: insert input: %w", err)
	}
	return nil
}

// liveMatchContext is the shared validation view of one live match: the
// manager's side (isHome), the frozen snapshot, the world id, and the
// authoritative match clock (currentMinute).
type liveMatchContext struct {
	isHome        bool
	clubID        uuid.UUID
	worldID       uuid.UUID
	snap          *simInputs
	currentMinute int
}

func (s *Service) liveMatchContext(ctx context.Context, matchID, managerID uuid.UUID) (*liveMatchContext, error) {
	var (
		status        string
		worldID       uuid.UUID
		home, away    uuid.UUID
		currentMinute int
		raw           []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT m.status, m.world_id, f.home_club_id, f.away_club_id, m.current_minute, m.sim_inputs
		FROM match.matches m JOIN match.fixtures f ON f.id = m.fixture_id
		WHERE m.id = $1`, matchID,
	).Scan(&status, &worldID, &home, &away, &currentMinute, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("live input: match %s not found", matchID)
	}
	if err != nil {
		return nil, fmt.Errorf("live input: load match: %w", err)
	}
	if status != "in_progress" {
		return nil, ErrMatchNotLive
	}

	managerClub, err := s.managerClub(ctx, managerID)
	if err != nil {
		return nil, err
	}
	switch managerClub {
	case home:
	case away:
	default:
		return nil, ErrManagerNotInvolved
	}

	snap := &simInputs{}
	if err := json.Unmarshal(raw, snap); err != nil {
		return nil, fmt.Errorf("live input: snapshot: %w", err)
	}
	return &liveMatchContext{
		isHome:        managerClub == home,
		clubID:        managerClub,
		worldID:       worldID,
		snap:          snap,
		currentMinute: currentMinute,
	}, nil
}

// managerClub resolves an active manager's current club.
func (s *Service) managerClub(ctx context.Context, managerID uuid.UUID) (uuid.UUID, error) {
	var club uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID).Scan(&club)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrManagerNotInvolved
	}
	return club, err
}

// nextInputSeq allocates the next sequence number for a match input stream.
func (s *Service) nextInputSeq(ctx context.Context, matchID uuid.UUID) (int, error) {
	var seq int
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM match.match_inputs WHERE match_id = $1`, matchID).
		Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

// loadMatchInputs reads the complete ordered manager input stream of a live
// match (sequence order = submission order).
func (s *Service) loadMatchInputs(ctx context.Context, matchID uuid.UUID) ([]matchsim.LiveInput, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT minute, kind, payload FROM match.match_inputs
		WHERE match_id = $1 ORDER BY sequence`, matchID)
	if err != nil {
		return nil, fmt.Errorf("load match inputs: %w", err)
	}
	defer rows.Close()

	var out []matchsim.LiveInput
	for rows.Next() {
		var (
			minute int
			kind   string
			raw    []byte
		)
		if err := rows.Scan(&minute, &kind, &raw); err != nil {
			return nil, fmt.Errorf("load match inputs: scan: %w", err)
		}
		var detail map[string]any
		if err := json.Unmarshal(raw, &detail); err != nil {
			return nil, fmt.Errorf("load match inputs: payload: %w", err)
		}
		// Normalise the "tactical_change" spelling on ingress (defensive:
		// through the API TacticChange already writes tactic_change).
		if kind == "tactical_change" {
			kind = "tactic_change"
		}
		clubID, _ := detail["club_id"].(string)
		out = append(out, matchsim.LiveInput{Minute: minute, ClubID: clubID, Kind: kind, Detail: detail})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load match inputs: iterate: %w", err)
	}
	return out, nil
}

// inputsUpTo keeps only inputs whose Minute is at or before m.
func inputsUpTo(inputs []matchsim.LiveInput, m int) []matchsim.LiveInput {
	out := make([]matchsim.LiveInput, 0, len(inputs))
	for _, in := range inputs {
		if in.Minute <= m {
			out = append(out, in)
		}
	}
	return out
}

// forcedSub carries the exact player-in/player-out a manager chose; the caster
// uses it to link a substitution event instead of its own bench draw.
type forcedSub struct {
	SubIn  uuid.UUID
	SubOut uuid.UUID
}

// forcedSubs indexes manager substitutions by (minute, club) for casting.
func forcedSubs(inputs []matchsim.LiveInput) map[int]map[string]forcedSub {
	out := make(map[int]map[string]forcedSub)
	for _, in := range inputs {
		if in.Kind != "substitution" {
			continue
		}
		playerIn, _ := in.Detail["player_in"].(string)
		playerOut, _ := in.Detail["player_out"].(string)
		inID, err1 := uuid.Parse(playerIn)
		outID, err2 := uuid.Parse(playerOut)
		if err1 != nil || err2 != nil {
			continue
		}
		if out[in.Minute] == nil {
			out[in.Minute] = make(map[string]forcedSub)
		}
		out[in.Minute][in.ClubID] = forcedSub{SubIn: inID, SubOut: outID}
	}
	return out
}

// containsInt reports whether xs contains v.
func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
