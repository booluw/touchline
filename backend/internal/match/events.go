package match

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/matchsim"
)

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
		if err := rows.Scan(&e.ID, &e.Match.ID, &e.Sequence, &e.Minute, &e.Type,
			&clubID, &playerID, &relatedID, &e.Detail); err != nil {
			return nil, err
		}
		e.Club = clubRef(clubID)
		e.Player = playerRef(playerID)
		e.RelatedPlayer = playerRef(relatedID)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
