package match

import "github.com/google/uuid"

type Fixture struct {
	ID            uuid.UUID `json:"id"`
	WorldID       uuid.UUID `json:"world_id"`
	CompetitionID uuid.UUID `json:"competition_id"`
	HomeClubID    uuid.UUID `json:"home_club_id"`
	AwayClubID    uuid.UUID `json:"away_club_id"`
	ScheduledAt   string    `json:"scheduled_at"`
	Status        string    `json:"status"` // scheduled, live, completed, postponed
}

type Match struct {
	ID         uuid.UUID `json:"id"`
	FixtureID  uuid.UUID `json:"fixture_id"`
	HomeGoals  int       `json:"home_goals"`
	AwayGoals  int       `json:"away_goals"`
	Seed       int64     `json:"seed"` // for deterministic replay
	Status     string    `json:"status"`
}

type MatchEvent struct {
	ID          uuid.UUID `json:"id"`
	MatchID     uuid.UUID `json:"match_id"`
	Minute      int       `json:"minute"`
	Type        string    `json:"type"` // goal, card, injury, substitution, chance, half_time, full_time
	Description string    `json:"description"`
	TeamID      uuid.UUID `json:"team_id"`
	PlayerID    *uuid.UUID `json:"player_id,omitempty"`
}

type MatchResult struct {
	Match    *Match       `json:"match"`
	Events   []*MatchEvent `json:"events"`
	Seed     int64         `json:"seed"`
}

type Service interface {
	SimulateMatch(seed int64, homeTeamID, awayTeamID uuid.UUID) (*MatchResult, error)
	GetFixture(id uuid.UUID) (*Fixture, error)
	GetMatchEvents(matchID uuid.UUID) ([]*MatchEvent, error)
}
