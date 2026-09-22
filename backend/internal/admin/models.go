// Package admin implements read-only per-country admin dashboard reads. The
// country pane is anchored on world.countries (the admin-declared geography);
// the three data lenses it aggregates are kept explicit (see plan):
//
//   - players belong to a country via player.players.country_id (their origin
//     pool; hard FK, set at generation, never updated on sign/release);
//   - leagues belong to a country via competition.competitions.country_id
//     (hard FK);
//   - clubs belong to a country by name match club.clubs.country =
//     world.countries.name (best-effort join; orphan clubs surface as
//     "unassigned" so nothing silently disappears from the numbers).
//
// Everything here is a read — admin monitors and drills down, never mutates.
package admin

import (
	"time"

	"github.com/google/uuid"
	"github.com/touchline/backend/pkg/apiref"
)

// CountryRef is the resolved world-scoped country a dashboard pane covers.
type CountryRef struct {
	ID      uuid.UUID `json:"id"`
	WorldID uuid.UUID `json:"world_id"`
	Code    string    `json:"code"`
	Name    string    `json:"name"`
}

// PopulationSummary counts players whose origin pool is this country.
type PopulationSummary struct {
	Total     int            `json:"total"`
	Active    int            `json:"active"`
	FreeAgent int            `json:"free_agents"`
	Retired   int            `json:"retired"`
	ByStatus  map[string]int `json:"by_status"`
}

// UnassignedSummary surfaces the two "nowhere to count" buckets: players in
// the world-level bootstrap pool (no country) and clubs whose country name
// matches no world.countries row. Admins need these visible, not hidden.
type UnassignedSummary struct {
	WorldPoolPlayers int `json:"world_pool_players"`
	OrphanClubs      int `json:"orphan_clubs"`
}

// ClubSummary counts clubs in the country by geography plus the crisis subset.
type ClubSummary struct {
	Total      int `json:"total"`
	InCrisis   int `json:"in_crisis"`
	Orphans    int `json:"orphan_clubs"`
	Unassigned int `json:"unassigned_clubs"`
}

// LeagueSummary counts the country's leagues, optionally split by tier.
type LeagueSummary struct {
	Total  int         `json:"total"`
	ByTier map[int]int `json:"by_tier"`
}

// MarketSummary snapshots the country's transfer market in counters.
type MarketSummary struct {
	OpenListings      int `json:"open_listings"`
	BidsReceived      int `json:"bids_received"`
	BidsMade          int `json:"bids_made"`
	TransfersIn       int `json:"transfers_in"`
	TransfersOut      int `json:"transfers_out"`
	FreeAgentSignings int `json:"free_agent_signings"`
}

// EconomySummary aggregates the country's clubs' finances.
type EconomySummary struct {
	CrisisClubs       int   `json:"crisis_clubs"`
	WageBill          int64 `json:"wage_bill"`
	WageAllocated     int64 `json:"wage_allocated"`
	WageCommitted     int64 `json:"wage_committed"`
	TransferAllocated int64 `json:"transfer_allocated"`
	TransferCommitted int64 `json:"transfer_committed"`
	Cash              int64 `json:"cash"`
}

