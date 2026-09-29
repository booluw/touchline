package competition

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// ---------------------------------------------------------------------------
// Domestic cup (IM04)
//
// The system-seeded knockout cup consumes the schema hooks migrations 0012/
// 0037 reserved: competition_type 'domestic_cup', format 'knockout', role
// 'cup' memberships, and the registered/qualified/eliminated/champion entry
// statuses. It stages a country's league clubs behind a configurable late
// entry (top N of tier 1 join when X bottom-tier survivors remain), draws the
// bracket deterministically from world_seed ⊕ cup_id ⊕ round, and resolves
// level ties by golden goal (overflow from matchsim, wired via format by
// internal/match). Knockout results never touch competition.standings and
// never trigger the country promotion/relegation cascade.
// ---------------------------------------------------------------------------

const (
	// cupEntryKind is the qualification source written to
	// competition_rules.qualification_rules at creation (introspection).
	cupEntryKind = "country_league_members"
	// cupMaxMemberships is the per-club role='cup' cap (migration 0037).
	cupMaxMemberships = 3
	// cup scheduling fallback when the admin declares none: one cup round per
	// weekly game-week, on the default kickoff-hour rotation.
	cupMatchdaysPerWeek = 1
	cupDaysPerWeek      = 7
	// Cup final-date policy (IM10): 'calculated' derives the final each season
	// a short offset after the scope's latest league fixture; 'fixed' pins an
	// absolute date that never recalculates. Wire/JSON values are date-only
	// "2006-01-02" strings.
	cupFinalModeCalculated    = "calculated"
	cupFinalModeFixed         = "fixed"
	cupDefaultFinalOffsetDays = 3
)

// CupParams is the admin declaration for a new knockout cup. A country-scoped
// cup uses CountryID with the IM04 staged-eligibility variables N and X; a
// region-scoped cup sets RegionID (plus optional soft Tier and the per-league
// Qualification bands) and creates a 'continental' competition.
type CupParams struct {
	WorldID           uuid.UUID       `json:"world_id"`
	CountryID         uuid.UUID       `json:"country_id"`
	RegionID          *uuid.UUID      `json:"region_id,omitempty"`
	Tier              *int            `json:"tier,omitempty"`
	Name              string          `json:"name"`
	FirstTierBye      int             `json:"first_tier_bye"`     // N: top-N tier-1 clubs join late
	SurvivorThreshold int             `json:"survivor_threshold"` // X: survivors remain when they do
	PrizePool         float64         `json:"prize_pool,omitempty"`
	SchedulingRules   json.RawMessage `json:"scheduling_rules,omitempty"`
	Qualification     []QualBandInput `json:"qualification,omitempty"`
	FinalDatePolicy
}

// FinalDatePolicy is the cup final-date declaration (IM10), shared by both cup
// scopes. 'calculated' re-derives the final each season from the scope's
// latest league fixture; 'fixed' pins FinalDate. FinalDate is a date-only
// "2006-01-02" string on the wire. FinalOffsetDays is a pointer so an explicit
// 0 (final on the first allowed weekday at/after the last league fixture) is
// distinguishable from "unset" (defaults to 3, which reproduces the IM05
// anchored final exactly). Fixed cups ignore offsets; the calculated mode
// ignores a supplied date.
type FinalDatePolicy struct {
	FinalDateMode   string  `json:"final_date_mode"`
	FinalDate       *string `json:"final_date,omitempty"`
	FinalOffsetDays *int    `json:"final_offset_days,omitempty"`
}

