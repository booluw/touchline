package competition

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
)

// IM14 — league membership administration. Both operations below are pure
// declarations: they never write standings, fixtures, or a season. They bind at
// the NEXT season composition — rollover for a league that has played, the
// first StartSeason for one that has not — and every change is audited through
// recordSeedEvent so the event log never disagrees with state.
//
// The add-cap rule (AddClubToLeague) is what makes structural over-subscription
// impossible: a league's next season is at most
//
//	oldN - streamed out + streamed in + declared adds  <=  oldN + declared adds
//
// and asking for team_count - realSize - pendingDeclared + 1 <= team_count
// bounds declared adds so composition can always reach exactly team_count.
// rolloverCountry keeps a belt-and-braces errInternalRollover check on top.

// CapacityParams is the admin's declaration for a league's next season:
// team_count and the promotion/relegation counts that will apply at rollover.
// Neighbouring counts are auto-adjusted reciprocally in the same transaction.
type CapacityParams struct {
	TeamCount   int `json:"team_count"`
	Promotions  int `json:"promotions"`
	Relegations int `json:"relegations"`
}

// ClubLeagueAdmission reports a club's admission into a league (IM14). The club
// joins in at the NEXT season composition; DefersToNextSeason is false only for
// a league that has never played, where the first StartSeason composes it.
type ClubLeagueAdmission struct {
	League             *League        `json:"league"`
	Club               apiref.ClubRef `json:"club"`
	CurrentSize        int            `json:"current_size"`
	PendingMembers     int            `json:"pending_members"`
	DefersToNextSeason bool           `json:"defers_to_next_season"`
}