// Headline is one hand-curated "what happened here" line for the overview.
type Headline struct {
	Kind       string    `json:"kind"` // transfer_in | transfer_out | signing | crisis | intake
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Overview is the country's home pane: compact counters on every axis,
// plus the top headlines. Factoring happens in the drill-down endpoints.
type Overview struct {
	Country    *CountryRef       `json:"country"`
	AsOf       time.Time         `json:"as_of"`
	Population PopulationSummary `json:"population"`
	Clubs      ClubSummary       `json:"clubs"`
	Leagues    LeagueSummary     `json:"leagues"`
	Market     MarketSummary     `json:"market"`
	Economy    EconomySummary    `json:"economy"`
	Unassigned UnassignedSummary `json:"unassigned"`
	Headlines  []Headline        `json:"headlines"`
}

// SeasonRef is the latest season of a competition, when one exists.
type SeasonRef struct {
	SeasonID     uuid.UUID `json:"season_id"`
	SeasonLabel  string    `json:"season_label"`
	SeasonNumber int       `json:"season_number"`
	Status       string    `json:"status"`
}

// LeagueRow is one league of the country's pyramid.
type LeagueRow struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Tier        int        `json:"tier"`
	TeamCount   int        `json:"team_count"`
	Status      string     `json:"status"`
	Promotions  int        `json:"promotions"`
	Relegations int        `json:"relegations"`
	ClubCount   int        `json:"club_count"`
	Season      *SeasonRef `json:"season,omitempty"`
}

// Pyramid is the country's league pyramid grouped by tier.
type Pyramid struct {
	Country *CountryRef `json:"country"`
	Leagues []LeagueRow `json:"leagues"`
}

// ClubRow is one club row of the country panel: identity, league, squad and
// finance sanity numbers in one line. The league id+name nest into a ref;
// league tier stays flat.
type ClubRow struct {
	ID                uuid.UUID         `json:"id"`
	Name              string            `json:"name"`
	ShortName         string            `json:"short_name"`
	IsAIControlled    bool              `json:"is_ai_controlled"`
	Tier              int               `json:"tier"`
	LeagueID          *uuid.UUID        `json:"-"`
	League            *apiref.LeagueRef `json:"league,omitempty"`
	LeagueName        string            `json:"-"`
	LeagueTier        int               `json:"league_tier"`
	SquadSize         int               `json:"squad_size"`
	TopPlayerName     string            `json:"top_player_name,omitempty"`
	TopPlayerMarket   int64             `json:"top_player_market_value"`
	WageBill          int64             `json:"wage_bill"`
	WageAllocated     int64             `json:"wage_allocated"`
	WageCommitted     int64             `json:"wage_committed"`
	TransferAllocated int64             `json:"transfer_allocated"`
	TransferCommitted int64             `json:"transfer_committed"`
	CrisisStage       *string           `json:"crisis_stage,omitempty"`
	Cash              int64             `json:"cash"`
}

// ClubsPanels is the country's club list.
type ClubsPanels struct {
	Country *CountryRef `json:"country"`
	Clubs   []ClubRow   `json:"clubs"`
}

// PositionBreakdown is one position's population line: count plus the
// position-weighted overall and average potential (per the squad rating
// recipe).
type PositionBreakdown struct {
	Position     string `json:"position"`
	Count        int    `json:"count"`
	AvgOverall   int    `json:"avg_overall"`
	AvgPotential int    `json:"avg_potential"`
}

// NationalityMix is one nationality (code + display name) within the country.
type NationalityMix struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// IntakeRow is one season's country youth intake, oldest first.
type IntakeRow struct {
	SeasonNumber int `json:"season_number"`
	PlayerCount  int `json:"player_count"`
}

// PlayerSummary is the country's population drill-down aggregate.
type PlayerSummary struct {
	Country       *CountryRef         `json:"country"`
	Total         int                 `json:"total"`
	ByStatus      map[string]int      `json:"by_status"`
	ByOrigin      map[string]int      `json:"by_origin"`
	ByPosition    []PositionBreakdown `json:"by_position"`
	Nationalities []NationalityMix    `json:"nationalities"`
	Intakes       []IntakeRow         `json:"intakes"`
	AvgAge        float64             `json:"avg_age"`
}

// ListingRow is one open listing for a country club.
type ListingRow struct {
	ListingID   uuid.UUID         `json:"listing_id"`
	Player      *apiref.PlayerRef `json:"player"`
	Position    string            `json:"position"`
	ListingClub *apiref.ClubRef   `json:"listing_club"`
	AskingPrice *int64            `json:"asking_price,omitempty"`
	MarketValue int64             `json:"market_value"`
	ListingType string            `json:"listing_type"`
	ListedAt    time.Time         `json:"listed_at"`
}