// parseDateOnly parses a wire date-only string ("2006-01-02") into a UTC
// midnight time.
func parseDateOnly(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// finalDateParam converts a date-only wire string into the pgx DATE parameter
// shape (nil for an unset date).
func finalDateParam(d *string) any {
	if d == nil {
		return nil
	}
	t, err := parseDateOnly(*d)
	if err != nil {
		return nil
	}
	return t
}

// normalizeFinalDatePolicy validates and completes a cup's final-date
// declaration (IM10): the mode defaults to 'calculated', a calculated cup's
// offset defaults to 3 and must be >= 0 (any supplied date is ignored), and a
// fixed cup requires a parseable final date (offsets are ignored).
func normalizeFinalDatePolicy(p FinalDatePolicy) (FinalDatePolicy, error) {
	out := FinalDatePolicy{FinalDateMode: p.FinalDateMode}
	if out.FinalDateMode == "" {
		out.FinalDateMode = cupFinalModeCalculated
	}
	switch out.FinalDateMode {
	case cupFinalModeCalculated:
		out.FinalDate = nil // created cups always derive the final
		if p.FinalOffsetDays == nil {
			d := cupDefaultFinalOffsetDays
			out.FinalOffsetDays = &d
		} else {
			out.FinalOffsetDays = p.FinalOffsetDays
		}
		if *out.FinalOffsetDays < 0 {
			return out, fmt.Errorf("%w: final_offset_days must be >= 0", ErrCupFinalDateInvalid)
		}
		return out, nil
	case cupFinalModeFixed:
		if p.FinalDate == nil || *p.FinalDate == "" {
			return out, fmt.Errorf("%w: fixed cups require a final_date", ErrCupFinalDateInvalid)
		}
		d, err := parseDateOnly(*p.FinalDate)
		if err != nil {
			return out, fmt.Errorf("%w: final_date must be a calendar date (YYYY-MM-DD)", ErrCupFinalDateInvalid)
		}
		d = daysTruncate(d)
		s := d.Format("2006-01-02")
		out.FinalDate = &s
		off := cupDefaultFinalOffsetDays
		out.FinalOffsetDays = &off // fixed ignores offsets; the column keeps its default
		return out, nil
	default:
		return out, fmt.Errorf("%w: final_date_mode must be 'calculated' or 'fixed'", ErrCupFinalDateInvalid)
	}
}

// Cup is a knockout cup competition decorated with its scope (country or
// region), soft tier, and rules. Country is nil for regional cups; Region and
// Tier are nil for country cups.
type Cup struct {
	ID                uuid.UUID          `json:"id"`
	WorldID           uuid.UUID          `json:"world_id"`
	Country           *apiref.CountryRef `json:"country,omitempty"`
	Region            *RegionRef         `json:"region,omitempty"`
	Tier              *int               `json:"tier,omitempty"`
	Name              string             `json:"name"`
	CompetitionType   string             `json:"competition_type"`
	Status            string             `json:"status"`
	PrizePool         float64            `json:"prize_pool"`
	Format            string             `json:"format"`
	IsHomeAndAway     bool               `json:"is_home_and_away"`
	FirstTierBye      int                `json:"first_tier_bye"`
	SurvivorThreshold int                `json:"survivor_threshold"`
	FinalDatePolicy
}

// CupTie is one bracket tie: a scheduled fixture with a decided winner where
// the round has completed.
type CupTie struct {
	ID          uuid.UUID       `json:"id"`
	Home        apiref.ClubRef  `json:"home_club"`
	Away        apiref.ClubRef  `json:"away_club"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	Status      string          `json:"status"`
	HomeScore   *int            `json:"home_score,omitempty"`
	AwayScore   *int            `json:"away_score,omitempty"`
	Winner      *apiref.ClubRef `json:"winner,omitempty"`
}

// CupRound is one materialized round: its ties plus the clubs that advanced
// to it on a bye (no tie this round).
type CupRound struct {
	Round       int              `json:"round"`
	ScheduledAt *time.Time       `json:"scheduled_at,omitempty"`
	Ties        []CupTie         `json:"ties"`
	Byes        []apiref.ClubRef `json:"byes,omitempty"`
}

// CupCampaign is the manager cup view: cup, qualification read-back, season,
// the materialized round tree, and the champion once decided.
type CupCampaign struct {
	Cup                Cup             `json:"cup"`
	QualificationRules json.RawMessage `json:"qualification_rules"`
	LateEntryRound     int             `json:"late_entry_round"`
	TotalRounds        int             `json:"total_rounds"`
	Season             *Season         `json:"season,omitempty"`
	Champion           *apiref.ClubRef `json:"champion,omitempty"`
	Rounds             []CupRound      `json:"rounds"`
}

// roundPlan is one round of the computed cup ladder (JSON-tagged so the plan
// persists the ladder for lazy materialization).
type roundPlan struct {
	Round int `json:"round"`
	N     int `json:"n"`
	Ties  int `json:"ties"`
	Byes  int `json:"byes"`
	// Date is the IM05 anchored calendar slot: the final lands a few days
	// after the country's latest league fixture, and earlier rounds walk
	// backward on seeded 2-3-day gaps snapped to league-free days. Nil keeps
	// the legacy one-round-per-week placement (used when the country has no
	// league season to anchor against).
	Date *time.Time `json:"date,omitempty"`
}

// cupPlan is the per-cup campaign read-back stored on
// competition_rules.qualification_rules at campaign time so later rounds
// materialize against the same entrants the bracket was drawn from. The full
// ladder is persisted so lazy materialization reproduces the landing-round
// sizes exactly (naive halving would drift from the join trigger).
type cupPlan struct {
	TopNClubIDs []uuid.UUID `json:"top_n_club_ids"`
	LateEntry   int         `json:"late_entry_round"`
	Total       int         `json:"total_rounds"`
	Ladder      []roundPlan `json:"ladder"`
	Joined      bool        `json:"joined"`
	// Entrants is the campaign's field with each club's IM07 origin (recorded
	// for preview/news; regional cups always set it, domestic cups may not).
	Entrants []cupPlanEntrant `json:"entrants,omitempty"`
}

// cupPlanEntrant is one campaign field entrant with its qualification origin.
type cupPlanEntrant struct {
	ClubID uuid.UUID `json:"club_id"`
	Origin string    `json:"origin"`
}

// cupLateEntry mirrors the admin-facing qualification JSON.
type cupLateEntry struct {
	Teams              int `json:"teams"`
	EnterWhenSurvivors int `json:"enter_when_survivors"`
}

// cupRulesDoc is the qualification_rules document shape used by both create
// and campaign steps.
type cupRulesDoc struct {
	Entry              string       `json:"entry"`
	FirstTierLateEntry cupLateEntry `json:"first_tier_late_entry"`
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}