// AddClubToLeague admits an existing, league-less club into a league as a
// declared member (a role='league' competition.club_competitions row). The
// club's country text is normalised to the league's country so the
// seed-status / auto-fill country checks stay coherent. The change binds at the
// next season composition and is audited as CLUB_JOINED_LEAGUE.
func (s *Service) AddClubToLeague(ctx context.Context, worldID, clubID, leagueID uuid.UUID) (*ClubLeagueAdmission, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin add-club-to-league tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&worldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	// The club must exist in the same world and be league-less: it can hold cup
	// memberships but never a role='league' row (schema-enforced one league per
	// club; the membership model is the seed-status "league-less" definition).
	var club apiref.ClubRef
	err = tx.QueryRow(ctx,
		`SELECT id, name FROM club.clubs WHERE id = $1`, clubID).Scan(&club.ID, &club.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load club: %w", err)
	}
	var clubWorld uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT world_id FROM club.clubs WHERE id = $1`, clubID).Scan(&clubWorld); err != nil {
		return nil, fmt.Errorf("load club world: %w", err)
	}
	if clubWorld != worldID {
		return nil, ErrClubWorldMismatch
	}
	var alreadyLeagued bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.club_competitions cc
			WHERE cc.club_id = $1 AND cc.role = 'league')`, clubID).Scan(&alreadyLeagued); err != nil {
		return nil, fmt.Errorf("check club league membership: %w", err)
	}
	if alreadyLeagued {
		return nil, ErrClubAlreadyInLeague
	}

	league, err := s.getLeague(ctx, tx, leagueID)
	if err != nil {
		return nil, err
	}
	if league.WorldID != worldID {
		return nil, ErrCompetitionWorldMismatch
	}

	// Add-cap: realSize(live season) + pending declared + 1 must fit team_count.
	// The +1 reserves this club's seat so rollover can always compose exactly.
	realSize, err := s.realSeasonSize(ctx, tx, leagueID, worldID)
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingDeclaredMembers(ctx, tx, leagueID)
	if err != nil {
		return nil, err
	}
	if realSize+len(pending)+1 > league.TeamCount {
		return nil, ErrLeagueFull
	}

	if _, err := tx.Exec(ctx,
		`UPDATE club.clubs SET country = $2 WHERE id = $1`, clubID, league.Country.Name); err != nil {
		return nil, fmt.Errorf("normalise club country: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
		VALUES ($1, $2, $3, 'league')`, worldID, clubID, leagueID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrClubAlreadyInLeague
		}
		return nil, fmt.Errorf("record league membership: %w", err)
	}

	hasSeason := realSize > 0 || len(pending) > 0
	if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "CLUB_JOINED_LEAGUE",
		Payload: mustJSON(map[string]any{
			"club_id":               clubID,
			"league_id":             leagueID,
			"country_id":            league.Country.ID,
			"current_size":          realSize,
			"pending_members":       len(pending) + 1,
			"defers_to_next_season": hasSeason,
		}),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit add club to league: %w", err)
	}

	freshLeague, err := s.getLeague(ctx, s.pool, leagueID)
	if err != nil {
		return nil, err
	}
	freshPending, err := s.pendingDeclaredMembers(ctx, s.pool, leagueID)
	if err != nil {
		return nil, err
	}
	freshReal, err := s.realSeasonSize(ctx, s.pool, leagueID, worldID)
	if err != nil {
		return nil, err
	}
	return &ClubLeagueAdmission{
		League:             freshLeague,
		Club:               club,
		CurrentSize:        freshReal,
		PendingMembers:     len(freshPending),
		DefersToNextSeason: freshReal > 0 || len(freshPending) > 0,
	}, nil
}

// SetLeagueCapacity raises a league's team_count and declares the
// promotions/relegations that will apply at the next rollover (upward-only).
// The reciprocal counts on the league's neighbours are auto-adjusted in the
// same transaction — per-league edits can never satisfy the ladder's adjacency
// symmetry alone — and the whole country ladder is re-validated (util.go)
// before committing; a call that would leave the ladder incoherent rolls back
// with ErrAdjacencyMismatch. Audited as LEAGUE_CAPACITY_CHANGED.
func (s *Service) SetLeagueCapacity(ctx context.Context, leagueID uuid.UUID, p CapacityParams) (*League, error) {
	if p.TeamCount < 4 || p.TeamCount%2 != 0 {
		return nil, ErrInvalidTeamCount
	}
	if p.Promotions < 0 || p.Relegations < 0 || p.Promotions+p.Relegations >= p.TeamCount {
		return nil, ErrInvalidCounts
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin set-capacity tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	league, err := s.getLeague(ctx, tx, leagueID)
	if err != nil {
		return nil, err
	}
	if p.TeamCount < league.TeamCount {
		return nil, ErrLeagueShrink
	}

	// Lock the country's whole ladder in deterministic order so concurrent
	// capacity edits serialize instead of deadlocking on shared neighbour rows.
	if _, err := tx.Exec(ctx, `
		SELECT r.competition_id
		FROM competition.competition_rules r
		JOIN competition.competitions c ON c.id = r.competition_id
		WHERE c.country_id = $1 AND c.competition_type = 'league'
		ORDER BY c.tier, c.name
		FOR UPDATE OF r`, league.Country.ID); err != nil {
		return nil, fmt.Errorf("lock country ladder: %w", err)
	}

	leagues, err := s.leaguesByCountry(ctx, tx, league.Country.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*League, len(leagues))
	for i := range leagues {
		byID[leagues[i].ID] = &leagues[i]
	}
	target := byID[league.ID]
	if target == nil {
		return nil, ErrCompetitionNotFound
	}

	// A positive movement count requires the matching link; the neighbour's
	// reciprocal count is then derived, never left to drift (recorded decision:
	// mutual auto-adjust means one call moves both sides of every edge).
	if p.Promotions > 0 && target.PromotesTo == nil {
		return nil, ErrAdjacencyMismatch
	}
	if p.Relegations > 0 && target.RelegatesTo == nil {
		return nil, ErrAdjacencyMismatch
	}
	adjusted := map[uuid.UUID]capacityAdjust{}
	if target.PromotesTo != nil {
		above, ok := byID[target.PromotesTo.ID]
		if !ok {
			return nil, ErrBadAdjacency
		}
		if above.Relegations != p.Promotions {
			adjusted[above.ID] = capacityAdjust{Field: "relegations", Old: above.Relegations, New: p.Promotions}
			above.Relegations = p.Promotions
		}
	}
	if target.RelegatesTo != nil {
		below, ok := byID[target.RelegatesTo.ID]
		if !ok {
			return nil, ErrBadAdjacency
		}
		if below.Promotions != p.Relegations {
			adjusted[below.ID] = capacityAdjust{Field: "promotions", Old: below.Promotions, New: p.Relegations}
			below.Promotions = p.Relegations
		}
	}

	// Belt-and-braces: the whole ladder must now be coherent, or the change
	// would leave a dangling edge (e.g. a neighbour that relegates elsewhere).
	if err := validateAdjacency(leagues); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE competition.competitions SET team_count = $2
		WHERE id = $1 AND competition_type = 'league'`, leagueID, p.TeamCount); err != nil {
		return nil, fmt.Errorf("update league team_count: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_rules
		SET promotions = $2, relegations = $3
		WHERE competition_id = $1`, leagueID, p.Promotions, p.Relegations); err != nil {
		return nil, fmt.Errorf("update league counts: %w", err)
	}
	for adjLeagueID, a := range adjusted {
		var col string
		if a.Field == "promotions" {
			col = "promotions"
		} else {
			col = "relegations"
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`UPDATE competition.competition_rules SET %s = $2 WHERE competition_id = $1`, col),
			adjLeagueID, a.New); err != nil {
			return nil, fmt.Errorf("auto-adjust %s: %w", a.Field, err)
		}
	}

	adjustPayload := []map[string]any{}
	for id, a := range adjusted {
		adjustPayload = append(adjustPayload, map[string]any{
			"league_id": id,
			"field":     a.Field,
			"old":       a.Old,
			"new":       a.New,
		})
	}
	if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   league.WorldID,
		EventType: "LEAGUE_CAPACITY_CHANGED",
		Payload: mustJSON(map[string]any{
			"league_id":        leagueID,
			"country_id":       league.Country.ID,
			"old_team_count":   league.TeamCount,
			"new_team_count":   p.TeamCount,
			"promotions":       p.Promotions,
			"relegations":      p.Relegations,
			"reciprocal_edits": adjustPayload,
		}),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit set capacity: %w", err)
	}
	return s.getLeague(ctx, s.pool, leagueID)
}

// capacityAdjust records one reciprocal edit applied to a neighbour league.
type capacityAdjust struct {
	Field string
	Old   int
	New   int
}

// composeLeagueEntries resolves a league's next-season entry set at rollover
// (IM14): standings composition (stayers + promoted-in + relegated-in) is the
// base, declared-but-unseated members are merged next, then remaining seats are
// auto-filled from the country's league-less pool and finally from freshly
// generated AI clubs (mirroring SeedWorld's membership writer). It composes to
// exactly l.TeamCount or the rollover fails as an internal error.
func (s *Service) composeLeagueEntries(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, l *League, standings []uuid.UUID, seated map[uuid.UUID]bool) ([]uuid.UUID, error) {
	entries := append([]uuid.UUID(nil), standings...)
	seen := make(map[uuid.UUID]bool, len(entries))
	for _, id := range entries {
		seen[id] = true
	}

	// Declared members that are not seated anywhere (a promoted/relegated club
	// keeps its original role='league' row forever, so the standings seat wins).
	pending, err := s.pendingDeclaredMembers(ctx, tx, l.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range pending {
		if seen[id] || seated[id] {
			continue
		}
		entries = append(entries, id)
		seen[id] = true
	}

	if len(entries) > l.TeamCount {
		return nil, fmt.Errorf("%w: %s next season has %d entries, want %d",
			errInternalRollover, l.Name, len(entries), l.TeamCount)
	}

	if need := l.TeamCount - len(entries); need > 0 {
		candidates, err := s.clubLeagueLessCandidates(ctx, tx, worldID, l.Country.Name)
		if err != nil {
			return nil, err
		}
		for _, id := range candidates {
			if need == 0 {
				break
			}
			if seen[id] || seated[id] {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
				VALUES ($1, $2, $3, 'league')`, worldID, id, l.ID); err != nil {
				return nil, fmt.Errorf("declare auto-filled member for %s: %w", l.Name, err)
			}
			entries = append(entries, id)
			seen[id] = true
			need--
		}
	}

	if need := l.TeamCount - len(entries); need > 0 {
		created, err := s.generateAILeagueMembers(ctx, tx, worldID, l, need)
		if err != nil {
			return nil, err
		}
		entries = append(entries, created...)
	}

	if len(entries) != l.TeamCount {
		return nil, fmt.Errorf("%w: %s next season has %d entries after fill, want %d",
			errInternalRollover, l.Name, len(entries), l.TeamCount)
	}
	return entries, nil
}

