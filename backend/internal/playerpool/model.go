// Package playerpool implements the free-agent player lifecycle pool (A03):
// country-scoped free-agent supply that clubs draft from, a deterministic
// draft that respects the squad template, and pool replenishment so the market
// never empties. It has no dependency on bootstrap/competition — those packages
// call into this one (competition → bootstrap → playerpool), so all player
// persistence helpers live here (moved out of bootstrap when that package
// stopped generating players directly).
package playerpool

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/finance"
)

// PoolTargetSize is the steady-state free-agent pool size maintained per
// country (and the world-level pool at bootstrap): roughly 100 players.
const PoolTargetSize = 100

// Event kinds emitted by the player pool.
const (
	// EventClaimedFromPool fires when a club drafts a free agent into its
	// squad during bootstrap / league seeding (system actor).
	EventClaimedFromPool = "PLAYER_CLAIMED_FROM_POOL"
	// EventPlayerSigned fires when a free agent is signed by a club,
	// typically via the human-facing sign endpoint (A08).
	EventPlayerSigned = "PLAYER_SIGNED"
)

// Sentinel errors.
var (
	// ErrPoolTooSmall means the pool held fewer players than the draft
	// requested. Callers seed/replenish before drafting.
	ErrPoolTooSmall = errors.New("free-agent pool has too few players to draft a squad")
)

// DraftedPlayer is one player moved from the pool into a club during a draft.
type DraftedPlayer struct {
	PlayerID        uuid.UUID
	PersonID        uuid.UUID
	FirstName       string
	LastName        string
	DisplayName     string
	NationalityCode string
	DateOfBirth     time.Time
	Age             int
	PrimaryPosition string
	SquadNumber     int
	OverallRating   int
}

// DraftResult describes one completed squad draft.
type DraftResult struct {
	ClubID        uuid.UUID
	SquadSize     int
	PoolRemaining int
	Players       []DraftedPlayer
	// Seeds feed finance.BootstrapClub: the caller signs every drafted player
	// to a starter contract + wage commitment in the same transaction.
	Seeds []finance.ContractSeed
}

// FreeAgent is one free agent for the ListFreeAgents read model.
type FreeAgent struct {
	ID              uuid.UUID `json:"id"`
	PersonID        uuid.UUID `json:"person_id"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	DisplayName     string    `json:"display_name"`
	NationalityCode string    `json:"nationality_code"`
	DateOfBirth     string    `json:"date_of_birth"`
	Age             int       `json:"age"`
	PrimaryPosition string    `json:"primary_position"`
	Origin          string    `json:"origin"`
	OverallRating   int       `json:"overall_rating"`
	MarketValue     int64     `json:"market_value"`
}

// FreeAgentFilter narrows ListFreeAgents. A nil pointer field is unfiltered.
type FreeAgentFilter struct {
	CountryID   *uuid.UUID `json:"country_id"`
	Position    *string    `json:"position"`
	AgeMin      *int       `json:"age_min"`
	AgeMax      *int       `json:"age_max"`
	Nationality *string    `json:"nationality"`
}
