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
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// Fixture mirrors match.fixtures for the read/feed path (nested refs).
type Fixture struct {
	ID          uuid.UUID             `json:"id"`
	WorldID     uuid.UUID             `json:"world_id"`
	Competition apiref.CompetitionRef `json:"competition"`
	HomeClub    apiref.ClubRef        `json:"home_club"`
	AwayClub    apiref.ClubRef        `json:"away_club"`
	Matchday    int                   `json:"matchday,omitempty"`
	ScheduledAt time.Time             `json:"scheduled_at"`
	Status      string                `json:"status"`
}

// MatchView is the match-screen header: the live clock, status, and the
// server-computed scoreline (never derived by the client).
type MatchView struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	Minute    int       `json:"minute"`
	HomeScore int       `json:"home_score"`
	AwayScore int       `json:"away_score"`
}

// FixtureMatch is the aggregated GET /api/fixtures/:id response: the fixture
// header plus its match (nil until the fixture has kicked off).
type FixtureMatch struct {
	Fixture *Fixture   `json:"fixture"`
	Match   *MatchView `json:"match,omitempty"`
}

// Match mirrors match.matches. Seed is the deterministic replay input.
type Match struct {
	ID            uuid.UUID  `json:"id"`
	FixtureID     uuid.UUID  `json:"fixture_id"`
	WorldID       uuid.UUID  `json:"world_id"`
	Seed          int64      `json:"seed"`
	EngineVersion string     `json:"engine_version"`
	HomeGoals     int        `json:"home_goals"`
	AwayGoals     int        `json:"away_goals"`
	Status        string     `json:"status"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

// MatchEventRow mirrors match.match_events for the feed/read path. Club and
// player refs are nil when the engine event was a bare marker (kickoff/half/
// full time) or carried no castable player/related player.
type MatchEventRow struct {
	ID            uuid.UUID         `json:"id"`
	Match         apiref.MatchRef   `json:"match"`
	Sequence      int               `json:"sequence"`
	Minute        int               `json:"minute"`
	Type          string            `json:"type"`
	Club          *apiref.ClubRef   `json:"club,omitempty"`
	Player        *apiref.PlayerRef `json:"player,omitempty"`
	RelatedPlayer *apiref.PlayerRef `json:"related_player,omitempty"`
	Detail        json.RawMessage   `json:"detail,omitempty"` // JSONB commentary
}

// MatchResult is the full outcome of one fixture.
type MatchResult struct {
	Match  *Match           `json:"match"`
	Events []*MatchEventRow `json:"events"`
}

// MatchTickPayload is the match_tick realtime envelope payload (S04-03). It
// mirrors the persisted match_events rows plus the server-computed scoreline
// and clock, so the client renders the feed verbatim and never calculates
// outcomes. Events may be empty for silent minutes (the tick still advances
// the clock and scoreline).
type MatchTickPayload struct {
	Match     apiref.MatchRef   `json:"match"`
	Fixture   apiref.FixtureRef `json:"fixture"`
	Minute    int               `json:"minute"`
	Status    string            `json:"status"`
	HomeClub  apiref.ClubRef    `json:"home_club"`
	AwayClub  apiref.ClubRef    `json:"away_club"`
	HomeScore int               `json:"home_score"`
	AwayScore int               `json:"away_score"`
	Events    []*MatchEventRow  `json:"events,omitempty"`
}