// generateAILeagueMembers creates `need` fresh AI clubs for a league and records
// their role='league' memberships, mirroring SeedWorld's per-league membership
// loop (deterministic rng from seed ⊕ leagueID, same name streams, pool
// replenished after each draft so the market never empties).
func (s *Service) generateAILeagueMembers(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, l *League, need int) ([]uuid.UUID, error) {
	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return nil, fmt.Errorf("load world seed: %w", err)
	}
	var worldRef time.Time
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1`, worldID).Scan(&worldRef); err != nil {
		return nil, fmt.Errorf("load world ref: %w", err)
	}
	ref := daysTruncate(worldRef)

	pools, err := bootstrap.LoadClubNamePools(ctx, tx)
	if err != nil {
		return nil, err
	}
	pool := pools[l.Country.Code]
	if len(pool.Stems) == 0 || len(pool.Suffixes) == 0 {
		pool = pools[""]
	}

	used := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT name FROM club.clubs WHERE world_id = $1`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load existing club names: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan club name: %w", err)
		}
		used[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate club names: %w", err)
	}

	poolFactory, err := squadFactory(ctx, tx, hashMix(seed, worldID))
	if err != nil {
		return nil, err
	}

	lrng := rand.New(rand.NewSource(hashMix(seed, l.ID)))
	created := make([]uuid.UUID, 0, need)
	for i := 0; i < need; i++ {
		name := nextClubName(lrng, pool.Stems, pool.Suffixes, used)
		generated, err := bootstrap.GenerateAIClub(ctx, s.bus, tx, worldID, name, short(name), l.Country.Name, &l.Country.ID)
		if err != nil {
			return nil, fmt.Errorf("generate AI club for %s: %w", l.Name, err)
		}
		used[generated.ClubName] = true
		created = append(created, generated.ClubID)

		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
			VALUES ($1, $2, $3, 'league')`, worldID, generated.ClubID, l.ID); err != nil {
			return nil, fmt.Errorf("record league membership for %s: %w", l.Name, err)
		}

		if err := playerpool.ReplenishPool(ctx, tx, s.bus, worldID, &l.Country.ID, playerpool.PoolTargetSize, poolFactory, ref); err != nil {
			return nil, fmt.Errorf("replenish country pool: %w", err)
		}
	}
	return created, nil
}

// queryRunner is the query surface the IM14 membership reads share across the
// pool and an open transaction.
type queryRunner interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// realSeasonSize is the entry count of the league's live (non-completed) season
// — the "playing now" roster an admin's capacity math is measured against. It
// is 0 for a league that has never played.
func (s *Service) realSeasonSize(ctx context.Context, q queryRunner, leagueID, worldID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.competition_entries e
		JOIN competition.seasons s ON s.id = e.season_id
		WHERE s.competition_id = $1 AND s.world_id = $2 AND s.status <> 'completed'`,
		leagueID, worldID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("real season size: %w", err)
	}
	return n, nil
}