// BidRow is one bid attributed to a country — either received (selling club
// in the country) or made (bidding club in the country).
type BidRow struct {
	BidID       uuid.UUID         `json:"bid_id"`
	Player      *apiref.PlayerRef `json:"player"`
	BiddingClub *apiref.ClubRef   `json:"bidding_club"`
	SellingClub *apiref.ClubRef   `json:"selling_club"`
	Fee         int64             `json:"fee"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
}

// TransferRow is one completed transfer involving a country club, annotated
// with the player's current market value for the fee-vs-value read.
type TransferRow struct {
	TransferID  uuid.UUID         `json:"transfer_id"`
	Player      *apiref.PlayerRef `json:"player"`
	FromClub    *apiref.ClubRef   `json:"from_club,omitempty"`
	ToClub      *apiref.ClubRef   `json:"to_club"`
	Fee         int64             `json:"fee"`
	MarketValue int64             `json:"market_value"`
	Overpay     bool              `json:"overpay"`
	OverpayPct  *int              `json:"overpay_pct,omitempty"`
	CompletedAt time.Time         `json:"completed_at"`
}

// MarketPanels is the country transfer market: open listings, both bid
// directions, and the transfer ledger split into paid in/out and signings.
type MarketPanels struct {
	Country      *CountryRef   `json:"country"`
	WindowDays   int           `json:"window_days"`
	OpenListings []ListingRow  `json:"open_listings"`
	BidsReceived []BidRow      `json:"bids_received"`
	BidsMade     []BidRow      `json:"bids_made"`
	TransfersIn  []TransferRow `json:"transfers_in"`
	TransfersOut []TransferRow `json:"transfers_out"`
	Signings     []TransferRow `json:"signings"`
}

// CrisisClubRow is one club in an active financial crisis.
type CrisisClubRow struct {
	ClubID    uuid.UUID `json:"club_id"`
	ClubName  string    `json:"club_name"`
	Stage     string    `json:"stage"`
	StartedAt time.Time `json:"started_at"`
}

// WageRow top wage bills among the country's clubs.
type WageRow struct {
	ClubID     uuid.UUID `json:"club_id"`
	ClubName   string    `json:"club_name"`
	WeeklyBill int64     `json:"weekly_wage_bill"`
	Count      int       `json:"active_commitments"`
}

// BudgetSide is one budget axis (wage / transfer) summed across the country.
type BudgetSide struct {
	Allocated   int64 `json:"allocated"`
	Committed   int64 `json:"committed"`
	Available   int64 `json:"available"`
	UtilizedPct int   `json:"utilized_pct"`
}

// FinancePanels is the country economy: crisis, wage bill, budget capacity.
type FinancePanels struct {
	Country        *CountryRef     `json:"country"`
	CrisisClubs    []CrisisClubRow `json:"crisis_clubs"`
	WageBill       int64           `json:"wage_bill"`
	TopWageBills   []WageRow       `json:"top_wage_bills"`
	WageBudget     BudgetSide      `json:"wage_budget"`
	TransferBudget BudgetSide      `json:"transfer_budget"`
	Cash           int64           `json:"cash"`
}

// TimelineItem is one world event resolved to the country.
type TimelineItem struct {
	ID         uuid.UUID      `json:"id"`
	EventType  string         `json:"event_type"`
	OccurredAt time.Time      `json:"occurred_at"`
	ActorType  *string        `json:"actor_type,omitempty"`
	Title      string         `json:"title"`
	Payload    map[string]any `json:"payload"`
}

// Timeline is the filtered recent event stream for the country.
type Timeline struct {
	Country *CountryRef    `json:"country"`
	Since   time.Time      `json:"since"`
	Items   []TimelineItem `json:"items"`
}
