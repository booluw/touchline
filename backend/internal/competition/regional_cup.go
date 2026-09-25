package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
)

// ---------------------------------------------------------------------------
// Regional (continental) cups (IM08)
//
// A regional cup is a knockout cup whose field draws from per-league position
// bands across the countries of a region, computed by the IM07 engine
// (qualify.go) with the reigning-champion +1. Campaigns anchor their calendar
// to the union of the participating countries' league days and close exactly
// like a domestic cup (IM04). The champion and its next-best cascade entrant
// are exempt from the 3-cup membership cap; positional entrants stay capped.
// ---------------------------------------------------------------------------

// regionalEntryKind is the qualification source written to the
// qualification_rules mirror at creation (introspection only; the
// cup_qualification rows are authoritative).
const regionalEntryKind = "regional_league_bands"

// QualBandInput is the admin payload for one per-league position band:
// from_position..to_position (to nil runs through last place).
type QualBandInput struct {
	LeagueID uuid.UUID `json:"league_id"`
	From     int       `json:"from_position"`
	To       *int      `json:"to_position,omitempty"`
}

// RegionalCupParams is the admin declaration for a regional cup.
type RegionalCupParams struct {
	WorldID         uuid.UUID       `json:"world_id"`
	RegionID        uuid.UUID       `json:"region_id"`
	Tier            *int            `json:"tier,omitempty"`
	Name            string          `json:"name"`
	PrizePool       float64         `json:"prize_pool,omitempty"`
	SchedulingRules json.RawMessage `json:"scheduling_rules,omitempty"`
	Qualification   []QualBandInput `json:"qualification"`
	FinalDatePolicy
}

// CupPreviewParams is the draft-configuration payload for POST /api/admin/cups/preview.
// CupID is optional: giving an existing cup lets a re-preview surface the
// reigning-champion +1 after the cup has completed a campaign; a fresh draft
// has no champion yet.
type CupPreviewParams struct {
	WorldID       uuid.UUID       `json:"world_id"`
	RegionID      uuid.UUID       `json:"region_id"`
	Tier          *int            `json:"tier,omitempty"`
	Name          string          `json:"name,omitempty"`
	Qualification []QualBandInput `json:"qualification"`
	CupID         *uuid.UUID      `json:"cup_id,omitempty"`
}

// CupPreviewEntrant is one projected field row with the club's scope context.
type CupPreviewEntrant struct {
	ClubID  uuid.UUID          `json:"club_id"`
	Country *apiref.CountryRef `json:"country,omitempty"`
	League  *apiref.LeagueRef  `json:"league,omitempty"`
	Origin  string             `json:"origin"`
	Rank    int                `json:"rank"`
}

// CupPreviewResult is the preview response: the projected field with origins,
// the reigning champion (when the +1 applied), and human-readable warnings.
type CupPreviewResult struct {
	Field     []CupPreviewEntrant `json:"field"`
	Champion  *CupPreviewEntrant  `json:"champion,omitempty"`
	Warnings  []string            `json:"warnings"`
	FieldSize int                 `json:"field_size"`
}

// RegionalCampaignResult is what starting a regional cup campaign returns: the
// season plus any calendar warnings (e.g. no participating league calendar, so
// rounds fell back to weekly pacing).
type RegionalCampaignResult struct {
	Season   *Season  `json:"season"`
	Warnings []string `json:"warnings,omitempty"`
}

// qualBandDoc is the JSON shape of one band in the qualification_rules mirror.
type qualBandDoc struct {
	LeagueID uuid.UUID `json:"league_id"`
	From     int       `json:"from_position"`
	To       *int      `json:"to_position,omitempty"`
}

// regionalRulesDoc is the qualification_rules mirror for a regional cup
// (bands + tier + entry kind), populated at creation and refreshed by
// SetQualification. Bands are sorted for a stable read-back.
type regionalRulesDoc struct {
	Entry string        `json:"entry"`
	Tier  *int          `json:"tier,omitempty"`
	Bands []qualBandDoc `json:"bands"`
}

