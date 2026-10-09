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
	"github.com/touchline/backend/internal/injury"
	"github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/matchsim"
)

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
		&f.ID, &f.WorldID, &f.Competition.ID, &f.HomeClub.ID, &f.AwayClub.ID,
		&f.Matchday, &f.ScheduledAt, &f.Status)
	f.Gameweek = f.Matchday
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
		SELECT id, fixture_id, world_id, seed, engine_version, home_score, away_score,
		       home_xg::float8, away_xg::float8, status, ended_at
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&m.ID, &m.FixtureID, &m.WorldID, &m.Seed, &m.EngineVersion,
			&m.HomeGoals, &m.AwayGoals, &m.HomeXG, &m.AwayXG, &m.Status, &m.EndedAt)
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
			(fixture_id, world_id, seed, engine_version, home_score, away_score, home_xg, away_xg, status, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, $8, $9, 'completed', $7, $7)
		RETURNING id, ended_at`,
		fixtureID, worldID, seed, matchsim.EngineVersion, res.HomeGoals, res.AwayGoals, time.Now().UTC(),
		res.HomeXG, res.AwayXG).
		Scan(&id, &now)
	return id, now, err
}

func persistEvents(ctx context.Context, tx pgx.Tx, matchID uuid.UUID, evs []matchsim.MatchEvent, startSeq int) ([]*MatchEventRow, error) {
	out := make([]*MatchEventRow, 0, len(evs))
	for _, ev := range evs {
		playerID, relatedID := eventPlayers(ev)
		if ev.Sequence <= startSeq {
			continue // already persisted
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
			ID:            rowID,
			Match:         apiref.MatchRef{ID: matchID},
			Sequence:      ev.Sequence,
			Minute:        ev.Minute,
			Type:          ev.Type,
			Club:          clubRef(clubID),
			Player:        playerRef(playerID),
			RelatedPlayer: playerRef(relatedID),
			Detail:        append(json.RawMessage(nil), detail...),
		})
	}
	return out, nil
}

// eventPlayers converts the engine's attribution-pass linkage into row ids. A
// blank id (a side without lineups, or a structural event) stays nil.
func eventPlayers(ev matchsim.MatchEvent) (playerID, relatedID *uuid.UUID) {
	if ev.PlayerID != "" {
		if id, err := uuid.Parse(ev.PlayerID); err == nil {
			playerID = &id
		}
	}
	if ev.RelatedPlayerID != "" {
		if id, err := uuid.Parse(ev.RelatedPlayerID); err == nil {
			relatedID = &id
		}
	}
	return playerID, relatedID
}

// clubRef wraps an optional club id into a nested ref (nil stays nil).
func clubRef(id *uuid.UUID) *apiref.ClubRef {
	if id == nil {
		return nil
	}
	return &apiref.ClubRef{ID: *id}
}

// playerRef wraps an optional player id into a nested ref (nil stays nil).
func playerRef(id *uuid.UUID) *apiref.PlayerRef {
	if id == nil {
		return nil
	}
	return &apiref.PlayerRef{ID: *id}
}

// resolveEventRefs decorates a batch of persisted event rows with the club and
// player display names so REST feeds and live ticks carry nested {id,name}
// refs. Best-effort: an id that no longer resolves keeps an id-only ref.
func resolveEventRefs(ctx context.Context, conn interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, rows []*MatchEventRow) {
	clubIDs := make(map[uuid.UUID]struct{})
	playerIDs := make(map[uuid.UUID]struct{})
	for _, e := range rows {
		if e.Club != nil {
			clubIDs[e.Club.ID] = struct{}{}
		}
		if e.Player != nil {
			playerIDs[e.Player.ID] = struct{}{}
		}
		if e.RelatedPlayer != nil {
			playerIDs[e.RelatedPlayer.ID] = struct{}{}
		}
	}
	names := make(map[uuid.UUID]string, len(clubIDs)+len(playerIDs))
	if len(clubIDs) > 0 {
		if rs, err := conn.Query(ctx,
			`SELECT id, name FROM club.clubs WHERE id = ANY($1::uuid[])`, uuidSet(clubIDs)); err == nil {
			for rs.Next() {
				var id uuid.UUID
				var name string
				if rs.Scan(&id, &name) == nil {
					names[id] = name
				}
			}
			rs.Close()
		}
	}
	if len(playerIDs) > 0 {
		if rs, err := conn.Query(ctx, `
			SELECT p.id, pe.display_name
			FROM player.players p
			JOIN person.people pe ON pe.id = p.person_id
			WHERE p.id = ANY($1::uuid[])`, uuidSet(playerIDs)); err == nil {
			for rs.Next() {
				var id uuid.UUID
				var name string
				if rs.Scan(&id, &name) == nil {
					names[id] = name
				}
			}
			rs.Close()
		}
	}
	for _, e := range rows {
		if e.Club != nil {
			e.Club.Name = names[e.Club.ID]
		}
		if e.Player != nil {
			e.Player.Name = names[e.Player.ID]
		}
		if e.RelatedPlayer != nil {
			e.RelatedPlayer.Name = names[e.RelatedPlayer.ID]
		}
	}
	// Names in hand, the engine's role placeholders ("{player}", "{assist}",
	// "{sub}") become real names in the wire text — last, because it needs the
	// names resolveEventRefs just looked up.
	resolveCommentary(rows)
}

// uuidSet flattens a set of ids into a stable slice for ANY() parameters.
func uuidSet(set map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// lineupsTeam stamps a plan's frozen XI/bench/taker onto the engine Team so the
// v1.6 attribution pass can link events and derive ratings for this side.
func lineupsTeam(p *teamPlan) matchsim.Team {
	t := p.team
	if p.xi != nil && len(p.xi) == 0 {
		return t
	}
	li := playerLineupsFor(p.xi, p.bench, p.taker)
	if li != nil {
		t.Lineups = li
	}
	return t
}

// playerLineupsFor converts snapshot lineups into the engine's attribution
// input. Returns nil when no XI is present (a side without players).
func playerLineupsFor(xi, bench []squad.SquadMember, taker *squad.SquadMember) *matchsim.PlayerLineups {
	if len(xi) == 0 {
		return nil
	}
	li := &matchsim.PlayerLineups{
		XI:    make([]matchsim.PlayerRef, 0, len(xi)),
		Bench: make([]matchsim.PlayerRef, 0, len(bench)),
	}
	for _, m := range xi {
		li.XI = append(li.XI, matchsim.PlayerRef{ID: m.PlayerID.String(), Position: m.Position, Weight: m.AttributeWeight})
	}
	for _, m := range bench {
		li.Bench = append(li.Bench, matchsim.PlayerRef{ID: m.PlayerID.String(), Position: m.Position, Weight: m.AttributeWeight})
	}
	if taker != nil {
		li.Taker = taker.PlayerID.String()
	}
	return li
}

// appearancesFromRatings turns a side's v1.6 rating sheet (every XI member plus
// each sub-in, with authoritative minutes and tallies) into the appearance
// rows the morale/development passes persist. Players are Started when they
// were in the starting XI; goals fold open-play + penalty goals together, the
// persisted convention.
func appearancesFromRatings(xi []squad.SquadMember, ratings []matchsim.PlayerRating) []player.Appearance {
	inXI := make(map[uuid.UUID]bool, len(xi))
	for _, m := range xi {
		inXI[m.PlayerID] = true
	}
	out := make([]player.Appearance, 0, len(ratings))
	for _, r := range ratings {
		id, err := uuid.Parse(r.PlayerID)
		if err != nil {
			continue
		}
		out = append(out, player.Appearance{
			PlayerID: id,
			Started:  inXI[id],
			Minutes:  r.Minutes,
			Rating:   &r.Rating,
			Goals:    r.Goals + r.PenaltiesScored,
			Assists:  r.Assists,
		})
	}
	return out
}

// injuryCandidates exposes the engine's attributed injury events (v1.6 already
// resolved the injured player) plus the minutes each actually played, exactly
// the inputs the injury store needs to derive severity and duration.
func injuryCandidates(res matchsim.MatchResult) []injury.Candidate {
	minutes := make(map[string]int, len(res.HomePlayerRatings)+len(res.AwayPlayerRatings))
	for _, pr := range res.HomePlayerRatings {
		minutes[pr.PlayerID] = pr.Minutes
	}
	for _, pr := range res.AwayPlayerRatings {
		minutes[pr.PlayerID] = pr.Minutes
	}
	var out []injury.Candidate
	for _, ev := range res.Events {
		if ev.Type != matchsim.EventInjury || ev.PlayerID == "" {
			continue
		}
		pid, err := uuid.Parse(ev.PlayerID)
		if err != nil {
			continue
		}
		out = append(out, injury.Candidate{PlayerID: pid, Minutes: minutes[ev.PlayerID]})
	}
	return out
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
	var format string
	var reputation int
	err := tx.QueryRow(ctx,
		`SELECT competition_type, reputation, COALESCE(r.format, '') FROM competition.competitions c
		 LEFT JOIN competition.competition_rules r ON r.competition_id = c.id
		 WHERE c.id = $1`, f.Competition.ID).
		Scan(&compType, &reputation, &format)
	if errors.Is(err, pgx.ErrNoRows) {
		compType = "custom"
	} else if err != nil {
		return fc, err
	}
	fc.IsCupTie = compType == "domestic_cup" || compType == "continental"
	// Golden goal is the knockout tie-decision contract (IM04): only fixtures
	// whose competition rules declare format='knockout' arm sudden death.
	fc.GoldenGoal = format == "knockout"
	if reputation > 0 {
		fc.LeagueTier = reputation
	}

	intensity, err := s.squad.LoadRivalryIntensity(ctx, f.HomeClub.ID, f.AwayClub.ID)
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
