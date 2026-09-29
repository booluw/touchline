package match

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/matchsim"
)

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
	// The awaited-minute check is atomic with the insert: the pacing goroutine
	// advances the clock under FOR UPDATE while this validation is read-only, so
	// the pre-check alone could admit an input whose minute already streamed
	// (a late style/sub landing after the client saw that minute). Re-checking
	// current_minute in the INSERT's WHERE closes that window: an input for an
	// already-persisted minute accepts zero rows and the manager gets
	// ErrMinuteClosed instead of a change that silently reruns at Finalize.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO match.match_inputs (match_id, world_id, sequence, minute, kind, payload, created_by_manager_id)
		SELECT $1, $2, $3, $4, 'substitution', $5, $6
		WHERE EXISTS (
			SELECT 1 FROM match.matches
			WHERE id = $1 AND status = 'in_progress' AND current_minute < $4
		)`, matchID, live.worldID, seq, minute, payload, managerID)
	if err != nil {
		return fmt.Errorf("substitute: insert input: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrMinuteClosed
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
	// Same atomic awaited-minute contract as Substitute: the insert is guarded
	// by the match row's live current_minute so an input can never land at a
	// minute the pacing goroutine already streamed (see the comment above).
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO match.match_inputs (match_id, world_id, sequence, minute, kind, payload, created_by_manager_id)
		SELECT $1, $2, $3, $4, 'tactic_change', $5, $6
		WHERE EXISTS (
			SELECT 1 FROM match.matches
			WHERE id = $1 AND status = 'in_progress' AND current_minute < $4
		)`, matchID, live.worldID, seq, minute, payload, managerID)
	if err != nil {
		return fmt.Errorf("tactic change: insert input: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrMinuteClosed
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

// containsInt reports whether xs contains v.
func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// lineupsSessionTeams stamps a session's frozen snapshot lineups onto its
// engine teams so the v1.6 attribution pass links events for both sides and
// Finalize can read the per-player rating sheets.
func lineupsSessionTeams(sess *LiveSession) (matchsim.Team, matchsim.Team) {
	home, away := sess.Home, sess.Away
	home.Lineups = playerLineupsFor(sess.homeXI, sess.homeBench, sess.homeTaker)
	away.Lineups = playerLineupsFor(sess.awayXI, sess.awayBench, sess.awayTaker)
	return home, away
}
