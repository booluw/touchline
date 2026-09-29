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

	"github.com/touchline/backend/pkg/eventbus"
)

// ---------------------------------------------------------------------------
// Campaign
// ---------------------------------------------------------------------------

// StartCupCampaign materializes a cup's first campaign: the season row, cup
// memberships for every eligible club, competition_entries, the campaign plan,
// and the Round-1 bracket/fixtures. Mirrors StartSeason: exactly one campaign
// per cup at a time.
func (s *Service) StartCupCampaign(ctx context.Context, worldID, countryID, cupID uuid.UUID) (*Season, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cup campaign: %w", err)
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
	if cup.Country.ID != countryID {
		return nil, ErrCompetitionWorldMismatch
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

	field, tierOne, err := countryField(ctx, tx, worldID, countryID)
	if err != nil {
		return nil, err
	}

	topN, err := topTierOneClubs(ctx, s, tx, worldID, countryID, tierOne)
	if err != nil {
		return nil, err
	}
	if cup.FirstTierBye > 0 && len(topN) < cup.FirstTierBye {
		return nil, ErrStagingInvalid
	}

	bottom := field
	if cup.FirstTierBye > 0 {
		keep := map[uuid.UUID]bool{}
		for _, id := range topN[:cup.FirstTierBye] {
			keep[id] = true
		}
		bottom = make([]uuid.UUID, 0, len(field)-cup.FirstTierBye)
		for _, id := range field {
			if !keep[id] {
				bottom = append(bottom, id)
			}
		}
	}
	if len(bottom) < cup.SurvivorThreshold {
		return nil, ErrStagingInvalid
	}

	ladder, err := cupLadder(len(bottom), cup.SurvivorThreshold, cup.FirstTierBye)
	if err != nil {
		return nil, err
	}

	// Membership cap (migration 0037): at most 3 role='cup' memberships per
	// club across the world.
	var capped uuid.UUID
	cappedErr := tx.QueryRow(ctx, `
		SELECT cc.club_id
		FROM competition.club_competitions cc
		WHERE cc.world_id = $1 AND cc.role = 'cup' AND cc.competition_id <> $2
		  AND cc.club_id = ANY($3::uuid[])
		GROUP BY cc.club_id
		HAVING COUNT(*) >= $4
		LIMIT 1`, worldID, cupID, field, cupMaxMemberships).Scan(&capped)
	switch {
	case cappedErr == nil:
		return nil, fmt.Errorf("%w (club %s)", ErrCupLimit, capped)
	case errors.Is(cappedErr, pgx.ErrNoRows):
		// no club at the cap
	default:
		return nil, fmt.Errorf("cup membership cap: %w", cappedErr)
	}

	for _, clubID := range field {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
			VALUES ($1, $2, $3, 'cup') ON CONFLICT (club_id, competition_id) DO NOTHING`,
			worldID, clubID, cupID); err != nil {
			return nil, fmt.Errorf("cup membership %s: %w", clubID, err)
		}
	}

	season, err := s.createSeason(ctx, tx, worldID, cupID, worldRef, field)
	if err != nil {
		return nil, err
	}

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return nil, fmt.Errorf("load world seed: %w", err)
	}
	anchorDays, err := s.countryLeagueDays(ctx, tx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	ladder, err = s.planCupCalendar(ctx, tx, worldID, anchorDays, cupID, seed, ladder)
	if err != nil {
		return nil, err
	}

	// Round-1 participants: the bottom pool, or — when the pool is already
	// exactly X strong (F == X) — the late entrants join at Round 1 itself.
	participants := bottom
	if cup.FirstTierBye > 0 && len(bottom) == cup.SurvivorThreshold {
		participants = append(append([]uuid.UUID(nil), bottom...), topN[:cup.FirstTierBye]...)
	}
	planDoc := cupPlan{
		Total:  len(ladder),
		Ladder: ladder,
	}
	if cup.FirstTierBye > 0 {
		planDoc.LateEntry = 1
		for _, rp := range ladder {
			if rp.N == cup.FirstTierBye+cup.SurvivorThreshold {
				planDoc.LateEntry = rp.Round
				break
			}
		}
		planDoc.TopNClubIDs = append([]uuid.UUID(nil), topN[:cup.FirstTierBye]...)
		// F == X (the bottom pool is already exactly X strong): the late
		// entrants are in from Round 1, so the plan must not join them again
		// when applyKnockoutResult reaches LateEntry.
		if len(bottom) == cup.SurvivorThreshold {
			planDoc.Joined = true
		}
	}
	pb, err := json.Marshal(planDoc)
	if err != nil {
		return nil, fmt.Errorf("marshal campaign plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_rules
		SET qualification_rules = jsonb_set(
			COALESCE(qualification_rules, '{}'::jsonb), '{campaign}', $2::jsonb)
		WHERE competition_id = $1`, cupID, pb); err != nil {
		return nil, fmt.Errorf("persist campaign plan: %w", err)
	}

	if err := s.materializeRound(ctx, tx, worldID, cupID, season.ID, ladder[0], participants, worldRef, seed); err != nil {
		return nil, err
	}

	if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "CUP_CAMPAIGN_STARTED",
		Payload: mustJSON(map[string]any{
			"competition_id":     cupID,
			"season_id":          season.ID,
			"country_id":         countryID,
			"team_count":         len(field),
			"late_entry_teams":   cup.FirstTierBye,
			"survivor_threshold": cup.SurvivorThreshold,
			"rounds":             len(ladder),
		}),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cup campaign: %w", err)
	}
	return season, nil
}

