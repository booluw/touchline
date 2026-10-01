package match

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/matchsim"
	"github.com/touchline/backend/pkg/pitchsim"
)

// IM34 — the positional engine (pkg/pitchsim) rides on top of matchsim. This
// file is the whole seam: the world switch, building pitchsim's input from the
// frozen match snapshot, storing its extra events, and serving the track.

const (
	// visualEngineKey is the per-world rollout switch. "2d" turns the positional
	// engine on for matches that kick off afterwards; anything else is off. It is
	// read once at kickoff and frozen into the match snapshot.
	visualEngineKey = "match.visual_engine"
	visualEngine2D  = "2d"
	// minVisualPacing is the slowest-useful cadence: below it (lab worlds at
	// 10ms a minute) nobody can watch, so the engine stays off.
	minVisualPacing = 2 * time.Second

	sourceMatchsim = "matchsim"
	sourcePitchsim = "pitchsim"
)

// ErrNoTrack means the match was played without the positional engine (the
// world switch was off at kickoff, or the match predates IM34).
var ErrNoTrack = errors.New("match has no simulation track")

// resolveVisual reports whether the world has the positional engine on.
func (s *Service) resolveVisual(ctx context.Context, worldID uuid.UUID) bool {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT config_value FROM world.world_config
		WHERE world_id = $1 AND config_key = $2`, worldID, visualEngineKey).Scan(&raw); err != nil {
		return false
	}
	var v string
	return json.Unmarshal(raw, &v) == nil && v == visualEngine2D
}

// pitchInput builds the positional engine's input from the frozen snapshot and
// matchsim's events up to minute `minutes`.
func pitchInput(seed int64, home, away matchsim.Team, homeXI, awayXI []squad.SquadMember, evs []pitchsim.Event, minutes int) pitchsim.Input {
	side := func(t matchsim.Team, xi []squad.SquadMember) pitchsim.Side {
		s := pitchsim.Side{ClubID: t.ID}
		for _, m := range xi {
			s.XI = append(s.XI, pitchsim.Player{ID: m.PlayerID.String(), Position: m.Position})
		}
		return s
	}
	// The ball share is fixed from the kickoff ratings, never from the result,
	// so a minute already shown can never change.
	h, a := float64(home.Attack+home.Defense), float64(away.Attack+away.Defense)
	share := 0.5
	if h+a > 0 {
		share = h / (h + a)
	}
	return pitchsim.Input{Seed: seed, Home: side(home, homeXI), Away: side(away, awayXI), HomeShare: share, Events: evs, Minutes: minutes}
}

// simEvents converts matchsim's events up to minute `upTo`, and reports the
// last minute any of them falls in.
func simEvents(evs []matchsim.MatchEvent, upTo int) ([]pitchsim.Event, int) {
	out := make([]pitchsim.Event, 0, len(evs))
	last := 0
	for _, e := range evs {
		if e.Minute > upTo {
			continue
		}
		out = append(out, pitchsim.Event{Sequence: e.Sequence, Minute: e.Minute, Type: e.Type,
			ClubID: e.ClubID, PlayerID: e.PlayerID, RelatedPlayerID: e.RelatedPlayerID})
		if e.Minute > last {
			last = e.Minute
		}
	}
	return out, last
}

// trackSlice cuts minutes from..to out of a track: the minutes themselves, the
// in-minute offset of every event in them, and their extra events.
func trackSlice(tr pitchsim.Track, from, to int) ([]pitchsim.Minute, map[int]int, []pitchsim.Extra) {
	if from < 1 {
		from = 1
	}
	if to > len(tr.Minutes) {
		to = len(tr.Minutes)
	}
	if from > to {
		return nil, nil, nil
	}
	minutes := tr.Minutes[from-1 : to]
	offsets := map[int]int{}
	var extras []pitchsim.Extra
	for _, m := range minutes {
		for _, c := range m.Cues {
			offsets[c.Sequence] = c.T
		}
		extras = append(extras, m.Extras...)
	}
	return minutes, offsets, extras
}

// persistExtras stores the positional engine's extra events. Re-running a
// minute (crash recovery) inserts nothing twice; only newly written rows are
// returned.
func persistExtras(ctx context.Context, tx pgx.Tx, matchID uuid.UUID, extras []pitchsim.Extra) ([]*MatchEventRow, error) {
	out := make([]*MatchEventRow, 0, len(extras))
	for _, x := range extras {
		var clubID, playerID *uuid.UUID
		if id, err := uuid.Parse(x.ClubID); err == nil {
			clubID = &id
		}
		if id, err := uuid.Parse(x.PlayerID); err == nil {
			playerID = &id
		}
		detail, err := json.Marshal(map[string]string{"commentary": x.Text})
		if err != nil {
			return nil, err
		}
		rowID, offset := uuid.New(), x.Offset
		tag, err := tx.Exec(ctx, `
			INSERT INTO match.match_events
				(id, match_id, sequence, minute, event_type, club_id, player_id, detail, source, offset_millis)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (match_id, sequence) DO NOTHING`,
			rowID, matchID, x.Sequence, x.Minute, x.Type, clubID, playerID, detail, sourcePitchsim, offset)
		if err != nil {
			return nil, fmt.Errorf("insert pitchsim event: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		out = append(out, &MatchEventRow{
			ID: rowID, Match: apiref.MatchRef{ID: matchID}, Sequence: x.Sequence, Minute: x.Minute, Type: x.Type,
			Club: clubRef(clubID), Player: playerRef(playerID), Detail: detail, Source: sourcePitchsim, Offset: &offset,
		})
	}
	return out, nil
}

// sortFeed orders rows the way the feed reads: by minute, then position in the
// minute, then sequence.
func sortFeed(rows []*MatchEventRow) {
	off := func(e *MatchEventRow) int {
		if e.Offset == nil {
			return 0
		}
		return *e.Offset
	}
	sort.SliceStable(rows, func(a, b int) bool {
		x, y := rows[a], rows[b]
		if x.Minute != y.Minute {
			return x.Minute < y.Minute
		}
		if off(x) != off(y) {
			return off(x) < off(y)
		}
		return x.Sequence < y.Sequence
	})
}

// feedQuery is the one read of the event feed, in feed order.
const feedQuery = `
	SELECT id, match_id, sequence, minute, event_type, club_id, player_id, related_player_id, detail,
	       source, offset_millis
	FROM match.match_events WHERE match_id = $1
	ORDER BY minute, COALESCE(offset_millis, 0), sequence`

func scanFeed(rows pgx.Rows) ([]*MatchEventRow, error) {
	defer rows.Close()
	var out []*MatchEventRow
	for rows.Next() {
		e := &MatchEventRow{}
		var clubID, playerID, relatedID *uuid.UUID
		if err := rows.Scan(&e.ID, &e.Match.ID, &e.Sequence, &e.Minute, &e.Type,
			&clubID, &playerID, &relatedID, &e.Detail, &e.Source, &e.Offset); err != nil {
			return nil, err
		}
		e.Club = clubRef(clubID)
		e.Player = playerRef(playerID)
		e.RelatedPlayer = playerRef(relatedID)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TrackView is the GET /api/matches/:id/track response: the movement for a
// range of minutes plus what a renderer needs to label and pace it.
type TrackView struct {
	Version      int               `json:"version"`
	Status       string            `json:"status"`
	PacingMillis int               `json:"pacing_millis"` // real milliseconds one match minute took live; 0 for a quick-played match
	LastMinute   int               `json:"last_minute"`   // last minute that exists so far
	Players      map[string]string `json:"players"`       // player id → display name, for every lineup in the range
	Minutes      []pitchsim.Minute `json:"minutes"`
}

// GetTrack regenerates a match's movement for minutes from..to (0 = open
// ended) from what is already persisted: the seed, the kickoff snapshot and
// matchsim's events. Nothing positional is stored. pgx.ErrNoRows when the match
// does not exist; ErrNoTrack when it was played without the positional engine.
func (s *Service) GetTrack(ctx context.Context, matchID uuid.UUID, from, to int) (*TrackView, error) {
	var (
		seed           int64
		raw            []byte
		status         string
		minute, pacing int
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT seed, sim_inputs, status, COALESCE(current_minute, 0), COALESCE(pacing_millis, 0)
		FROM match.matches WHERE id = $1`, matchID).Scan(&seed, &raw, &status, &minute, &pacing); err != nil {
		return nil, err
	}
	snap := &simInputs{}
	if len(raw) == 0 || json.Unmarshal(raw, snap) != nil || !snap.Visual {
		return nil, ErrNoTrack
	}

	rows, err := s.pool.Query(ctx, `
		SELECT sequence, minute, event_type, COALESCE(club_id::text, ''), COALESCE(player_id::text, ''),
		       COALESCE(related_player_id::text, '')
		FROM match.match_events WHERE match_id = $1 AND source = $2 ORDER BY sequence`, matchID, sourceMatchsim)
	if err != nil {
		return nil, fmt.Errorf("track: events: %w", err)
	}
	evs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pitchsim.Event, error) {
		var e pitchsim.Event
		err := r.Scan(&e.Sequence, &e.Minute, &e.Type, &e.ClubID, &e.PlayerID, &e.RelatedPlayerID)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("track: events: %w", err)
	}
	last := minute
	for _, e := range evs {
		if e.Minute > last {
			last = e.Minute
		}
	}

	tr := pitchsim.Generate(pitchInput(seed, snap.HomeTeam, snap.AwayTeam, snap.HomeXI, snap.AwayXI, evs, last))
	if to <= 0 || to > last {
		to = last
	}
	minutes, _, _ := trackSlice(tr, from, to)
	view := &TrackView{Version: tr.Version, Status: status, PacingMillis: pacing, LastMinute: last,
		Players: map[string]string{}, Minutes: minutes}
	if view.Minutes == nil {
		view.Minutes = []pitchsim.Minute{}
	}

	ids := map[uuid.UUID]struct{}{}
	for _, m := range minutes {
		for _, id := range m.Lineup {
			if u, err := uuid.Parse(id); err == nil {
				ids[u] = struct{}{}
			}
		}
	}
	if len(ids) > 0 {
		names, err := s.pool.Query(ctx, `
			SELECT p.id::text, pe.display_name
			FROM player.players p JOIN person.people pe ON pe.id = p.person_id
			WHERE p.id = ANY($1::uuid[])`, uuidSet(ids))
		if err != nil {
			return nil, fmt.Errorf("track: names: %w", err)
		}
		defer names.Close()
		for names.Next() {
			var id, name string
			if err := names.Scan(&id, &name); err != nil {
				return nil, fmt.Errorf("track: names: %w", err)
			}
			view.Players[id] = name
		}
		if err := names.Err(); err != nil {
			return nil, fmt.Errorf("track: names: %w", err)
		}
	}
	return view, nil
}
