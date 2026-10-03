package player

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// dossierHistoryLimit caps every log section of a single-player dossier.
const dossierHistoryLimit = 10

// PlayerDossier is everything the player schema stores about one player except
// the potential ceiling (OPD-60, IM37). The snapshot sections are filled for
// any player in the caller's world. History is present on single-player reads
// only (never the roster), each log capped at dossierHistoryLimit rows, newest
// first. Private is present only for the caller's own players.
type PlayerDossier struct {
	Bio             PlayerBio                 `json:"bio"`
	AttributeValues map[string]map[string]int `json:"attribute_values"`
	Development     *DevelopmentState         `json:"development"`
	Condition       *PlayerCondition          `json:"condition"`
	Personality     *PersonalityTraits        `json:"personality"`
	History         *DossierHistory           `json:"history,omitempty"`
	Private         *DossierPrivate           `json:"private,omitempty"`
}

// PlayerBio is the player.players columns not already on the response.
type PlayerBio struct {
	PlayerID             uuid.UUID  `db:"id" json:"-"`
	SecondaryPositions   []string   `db:"secondary_positions" json:"secondary_positions"`
	MarketValue          float64    `db:"market_value" json:"market_value"`
	Status               string     `db:"status" json:"status"`
	IsAcademyProduct     bool       `db:"is_academy_product" json:"is_academy_product"`
	Origin               string     `db:"origin" json:"origin"`
	CountryID            *uuid.UUID `db:"country_id" json:"country_id"`
	DevelopedByManagerID *uuid.UUID `db:"developed_by_manager_id" json:"developed_by_manager_id"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
}

// DevelopmentState is player.player_development minus the potential columns.
type DevelopmentState struct {
	PlayerID                 uuid.UUID `db:"player_id" json:"-"`
	LastEvalWeek             int64     `db:"last_eval_week" json:"last_eval_week"`
	CumDevWeeks              int       `db:"cum_dev_weeks" json:"cum_dev_weeks"`
	ConsecutiveStagnantWeeks int       `db:"consecutive_stagnant_weeks" json:"consecutive_stagnant_weeks"`
	UpdatedAt                time.Time `db:"updated_at" json:"updated_at"`
}

// PlayerCondition is player.player_condition ([0,1] each) minus the transfer
// request cooldown, which is private.
type PlayerCondition struct {
	PlayerID            uuid.UUID `db:"player_id" json:"-"`
	Fatigue             float64   `db:"fatigue" json:"fatigue"`
	Fitness             float64   `db:"fitness" json:"fitness"`
	Sharpness           float64   `db:"sharpness" json:"sharpness"`
	InjuryRisk          float64   `db:"injury_risk" json:"injury_risk"`
	TacticalFamiliarity float64   `db:"tactical_familiarity" json:"tactical_familiarity"`
	Morale              float64   `db:"morale" json:"morale"`
	PlayingTimePct      float64   `db:"playing_time_pct" json:"playing_time_pct"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
}

// PersonalityTraits is player.player_personality ([1,100] each).
type PersonalityTraits struct {
	PlayerID            uuid.UUID `db:"player_id" json:"-"`
	Professionalism     int       `db:"professionalism" json:"professionalism"`
	Ambition            int       `db:"ambition" json:"ambition"`
	Loyalty             int       `db:"loyalty" json:"loyalty"`
	Ego                 int       `db:"ego" json:"ego"`
	Sociability         int       `db:"sociability" json:"sociability"`
	Adaptability        int       `db:"adaptability" json:"adaptability"`
	Patience            int       `db:"patience" json:"patience"`
	Leadership          int       `db:"leadership" json:"leadership"`
	EmotionalVolatility int       `db:"emotional_volatility" json:"emotional_volatility"`
}

// DossierHistory holds the public logs. Slices are never nil.
type DossierHistory struct {
	Appearances      []AppearanceLog   `json:"appearances"`
	AttributeChanges []AttributeChange `json:"attribute_changes"`
	Injuries         []InjuryLog       `json:"injuries"`
	Events           []HistoryEvent    `json:"events"`
}

type AppearanceLog struct {
	MatchID  uuid.UUID  `db:"match_id" json:"match_id"`
	PlayedAt *time.Time `db:"played_at" json:"played_at"`
	Started  bool       `db:"started" json:"started"`
	Minutes  int        `db:"minutes" json:"minutes"`
	Rating   *int       `db:"rating" json:"rating"`
	Goals    int        `db:"goals" json:"goals"`
	Assists  int        `db:"assists" json:"assists"`
}

type AttributeChange struct {
	AppliedWeek  int64     `db:"applied_week" json:"applied_week"`
	AttributeKey string    `db:"attribute_key" json:"attribute_key"`
	Delta        int       `db:"delta" json:"delta"`
	RecordedAt   time.Time `db:"recorded_at" json:"recorded_at"`
}