// databaseQuerier is the slice of *pgxpool.Pool / pgx.Tx the regional reads
// need (rowQueryer plus multi-row Query).
type databaseQuerier interface {
	rowQueryer
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// DefaultBandForReputation is the wizard suggestion for a league: a default
// from..to band driven by the league's reputation (clamped to 0..100). These
// are UI defaults only — the engine reads the committed cup_qualification rows.
func DefaultBandForReputation(rep int) (from, to int) {
	if rep > 100 {
		rep = 100
	}
	if rep < 0 {
		rep = 0
	}
	from, to = 1, 1
	switch {
	case rep >= 90:
		to = 4
	case rep >= 75:
		to = 3
	case rep >= 60:
		to = 2
	}
	return from, to
}

// inputsToBands converts admin payload bands into engine Band rows, validating
// each row's positions.
func inputsToBands(inputs []QualBandInput) ([]Band, error) {
	out := make([]Band, 0, len(inputs))
	for _, in := range inputs {
		b := Band{LeagueID: in.LeagueID, From: in.From, To: in.To}
		if err := validateQualBand(b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// regionalRulesJSON renders the qualification_rules mirror for a regional cup.
func regionalRulesJSON(tier *int, bands []Band) ([]byte, error) {
	doc := regionalRulesDoc{Entry: regionalEntryKind, Tier: tier}
	for _, b := range sortedBands(bands) {
		doc.Bands = append(doc.Bands, qualBandDoc{LeagueID: b.LeagueID, From: b.From, To: b.To})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal regional qualification rules: %w", err)
	}
	return b, nil
}

// validateBandsNoOverlap rejects overlapping bands on the same league (bands
// must not share a position; to nil means through last place, so nothing may
// follow it). Pure for the unit tests.
func validateBandsNoOverlap(bands []Band) error {
	byLeague := map[uuid.UUID][]Band{}
	for _, b := range bands {
		byLeague[b.LeagueID] = append(byLeague[b.LeagueID], b)
	}
	for leagueID, list := range byLeague {
		prevEnd := 0 // inclusive upper bound of the previous band
		for _, b := range sortedBands(list) {
			end := int(^uint(0) >> 1)
			if b.To != nil {
				end = *b.To
			}
			if b.From <= prevEnd {
				return fmt.Errorf("%w (league %s)", ErrQualificationOverlap, leagueID)
			}
			prevEnd = end
		}
	}
	return nil
}

// regionalRef builds the engine-facing identity of an already-loaded regional cup.
func regionalRef(cup *Cup) CompetitionRef {
	ref := CompetitionRef{
		ID:      cup.ID,
		WorldID: cup.WorldID,
		Name:    cup.Name,
		Type:    cup.CompetitionType,
		Scope:   ScopeRegion,
	}
	if cup.Region != nil {
		ref.RegionID = cup.Region.ID
	}
	if cup.Tier != nil {
		ref.Tier = *cup.Tier
	}
	return ref
}

// validateRegionalBands asserts every banded league is a real league whose
// country sits inside the region, and that no two bands overlap. A missing
// league is ErrCompetitionNotFound; a league outside the region is
// ErrRegionMismatch.
func (s *Service) validateRegionalBands(ctx context.Context, q databaseQuerier, regionID uuid.UUID, bands []Band) error {
	if err := validateBandsNoOverlap(bands); err != nil {
		return err
	}
	if len(bands) == 0 {
		return nil
	}

	rows, err := q.Query(ctx, `
		SELECT c.id
		FROM competition.competitions c
		JOIN world.countries wc ON wc.id = c.country_id
		WHERE c.id = ANY($1::uuid[]) AND c.competition_type = 'league' AND wc.region_id = $2`,
		uniqueLeagueIDs(bands), regionID)
	if err != nil {
		return fmt.Errorf("check banded leagues: %w", err)
	}
	inRegion := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan banded league: %w", err)
		}
		inRegion[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	missing := []uuid.UUID{}
	for _, lid := range uniqueLeagueIDs(bands) {
		if !inRegion[lid] {
			missing = append(missing, lid)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	leagues, err := q.Query(ctx, `
		SELECT c.id FROM competition.competitions c
		WHERE c.id = ANY($1::uuid[]) AND c.competition_type = 'league'`, missing)
	if err != nil {
		return fmt.Errorf("recheck banded leagues: %w", err)
	}
	realLeagues := map[uuid.UUID]bool{}
	for leagues.Next() {
		var id uuid.UUID
		if err := leagues.Scan(&id); err != nil {
			leagues.Close()
			return fmt.Errorf("scan recheck league: %w", err)
		}
		realLeagues[id] = true
	}
	leagues.Close()
	if err := leagues.Err(); err != nil {
		return err
	}
	for _, lid := range missing {
		if !realLeagues[lid] {
			return ErrCompetitionNotFound
		}
	}
	return ErrRegionMismatch
}

// bandsForCup loads a cup's cup_qualification rows as engine Band rows
// (sorted; the canonical read path for both ComputeCupField and campaigns).
func (s *Service) bandsForCup(ctx context.Context, q databaseQuerier, cupID uuid.UUID) ([]Band, error) {
	rows, err := q.Query(ctx, `
		SELECT league_id, from_position, to_position
		FROM competition.cup_qualification WHERE cup_id = $1
		ORDER BY league_id, from_position`, cupID)
	if err != nil {
		return nil, fmt.Errorf("qualification bands of %s: %w", cupID, err)
	}
	defer rows.Close()
	out := []Band{}
	for rows.Next() {
		var b Band
		if err := rows.Scan(&b.LeagueID, &b.From, &b.To); err != nil {
			return nil, fmt.Errorf("scan qualification band: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// insertQualBands persists a cup's qualification rows (replace semantics).
func insertQualBands(ctx context.Context, tx pgx.Tx, cupID uuid.UUID, bands []Band) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM competition.cup_qualification WHERE cup_id = $1`, cupID); err != nil {
		return fmt.Errorf("clear qualification bands: %w", err)
	}
	for _, b := range bands {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.cup_qualification (cup_id, league_id, from_position, to_position)
			VALUES ($1, $2, $3, $4)`, cupID, b.LeagueID, b.From, b.To); err != nil {
			return fmt.Errorf("insert qualification band: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Create / edit / preview
// ---------------------------------------------------------------------------

// CreateRegionalCup declares a region-scoped continental cup: region_id, an
// optional soft tier, and the per-league qualification bands. Only the static
// checks run here (scope, bands, name); the field itself is validated at
// preview and campaign time when season tables exist.
func (s *Service) CreateRegionalCup(ctx context.Context, p RegionalCupParams) (*Cup, error) {
	name := trimSpace(p.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if p.Tier != nil && *p.Tier < 1 {
		return nil, ErrInvalidTier
	}
	bands, err := inputsToBands(p.Qualification)
	if err != nil {
		return nil, err
	}
	policy, err := normalizeFinalDatePolicy(p.FinalDatePolicy)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin regional cup: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var regionWorld uuid.UUID
	err = tx.QueryRow(ctx, `SELECT world_id FROM world.regions WHERE id = $1`, p.RegionID).Scan(&regionWorld)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load region: %w", err)
	}
	if regionWorld != p.WorldID {
		return nil, ErrCompetitionWorldMismatch
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.competitions
			WHERE world_id = $1 AND lower(name) = lower($2))`,
		p.WorldID, name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check cup name: %w", err)
	}
	if exists {
		return nil, ErrNameCollision
	}

	if err := s.validateRegionalBands(ctx, tx, p.RegionID, bands); err != nil {
		return nil, err
	}

	rules, err := cupRulesJSON(p.SchedulingRules)
	if err != nil {
		return nil, err
	}
	mirror, err := regionalRulesJSON(p.Tier, bands)
	if err != nil {
		return nil, err
	}

	var cupID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.competitions
			(world_id, region_id, tier, name, competition_type, prize_pool, status,
			 final_date_mode, final_date, final_offset_days)
		VALUES ($1, $2, $3, $4, 'continental', $5, 'active', $6, $7, $8)
		RETURNING id`, p.WorldID, p.RegionID, p.Tier, name, p.PrizePool,
		policy.FinalDateMode, finalDateParam(policy.FinalDate), policy.FinalOffsetDays).Scan(&cupID); err != nil {
		return nil, fmt.Errorf("insert regional cup: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO competition.competition_rules
			(competition_id, format, qualification_rules, is_home_and_away, scheduling_rules)
		VALUES ($1, 'knockout', $2, FALSE, $3)`,
		cupID, mirror, rules); err != nil {
		return nil, fmt.Errorf("insert regional cup rules: %w", err)
	}
	if err := insertQualBands(ctx, tx, cupID, bands); err != nil {
		return nil, err
	}

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit regional cup: %w", err)
	}
	return cup, nil
}

// SetQualification replaces a regional cup's qualification bands, re-running
// the scope/overlap validation and refreshing the qualification_rules mirror.
// The write happens before any campaign exists.
func (s *Service) SetQualification(ctx context.Context, cupID uuid.UUID, inputs []QualBandInput) (*Cup, error) {
	bands, err := inputsToBands(inputs)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin set qualification: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if cup.CompetitionType != "continental" {
		return nil, ErrCompetitionTypeMismatch
	}
	if err := s.validateRegionalBands(ctx, tx, cup.Region.ID, bands); err != nil {
		return nil, err
	}

	mirror, err := regionalRulesJSON(cup.Tier, bands)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_rules SET qualification_rules = $2
		WHERE competition_id = $1`, cupID, mirror); err != nil {
		return nil, fmt.Errorf("refresh qualification mirror: %w", err)
	}
	if err := insertQualBands(ctx, tx, cupID, bands); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit set qualification: %w", err)
	}
	return cup, nil
}

// PreviewCupField projects a regional cup draft's field through the IM07
// engine without writing anything. The reigning-champion +1 applies exactly as
// it would at campaign start (a re-preview with CupID set shows it). Bands and
// scope are validated; a field smaller than two clubs is a warning, not an
// error, so the admin sees the recommendation to widen the bands.
func (s *Service) PreviewCupField(ctx context.Context, p CupPreviewParams) (*CupPreviewResult, error) {
	bands, err := inputsToBands(p.Qualification)
	if err != nil {
		return nil, err
	}

	var regionWorld uuid.UUID
	err = s.pool.QueryRow(ctx, `SELECT world_id FROM world.regions WHERE id = $1`, p.RegionID).Scan(&regionWorld)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load region: %w", err)
	}
	if regionWorld != p.WorldID {
		return nil, ErrCompetitionWorldMismatch
	}
	if err := s.validateRegionalBands(ctx, s.pool, p.RegionID, bands); err != nil {
		return nil, err
	}

	ref := regionalRefFromPreview(p)
	field, err := computeField(ctx, QualifyField{Cup: ref, Bands: bands}, s, s, s)
	if err != nil {
		return nil, err
	}

	warnings := s.previewWarnings(ctx, field, bands)

	entrants, err := s.previewEntrants(ctx, field.Entrants)
	if err != nil {
		return nil, err
	}

	res := &CupPreviewResult{Field: entrants, Warnings: warnings, FieldSize: len(entrants)}
	for _, e := range field.Entrants {
		if e.Origin == OriginChampionDirect || e.Origin == OriginChampionOutOfBand {
			res.Champion = cupPreviewEntrantByClub(entrants, e.ClubID)
			break
		}
	}
	return res, nil
}

// regionalRefFromPreview builds the synthetic engine identity for a draft
// (uuid.Nil cup when no existing cup is re-previewsd, so no champion exists).
func regionalRefFromPreview(p CupPreviewParams) CompetitionRef {
	ref := CompetitionRef{
		WorldID:  p.WorldID,
		RegionID: p.RegionID,
		Name:     p.Name,
		Type:     "continental",
		Tier:     0,
		Scope:    ScopeRegion,
	}
	if p.Tier != nil {
		ref.Tier = *p.Tier
	}
	if p.CupID != nil {
		ref.ID = *p.CupID
	}
	return ref
}

// previewWarnings derives the human-readable warnings the doc contracts:
// per-banded-league "no completed season" / "band empty" / "band partially
// full", plus field size < 2 and cross-cup conflicts (IM09).
func (s *Service) previewWarnings(ctx context.Context, field *Field, bands []Band) []string {
	warnings := []string{}
	if len(field.Entrants) < 2 {
		warnings = append(warnings,
			fmt.Sprintf("field has %d club(s) - widen the bands so at least 2 clubs qualify", len(field.Entrants)))
	}

	unavailable := map[uuid.UUID]bool{}
	for _, u := range field.Unavailable {
		unavailable[u.LeagueID] = true
	}

	leagueIDs := uniqueLeagueIDs(bands)
	tables := map[uuid.UUID]*Table{}
	for _, lid := range leagueIDs {
		if unavailable[lid] {
			warnings = append(warnings,
				fmt.Sprintf("league %s has no completed season yet - its band yields nothing", lid))
			continue
		}
		t, err := s.LastCompletedStandings(ctx, lid)
		if err == nil {
			tables[lid] = t
		}
	}

	if len(tables) > 0 {
		for _, b := range bands {
			t := tables[b.LeagueID]
			if t == nil {
				continue
			}
			lo := b.From - 1
			hi := effectiveTo(t.Ranks, b)
			slots := hi - lo
			if slots < 0 {
				slots = 0
			}
			switch {
			case slots == 0:
				warnings = append(warnings, fmt.Sprintf("band %d..last place of league %s is empty",
					b.From, b.LeagueID))
			case b.To != nil && slots < *b.To-b.From+1:
				warnings = append(warnings, fmt.Sprintf("band %d..%d of league %s is partially full (%d of %d positions)",
					b.From, *b.To, b.LeagueID, slots, *b.To-b.From+1))
			}
		}
	}

	for range field.Conflicts {
		// Full double-booking resolution is IM09; the preview just flags it.
		warnings = append(warnings,
			"a club also appears in another regional cup field - double-booking resolution is IM09")
	}
	sort.Strings(warnings)
	return warnings
}

// previewEntrants enriches the projected field with each club's league + scope
// country (the realm the club currently belongs to), for the admin preview.
func (s *Service) previewEntrants(ctx context.Context, entrants []Entrant) ([]CupPreviewEntrant, error) {
	clubIDs := make([]uuid.UUID, 0, len(entrants))
	for _, e := range entrants {
		clubIDs = append(clubIDs, e.ClubID)
	}
	scope := map[uuid.UUID]*CupPreviewEntrant{}
	if len(clubIDs) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT cc.club_id, c.id, c.name, wc.id, wc.name, wc.code
			FROM competition.club_competitions cc
			JOIN competition.competitions c ON c.id = cc.competition_id AND c.competition_type = 'league'
			JOIN world.countries wc ON wc.id = c.country_id
			WHERE cc.club_id = ANY($1::uuid[]) AND cc.role = 'league'`, clubIDs)
		if err != nil {
			return nil, fmt.Errorf("preview league context: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				clubID            uuid.UUID
				leagueID          uuid.UUID
				leagueName        string
				countryID         uuid.UUID
				countryName, code string
			)
			if err := rows.Scan(&clubID, &leagueID, &leagueName, &countryID, &countryName, &code); err != nil {
				return nil, fmt.Errorf("scan preview league context: %w", err)
			}
			scope[clubID] = &CupPreviewEntrant{
				ClubID:  clubID,
				Country: &apiref.CountryRef{ID: countryID, Name: countryName, Code: code},
				League:  &apiref.LeagueRef{ID: leagueID, Name: leagueName},
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]CupPreviewEntrant, 0, len(entrants))
	for _, e := range entrants {
		pe := CupPreviewEntrant{ClubID: e.ClubID, Origin: e.Origin, Rank: e.Rank}
		if ctxRef, ok := scope[e.ClubID]; ok {
			pe.Country = ctxRef.Country
			pe.League = ctxRef.League
		}
		out = append(out, pe)
	}
	return out, nil
}

// cupPreviewEntrantByClub finds the preview entrant row for a club.
func cupPreviewEntrantByClub(entrants []CupPreviewEntrant, clubID uuid.UUID) *CupPreviewEntrant {
	for i := range entrants {
		if entrants[i].ClubID == clubID {
			return &entrants[i]
		}
	}
	return nil
}

// CupScope reports a cup's competition type and (when scoped to a country) its
// country, so the shared campaign route can dispatch between scopes.
func (s *Service) CupScope(ctx context.Context, cupID uuid.UUID) (competitionType string, countryID *uuid.UUID, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT competition_type, country_id FROM competition.competitions WHERE id = $1`, cupID).
		Scan(&competitionType, &countryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrCompetitionNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("cup scope: %w", err)
	}
	return competitionType, countryID, nil
}

// ---------------------------------------------------------------------------
// Campaign
// ---------------------------------------------------------------------------

// StartRegionalCupCampaign materializes a regional cup's first campaign: the
// IM07 field (bands + reigning-champion +1), cap-exempt memberships, entries
// marked qualified, the anchored calendar over the union of the participating
// countries' league days, and the Round-1 bracket. Mirrors StartCupCampaign:
// exactly one campaign per cup at a time.
func (s *Service) StartRegionalCupCampaign(ctx context.Context, worldID, cupID uuid.UUID) (*RegionalCampaignResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin regional cup campaign: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldStatus string
	var worldRef time.Time
	err = tx.QueryRow(ctx,
		`SELECT status, COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID).
		Scan(&worldStatus, &worldRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if cup.WorldID != worldID {
		return nil, ErrCompetitionWorldMismatch
	}
	if cup.CompetitionType != "continental" {
		return nil, ErrCompetitionTypeMismatch
	}

	var hasSeason bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM competition.seasons WHERE competition_id = $1)`, cupID).
		Scan(&hasSeason); err != nil {
		return nil, fmt.Errorf("check cup seasons: %w", err)
	}
	if hasSeason {
		return nil, ErrCupCampaignExists
	}

	bands, err := s.bandsForCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRegionalBands(ctx, tx, cup.Region.ID, bands); err != nil {
		return nil, err
	}

	// Resolution sweep (IM09): recompute every continental cup's IM07
	// entitlement-max field tx-consistently, consume manager_cup_choices, and
	// resolve every double-booked champion to exactly one cup before any
	// membership/entry is written. The starting cup writes the *resolved*
	// field; the cascade chain becomes the commitment news below.
	resolved, fieldsByCup, err := s.resolveContinentalCups(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	field := fieldsByCup[cupID]
	if len(field.Unavailable) > 0 {
		league := field.Unavailable[0].LeagueID
		return nil, fmt.Errorf("%w (league %s)", ErrQualificationUnavailable, league)
	}
	entrants := resolved.Assignments[cupID]

	reigning, err := s.ReigningChampion(ctx, cupID)
	if err != nil {
		return nil, err
	}
	countries, err := s.regionCampaignCountries(ctx, tx, cupID, reigning)
	if err != nil {
		return nil, err
	}

	ladder, err := cupLadder(len(entrants), 2, 0)
	if err != nil {
		return nil, err
	}

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return nil, fmt.Errorf("load world seed: %w", err)
	}

	anchorDays, err := s.unionLeagueDays(ctx, tx, worldID, countries)
	if err != nil {
		return nil, err
	}
	ladder, err = s.planCupCalendar(ctx, tx, worldID, anchorDays, cupID, seed, ladder)
	if err != nil {
		return nil, err
	}

	if err := s.writeRegionalMemberships(ctx, tx, worldID, cupID, entrants, cupMaxMemberships); err != nil {
		return nil, err
	}

	entrantIDs := make([]uuid.UUID, 0, len(entrants))
	for _, e := range entrants {
		entrantIDs = append(entrantIDs, e.ClubID)
	}
	sort.Slice(entrantIDs, func(i, j int) bool { return entrantIDs[i].String() < entrantIDs[j].String() })

	season, err := s.createSeason(ctx, tx, worldID, cupID, worldRef, entrantIDs)
	if err != nil {
		return nil, err
	}
	if err := s.setEntryQualified(ctx, tx, season.ID, entrantIDs); err != nil {
		return nil, err
	}

	planDoc := cupPlan{
		Total:    len(ladder),
		Ladder:   ladder,
		Entrants: fieldEntrantsToPlan(entrants),
	}
	pb, err := json.Marshal(planDoc)
	if err != nil {
		return nil, fmt.Errorf("marshal regional campaign plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_rules
		SET qualification_rules = jsonb_set(
			COALESCE(qualification_rules, '{}'::jsonb), '{campaign}', $2::jsonb)
		WHERE competition_id = $1`, cupID, pb); err != nil {
		return nil, fmt.Errorf("persist regional campaign plan: %w", err)
	}

	if err := s.materializeRound(ctx, tx, worldID, cupID, season.ID, ladder[0], entrantIDs, worldRef, seed); err != nil {
		return nil, err
	}

	campaignEvent := &eventbus.Event{
		WorldID:   worldID,
		EventType: "CUP_CAMPAIGN_STARTED",
		Payload: mustJSON(map[string]any{
			"competition_id": cupID,
			"season_id":      season.ID,
			"region_id":      cup.Region.ID,
			"team_count":     len(entrants),
			"rounds":         len(ladder),
		}),
	}
	if err := s.recordSeedEvent(ctx, tx, campaignEvent); err != nil {
		return nil, err
	}

	if err := s.publishCupCascadeNews(ctx, tx, worldID, cupID, campaignEvent.ID, resolved.Cascades); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit regional cup campaign: %w", err)
	}

	res := &RegionalCampaignResult{Season: season}
	if len(ladder) > 0 && ladder[len(ladder)-1].Date == nil {
		res.Warnings = append(res.Warnings,
			"no participating country has a league calendar yet - cup rounds use the weekly default pacing")
	}
	return res, nil
}

// fieldEntrantsToPlan encodes the field (with each club's IM07 origin) for the
// campaign plan; IM09's precedence sweep and news read it back.
func fieldEntrantsToPlan(entrants []Entrant) []cupPlanEntrant {
	out := make([]cupPlanEntrant, 0, len(entrants))
	for _, e := range entrants {
		out = append(out, cupPlanEntrant{ClubID: e.ClubID, Origin: e.Origin})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClubID.String() < out[j].ClubID.String() })
	return out
}

// writeRegionalMemberships writes role='cup' memberships for the whole field.
// The champion and its next-best cascade entrant are exempt from the per-club
// cap (they can never be denied their entitlement); positional entrants stay
// capped (ErrCupLimit).
func (s *Service) writeRegionalMemberships(ctx context.Context, tx pgx.Tx, worldID, cupID uuid.UUID, entrants []Entrant, cap int) error {
	positional := make([]uuid.UUID, 0, len(entrants))
	for _, e := range entrants {
		if e.Origin == OriginPosition {
			positional = append(positional, e.ClubID)
		}
	}
	if len(positional) > 0 {
		var capped uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT cc.club_id
			FROM competition.club_competitions cc
			WHERE cc.world_id = $1 AND cc.role = 'cup' AND cc.competition_id <> $2
			  AND cc.club_id = ANY($3::uuid[])
			GROUP BY cc.club_id
			HAVING COUNT(*) >= $4
			LIMIT 1`, worldID, cupID, positional, cap).Scan(&capped)
		switch {
		case err == nil:
			return fmt.Errorf("%w (club %s)", ErrCupLimit, capped)
		case errors.Is(err, pgx.ErrNoRows):
			// no positional entrant at the cap
		default:
			return fmt.Errorf("cup membership cap: %w", err)
		}
	}
	for _, e := range entrants {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
			VALUES ($1, $2, $3, 'cup') ON CONFLICT (club_id, competition_id) DO NOTHING`,
			worldID, e.ClubID, cupID); err != nil {
			return fmt.Errorf("regional cup membership %s: %w", e.ClubID, err)
		}
	}
	return nil
}

// regionCampaignCountries returns the distinct countries a regional cup's
// calendar anchors to: every country whose league has a band row, plus the
// reigning champion's country when it differs.
func (s *Service) regionCampaignCountries(ctx context.Context, tx pgx.Tx, cupID uuid.UUID, reigning *Reigning) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT c.country_id
		FROM competition.cup_qualification q
		JOIN competition.competitions c ON c.id = q.league_id
		WHERE q.cup_id = $1 AND c.country_id IS NOT NULL`, cupID)
	if err != nil {
		return nil, fmt.Errorf("band country set: %w", err)
	}
	countries := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan band country: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			countries = append(countries, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if reigning != nil && reigning.LeagueID != uuid.Nil {
		var countryID *uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT country_id FROM competition.competitions
			WHERE id = $1 AND competition_type = 'league'`, reigning.LeagueID).Scan(&countryID)
		if errors.Is(err, pgx.ErrNoRows) {
			countryID = nil
		} else if err != nil {
			return nil, fmt.Errorf("champion country: %w", err)
		}
		if countryID != nil && !seen[*countryID] {
			seen[*countryID] = true
			countries = append(countries, *countryID)
		}
	}
	sort.Slice(countries, func(i, j int) bool { return countries[i].String() < countries[j].String() })
	return countries, nil
}
