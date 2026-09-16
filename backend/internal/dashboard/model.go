// Package dashboard owns the S07-01 home dashboard aggregator: read-only
// queries that turn the plane's already-persisted state into the urgent /
// important / interesting feed a manager sees on the home screen, plus the
// realtime `dashboard_update` push that keeps that feed fresh between walking
// the page open.
//
// The service never mutates game state — it is a pure aggregation layer over
// tables owned by transfer, finance, board, player, competition, social and
// match. Postgres stays authoritative; the realtime push is best-effort.
//
// All numbers live in this file (source of truth: docs/design/
// dashboard-numerics.md); recalibration is a data-only change.
package dashboard

import (
	"time"

	"github.com/google/uuid"
)

// Priority ranks a dashboard item within its section. The client renders the
// three sections in this order.
type Priority string

const (
	// PriorityUrgent demands attention now (respond, confirm, act before a
	// deadline lapses).
	PriorityUrgent Priority = "urgent"
	// PriorityImportant needs a decision soon but has no hard deadline.
	PriorityImportant Priority = "important"
	// PriorityInteresting is ambient world context the manager can skim.
	PriorityInteresting Priority = "interesting"
)

// Well-known item categories. The client groups / dedupes on Category; the
// string is part of the wire format and the item ID prefix.
const (
	CatBids      = "bids"      // incoming transfer bids awaiting a response
	CatContracts = "contracts" // expiring contracts
	CatMatch     = "match"     // imminent fixtures
	CatBoard     = "board"     // job-security confidence warnings
	CatFinance   = "finance"   // financial crisis / bank balance warnings
	CatMorale    = "morale"    // unhappy players / transfer requests
	CatStandings = "standings" // league position moves
	CatRivals    = "rivals"    // rival results / H2H context
	CatMarket    = "market"    // recent listing / completed transfer news
)

// Action kinds the client can route on. Each action carries the IDs needed to
// deep-link (a lineup screen, a transfer response modal, a player page ...).
const (
	ActionRespondBid    = "respond_bid"    // BidID + ClubID
	ActionRenewContract = "renew_contract" // PlayerID + ClubID
	ActionSetLineup     = "set_lineup"     // FixtureID + ClubID
	ActionViewBoard     = "view_board"     // ClubID
	ActionViewFinances  = "view_finances"  // ClubID
	ActionViewPlayer    = "view_player"    // PlayerID
	ActionViewStandings = "view_standings" // ClubID
	ActionViewFixture   = "view_fixture"   // FixtureID
)

// Expansion windows that decide which items surface (source of truth:
// docs/design/dashboard-numerics.md). These mirror the proposal defaults agreed
// at planning: a contract inside ContractExpiryWindowDays, a fixture inside
// FixtureUrgencyWindow.
const (
	// ContractExpiryWindowDays is how far out an expiring contract surfaces as
	// urgent.
	ContractExpiryWindowDays = 30
	// FixtureUrgencyWindow is how far out the next fixture surfaces as urgent.
	FixtureUrgencyWindow = 48 * time.Hour
	// BoardCriticalTotal is the job-security confidence line that flags a club
	// as at-risk (mirrors board.SackThresholdTotal).
	BoardCriticalTotal = 25
	// BoardDropAttention is the week-over-week confidence drop worth surfacing.
	BoardDropAttention = -15
	// MoraleUnhappyThreshold is the morale line that flags a player as unhappy
	// (mirrors player.UnhappyMoraleThreshold).
	MoraleUnhappyThreshold = 0.35
	// MarketNewest is the number of recent listing/transfer events surfaced.
	MarketNewest = 5
	// MaxPerSection caps each section so a busy late-season feed stays skimmable.
	MaxPerSection = 12
)

// Action carries the deep-link the client follows when the user taps an item.
// Kind selects the screen; the pointed IDs scope it. All fields except Kind are
// optional and only populated when relevant.
type Action struct {
	Kind      string     `json:"kind"`
	ClubID    *uuid.UUID `json:"club_id,omitempty"`
	PlayerID  *uuid.UUID `json:"player_id,omitempty"`
	FixtureID *uuid.UUID `json:"fixture_id,omitempty"`
	BidID     *uuid.UUID `json:"bid_id,omitempty"`
}

// Item is one rendered feed entry. ID is a stable dedupe key the client uses
// to prepend-or-merge realtime pushes without refetching (e.g.
// "bids:<bid_id>", "contracts:<player_id>").
type Item struct {
	ID          string    `json:"id"`
	Priority    Priority  `json:"priority"`
	Category    string    `json:"category"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Action      *Action   `json:"action,omitempty"`
}

// Snapshot is the GET /api/dashboard body: the three ranked sections.
type Snapshot struct {
	Urgent      []Item `json:"urgent"`
	Important   []Item `json:"important"`
	Interesting []Item `json:"interesting"`
}

// DashboardUpdatePayload is the server-pushed realtime envelope
// (type "dashboard_update"). Category is one of the priority values; the
// client prepends/merges items into that section by their stable IDs.
type DashboardUpdatePayload struct {
	Category Priority `json:"category"`
	Items    []Item   `json:"items"`
}

func ptr[V any](v V) *V { return &v }