// pendingDeclaredMembers returns the league's declared members that are not
// part of any live season: clubs with a role='league' membership in this league
// who have not yet played (or who have moved on via promotion/relegation and
// now play elsewhere). They are the members an admin's add waits for.
func (s *Service) pendingDeclaredMembers(ctx context.Context, q queryRunner, leagueID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT cc.club_id
		FROM competition.club_competitions cc
		WHERE cc.competition_id = $1 AND cc.role = 'league'
		  AND NOT EXISTS (
		      SELECT 1 FROM competition.competition_entries e
		      JOIN competition.seasons s ON s.id = e.season_id
		      WHERE s.competition_id = $1
		        AND s.status <> 'completed'
		        AND e.club_id = cc.club_id)
		  AND NOT EXISTS (
		      SELECT 1 FROM competition.competition_entries e
		      JOIN competition.seasons s ON s.id = e.season_id
		      WHERE s.competition_id <> $1
		        AND s.status <> 'completed'
		        AND e.club_id = cc.club_id)
		ORDER BY cc.joined_at, cc.club_id`, leagueID)
	if err != nil {
		return nil, fmt.Errorf("pending declared members: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending member: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// clubLeagueLessCandidates is the auto-fill pool: the country's clubs with no
// role='league' membership anywhere (country matched on the normalized text
// name, so an AddClubToLeague normalization stays findable). Ordered by name so
// the fill is deterministic.
func (s *Service) clubLeagueLessCandidates(ctx context.Context, q queryRunner, worldID uuid.UUID, countryName string) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id FROM club.clubs c
		WHERE c.world_id = $1
		  AND lower(c.country) = lower($2)
		  AND NOT EXISTS (
		      SELECT 1 FROM competition.club_competitions cc
		      WHERE cc.club_id = c.id AND cc.role = 'league')
		ORDER BY c.name, c.id`, worldID, countryName)
	if err != nil {
		return nil, fmt.Errorf("league-less club candidates: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan league-less candidate: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
