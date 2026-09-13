// Package match owns the deterministic matchday orchestration (matchsim
// addendum v1.4 Part 8): it reads the real persisted squad/DNA/form data,
// aggregates it into the pure engine's two sides, runs the seeded simulation,
// and persists the full outcome — match.matches, the match.match_events feed,
// club form, and the world.events log (lineup warnings + match played).
// pkg/matchsim stays pure; every database read/write and bus emission lives
// here. Standings application is deliberately NOT this package's job: the
// competition service consumes PlayFixture's result (Phase 6 wire-up).
package match

import (
	"time"

	"github.com/google/uuid"
)

// Fixture mirrors match.fixtures for the read/feed path.
type Fixture struct {
	ID            uuid.UUID `json:"id"`
	WorldID       uuid.UUID `json:"world_id"`
	CompetitionID uuid.UUID `json:"competition_id"`
	HomeClubID    uuid.UUID `json:"home_club_id"`
	AwayClubID    uuid.UUID `json:"away_club_id"`
	Matchday      int       `json:"matchday,omitempty"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	Status        string    `json:"status"`
}

// Match mirrors match.matches. Seed is the deterministic replay input.
type Match struct {
	ID            uuid.UUID `json:"id"`
	FixtureID     uuid.UUID `json:"fixture_id"`
	WorldID       uuid.UUID `json:"world_id"`
	Seed          int64     `json:"seed"`
	EngineVersion string    `json:"engine_version"`
	HomeGoals     int       `json:"home_goals"`
	AwayGoals     int       `json:"away_goals"`
	Status        string    `json:"status"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

// MatchEventRow mirrors match.match_events for the feed/read path. Player
// pointers are nil when the engine event was a bare marker (kickoff/half/full
// time) or carried no castable player.
type MatchEventRow struct {
	ID              uuid.UUID  `json:"id"`
	MatchID         uuid.UUID  `json:"match_id"`
	Sequence        int        `json:"sequence"`
	Minute          int        `json:"minute"`
	Type            string     `json:"type"`
	ClubID          *uuid.UUID `json:"club_id,omitempty"`
	PlayerID        *uuid.UUID `json:"player_id,omitempty"`
	RelatedPlayerID *uuid.UUID `json:"related_player_id,omitempty"`
	Detail          []byte     `json:"detail,omitempty"` // JSONB commentary
}

// MatchResult is the full outcome of one fixture.
type MatchResult struct {
	Match  *Match          `json:"match"`
	Events []*MatchEventRow `json:"events"`
}