// countryField loads every club with a role='league' membership in the country
// (all tiers), and separately the tier-1 memberships (club id → league id).
func countryField(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT cc.club_id
		FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id
		WHERE cc.world_id = $1 AND cc.role = 'league' AND l.country_id = $2
		ORDER BY cc.club_id`, worldID, countryID)
	if err != nil {
		return nil, nil, fmt.Errorf("country field: %w", err)
	}
	defer rows.Close()
	field := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, nil, fmt.Errorf("scan field: %w", err)
		}
		field = append(field, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	tRows, err := tx.Query(ctx, `
		SELECT cc.club_id, l.id FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id AND l.tier = 1
		WHERE cc.world_id = $1 AND l.country_id = $2`, worldID, countryID)
	if err != nil {
		return nil, nil, fmt.Errorf("tier one: %w", err)
	}
	defer tRows.Close()
	tierOne := map[uuid.UUID]uuid.UUID{}
	for tRows.Next() {
		var clubID, leagueID uuid.UUID
		if err := tRows.Scan(&clubID, &leagueID); err != nil {
			return nil, nil, fmt.Errorf("scan tier one: %w", err)
		}
		tierOne[clubID] = leagueID
	}
	return field, tierOne, tRows.Err()
}

// topTierOneClubs ranks the tier-1 clubs by most-recent standings
// (points, GD, GF, name); with no tier-1 results at all it falls back to the
// deterministic club order. Returns the full ordered list (the campaign caller
// takes the first N).
func topTierOneClubs(ctx context.Context, s *Service, tx pgx.Tx, worldID, countryID uuid.UUID, tierOne map[uuid.UUID]uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(tierOne))
	if len(tierOne) == 0 {
		return out, nil
	}
	leagueIDs := make([]uuid.UUID, 0, len(tierOne))
	seen := map[uuid.UUID]bool{}
	for _, leagueID := range tierOne {
		if !seen[leagueID] {
			seen[leagueID] = true
			leagueIDs = append(leagueIDs, leagueID)
		}
	}

	type ranked struct {
		club        uuid.UUID
		pts, gd, gf int
		name        string
		has         bool
	}
	rows, err := tx.Query(ctx, `
		SELECT cc.club_id, COALESCE(st.points, 0), COALESCE(st.goals_for - st.goals_against, 0),
		       COALESCE(st.goals_for, 0), cl.name, (st.points IS NOT NULL)
		FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id AND l.tier = 1
		JOIN club.clubs cl ON cl.id = cc.club_id
		LEFT JOIN competition.seasons se ON se.competition_id = l.id AND se.world_id = $1 AND se.status = 'in_progress'
		LEFT JOIN competition.standings st ON st.season_id = se.id AND st.club_id = cc.club_id
		WHERE cc.world_id = $1 AND l.country_id = $2 AND cc.competition_id = ANY($3::uuid[])
		ORDER BY cc.club_id`, worldID, countryID, leagueIDs)
	if err != nil {
		return nil, fmt.Errorf("tier one standings: %w", err)
	}
	list := []ranked{}
	anyStandings := false
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.club, &r.pts, &r.gd, &r.gf, &r.name, &r.has); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan tier one ranking: %w", err)
		}
		list = append(list, r)
		if r.has {
			anyStandings = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if !anyStandings {
		clubs := make([]uuid.UUID, 0, len(tierOne))
		for clubID := range tierOne {
			clubs = append(clubs, clubID)
		}
		sort.Slice(clubs, func(i, j int) bool { return clubs[i].String() < clubs[j].String() })
		return clubs, nil
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.has != b.has {
			return a.has
		}
		if a.pts != b.pts {
			return a.pts > b.pts
		}
		if a.gd != b.gd {
			return a.gd > b.gd
		}
		if a.gf != b.gf {
			return a.gf > b.gf
		}
		return a.name < b.name
	})
	for _, r := range list {
		out = append(out, r.club)
	}
	return out, nil
}
