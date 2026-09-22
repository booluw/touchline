// Package apiref holds the shared nested reference types API responses embed
// instead of flat foreign-key ids (league_id -> league.id, home_club_id ->
// home_club.id, ...). The wire rule across the API: a payload that joins
// another table returns that row as a nested object here; request bodies and
// URL path ids stay flat. Fields are omitempty so id-only refs (internal row
// loads that never reach the wire) marshal cleanly.
package apiref

import "github.com/google/uuid"

// ClubRef is the club identity nested in fixture, standing, and event
// responses. Short is the club's abbreviation when one exists.
type ClubRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name,omitempty"`
	Short string    `json:"short,omitempty"`
}

// PlayerRef is the player identity nested in cast event-feed responses.
type PlayerRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name,omitempty"`
}

// CountryRef is the country identity nested in competition responses. ID is
// omitempty because some join targets (ref.nationalities) are code-keyed with
// no uuid.
type CountryRef struct {
	ID   uuid.UUID `json:"id,omitempty"`
	Name string    `json:"name,omitempty"`
	Code string    `json:"code,omitempty"`
}

// CompetitionRef is the competition identity nested in season, fixture, and
// match responses.
type CompetitionRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name,omitempty"`
}

// LeagueRef is the target league of a promotion/relegation link.
type LeagueRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name,omitempty"`
}

// SeasonRef is the season identity nested in standings responses.
type SeasonRef struct {
	ID     uuid.UUID `json:"id"`
	Label  string    `json:"label"`
	Number int       `json:"number"`
	Status string    `json:"status"`
}

// MatchRef is the match identity nested in event-feed responses.
type MatchRef struct {
	ID uuid.UUID `json:"id"`
}

// FixtureRef is the fixture identity nested in match responses.
type FixtureRef struct {
	ID uuid.UUID `json:"id"`
}

// ManagerRef is the manager identity nested in payloads that join
// manager.managers.
type ManagerRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name,omitempty"`
}

// EntityRef is a polymorphic graph identity (manager | club | player |
// system) nested in social payloads. Type carries the discriminator; a system
// sender has neither an id nor a name.
type EntityRef struct {
	ID   uuid.UUID `json:"id,omitempty"`
	Name string    `json:"name,omitempty"`
	Type string    `json:"type"`
}