type InjuryLog struct {
	ID                   uuid.UUID  `db:"id" json:"id"`
	InjuryType           string     `db:"injury_type" json:"injury_type"`
	Severity             int        `db:"severity" json:"severity"`
	ExpectedRecoveryDate time.Time  `db:"expected_recovery_date" json:"expected_recovery_date"`
	ActualRecoveryDate   *time.Time `db:"actual_recovery_date" json:"actual_recovery_date"`
	RecurrenceRisk       float64    `db:"recurrence_risk" json:"recurrence_risk"`
	SetbackDays          int        `db:"setback_days" json:"setback_days"`
	MatchID              *uuid.UUID `db:"match_id" json:"match_id"`
	OccurredAt           time.Time  `db:"occurred_at" json:"occurred_at"`
}

type HistoryEvent struct {
	ID          uuid.UUID  `db:"id" json:"id"`
	Season      int        `db:"season" json:"season"`
	ClubID      *uuid.UUID `db:"club_id" json:"club_id"`
	EventType   string     `db:"event_type" json:"event_type"`
	Description string     `db:"description" json:"description"`
	OccurredAt  time.Time  `db:"occurred_at" json:"occurred_at"`
}

// DossierPrivate holds what only the player's own club may see. Slices are
// never nil.
type DossierPrivate struct {
	TransferRequestCooldownUntil *time.Time           `json:"transfer_request_cooldown_until"`
	Contracts                    []ContractLog        `json:"contracts"`
	EmotionalStates              []EmotionalLog       `json:"emotional_states"`
	Preferences                  []PreferenceRow      `json:"preferences"`
	TransferRequests             []TransferRequestLog `json:"transfer_requests"`
}

type ContractLog struct {
	ID                 uuid.UUID `db:"id" json:"id"`
	ClubID             uuid.UUID `db:"club_id" json:"club_id"`
	WeeklyWage         float64   `db:"weekly_wage" json:"weekly_wage"`
	SigningBonus       float64   `db:"signing_bonus" json:"signing_bonus"`
	StartDate          time.Time `db:"start_date" json:"start_date"`
	EndDate            time.Time `db:"end_date" json:"end_date"`
	ReleaseClause      *float64  `db:"release_clause" json:"release_clause"`
	PlayingTimePromise *string   `db:"playing_time_promise" json:"playing_time_promise"`
	Status             string    `db:"status" json:"status"`
	SquadRole          *string   `db:"squad_role" json:"squad_role"`
	ContractType       string    `db:"contract_type" json:"contract_type"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
}

type EmotionalLog struct {
	EmotionalState string     `db:"emotional_state" json:"emotional_state"`
	Cause          string     `db:"cause" json:"cause"`
	Intensity      int        `db:"intensity" json:"intensity"`
	OccurredAt     time.Time  `db:"occurred_at" json:"occurred_at"`
	ExpiresAt      *time.Time `db:"expires_at" json:"expires_at"`
}

type PreferenceRow struct {
	PreferenceType  string `db:"preference_type" json:"preference_type"`
	PreferenceValue string `db:"preference_value" json:"preference_value"`
	Strength        *int   `db:"strength" json:"strength"`
}

type TransferRequestLog struct {
	ID             uuid.UUID  `db:"id" json:"id"`
	ClubID         uuid.UUID  `db:"club_id" json:"club_id"`
	Status         string     `db:"status" json:"status"`
	Reason         string     `db:"reason" json:"reason"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	ResolvedAt     *time.Time `db:"resolved_at" json:"resolved_at"`
	ReassuredUntil *time.Time `db:"reassured_until" json:"reassured_until"`
}

// collect runs one query and scans every row into T by column name. The result
// is never nil, so an empty log serialises as [].
func collect[T any](ctx context.Context, q dbtx, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if out == nil {
		out = []T{}
	}
	return out, err
}

// loadDossiers loads the snapshot sections for a set of players in one query
// per table (the roster path). Every requested id gets a dossier; a section
// with no row stays nil.
func loadDossiers(ctx context.Context, q dbtx, ids []uuid.UUID) (map[uuid.UUID]*PlayerDossier, error) {
	out := make(map[uuid.UUID]*PlayerDossier, len(ids))
	for _, id := range ids {
		out[id] = &PlayerDossier{AttributeValues: map[string]map[string]int{}}
	}

	bios, err := collect[PlayerBio](ctx, q, `
		SELECT id, secondary_positions, market_value, status, is_academy_product,
		       origin, country_id, developed_by_manager_id, created_at
		FROM player.players WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for _, b := range bios {
		out[b.PlayerID].Bio = b
	}

	type attrRow struct {
		PlayerID uuid.UUID `db:"player_id"`
		Category string    `db:"attribute_category"`
		Key      string    `db:"attribute_key"`
		Value    int       `db:"value"`
	}
	attrs, err := collect[attrRow](ctx, q, `
		SELECT player_id, attribute_category, attribute_key, value
		FROM player.player_attributes WHERE player_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range attrs {
		vals := out[a.PlayerID].AttributeValues
		if vals[a.Category] == nil {
			vals[a.Category] = map[string]int{}
		}
		vals[a.Category][a.Key] = a.Value
	}

	devs, err := collect[DevelopmentState](ctx, q, `
		SELECT player_id, last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks, updated_at
		FROM player.player_development WHERE player_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for i := range devs {
		out[devs[i].PlayerID].Development = &devs[i]
	}

	conds, err := collect[PlayerCondition](ctx, q, `
		SELECT player_id, fatigue, fitness, sharpness, injury_risk, tactical_familiarity,
		       morale, playing_time_pct, updated_at
		FROM player.player_condition WHERE player_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for i := range conds {
		out[conds[i].PlayerID].Condition = &conds[i]
	}

	pers, err := collect[PersonalityTraits](ctx, q, `
		SELECT player_id, professionalism, ambition, loyalty, ego, sociability,
		       adaptability, patience, leadership, emotional_volatility
		FROM player.player_personality WHERE player_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for i := range pers {
		out[pers[i].PlayerID].Personality = &pers[i]
	}
	return out, nil
}

// playerDossier is the single-player dossier: snapshot + history, plus the
// private section when ownClub is true. The caller decides ownership.
func playerDossier(ctx context.Context, q dbtx, playerID uuid.UUID, ownClub bool) (*PlayerDossier, error) {
	all, err := loadDossiers(ctx, q, []uuid.UUID{playerID})
	if err != nil {
		return nil, err
	}
	d := all[playerID]
	if d.History, err = dossierHistory(ctx, q, playerID); err != nil {
		return nil, err
	}
	if ownClub {
		if d.Private, err = dossierPrivate(ctx, q, playerID); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func dossierHistory(ctx context.Context, q dbtx, playerID uuid.UUID) (h *DossierHistory, err error) {
	h = &DossierHistory{}
	if h.Appearances, err = collect[AppearanceLog](ctx, q, `
		SELECT a.match_id, m.ended_at AS played_at, a.started, a.minutes, a.rating, a.goals, a.assists
		FROM player.player_appearances a
		JOIN match.matches m ON m.id = a.match_id
		WHERE a.player_id = $1
		ORDER BY m.ended_at DESC NULLS LAST LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	if h.AttributeChanges, err = collect[AttributeChange](ctx, q, `
		SELECT applied_week, attribute_key, delta, recorded_at
		FROM player.player_attribute_changes
		WHERE player_id = $1
		ORDER BY applied_week DESC, attribute_key LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	if h.Injuries, err = collect[InjuryLog](ctx, q, `
		SELECT i.id, i.injury_type, i.severity, i.expected_recovery_date, i.actual_recovery_date,
		       i.recurrence_risk,
		       COALESCE((SELECT SUM(s.days_added) FROM player.injury_setbacks s WHERE s.injury_id = i.id), 0)::int AS setback_days,
		       i.match_id, i.occurred_at
		FROM player.injuries i
		WHERE i.player_id = $1
		ORDER BY i.occurred_at DESC LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	if h.Events, err = collect[HistoryEvent](ctx, q, `
		SELECT id, season, club_id, event_type, description, occurred_at
		FROM player.player_history
		WHERE player_id = $1
		ORDER BY occurred_at DESC LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	return h, nil
}

func dossierPrivate(ctx context.Context, q dbtx, playerID uuid.UUID) (p *DossierPrivate, err error) {
	p = &DossierPrivate{}
	if err = q.QueryRow(ctx, `
		SELECT transfer_request_cooldown_until FROM player.player_condition WHERE player_id = $1`,
		playerID).Scan(&p.TransferRequestCooldownUntil); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if p.Contracts, err = collect[ContractLog](ctx, q, `
		SELECT id, club_id, weekly_wage, signing_bonus, start_date, end_date, release_clause,
		       playing_time_promise, status, squad_role, contract_type, created_at
		FROM player.contracts
		WHERE player_id = $1
		ORDER BY start_date DESC, created_at DESC LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	if p.EmotionalStates, err = collect[EmotionalLog](ctx, q, `
		SELECT emotional_state, cause, intensity, occurred_at, expires_at
		FROM player.player_emotional_states
		WHERE player_id = $1
		ORDER BY occurred_at DESC LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	// Preferences are a bounded profile, not a log: all of them.
	if p.Preferences, err = collect[PreferenceRow](ctx, q, `
		SELECT preference_type, preference_value, strength
		FROM player.player_preferences
		WHERE player_id = $1
		ORDER BY preference_type, strength DESC NULLS LAST`, playerID); err != nil {
		return nil, err
	}
	if p.TransferRequests, err = collect[TransferRequestLog](ctx, q, `
		SELECT id, club_id, status, reason, created_at, resolved_at, reassured_until
		FROM player.player_transfer_requests
		WHERE player_id = $1
		ORDER BY created_at DESC LIMIT $2`, playerID, dossierHistoryLimit); err != nil {
		return nil, err
	}
	return p, nil
}
