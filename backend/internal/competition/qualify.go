package competition

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Origin labels carried by every Field entrant (IM07). The preview (IM08) and
// resolution news (IM09) surface these to say *why* a club is in the field.
const (
	OriginPosition          = "position"
	OriginChampionDirect    = "champion_direct"
	OriginChampionNextBest  = "champion_next_best"
	OriginChampionOutOfBand = "champion_out_of_band"
)

// CompetitionScope is the geographic scope a cup draws its field from.
type CompetitionScope string

const (
	ScopeCountry CompetitionScope = "country"
	ScopeRegion  CompetitionScope = "region"
)

// Band is one competition.cup_qualification row: the league's final-table
// positions `from..to` that enter the cup. To == nil means through last place.
type Band struct {
	LeagueID uuid.UUID
	From     int
	To       *int
}

// Table is a league's most recent completed season, ranked exactly as the
// final standings: points DESC, goal difference DESC, goals for DESC, name.
type Table struct {
	LeagueID     uuid.UUID
	SeasonNumber int
	Ranks        []uuid.UUID
}

// Reigning is the champion of a cup's most recent completed campaign together
// with the league the club currently belongs to (club_competitions
// role='league'). LeagueID is zero when the champion's membership is unknown —
// the engine then treats the champion as direct.
type Reigning struct {
	ClubID   uuid.UUID
	LeagueID uuid.UUID
}

// SiblingCup is another continental cup in the world whose projected field is
// compared against the computed one for overlap conflicts (depth one; sibling
// fields are never themselves conflict-resolved).
type SiblingCup struct {
	Ref   CompetitionRef
	Bands []Band
}

// CompetitionRef identifies a cup for the engine: world, scope, and the soft
// tier (a label; only IM09's precedence sweep compares tiers).
type CompetitionRef struct {
	ID        uuid.UUID
	WorldID   uuid.UUID
	CountryID uuid.UUID
	RegionID  uuid.UUID
	Name      string
	Type      string
	Tier      int
	Scope     CompetitionScope
}

// Entrant is one club in the computed field with the reason it enters and its
// league-table rank (0 for a direct champion).
type Entrant struct {
	ClubID uuid.UUID
	Origin string
	Rank   int
}

// Conflict flags a club that also appears in another continental cup's field.
type Conflict struct {
	OtherCupID uuid.UUID
	Tier       int
}

// UnavailableLeague is a banded league with no completed season: the row
// cannot qualify anyone until one exists. IM08's campaign start promotes the
// presence of these to ErrQualificationUnavailable (422); the preview renders
// them as warnings.
type UnavailableLeague struct {
	LeagueID uuid.UUID
	CupID    uuid.UUID
}

// Field is the entitlement-maximal projected field. It never drops a champion
// — resolving double bookings is IM09's sweep.
type Field struct {
	Cup         CompetitionRef
	ClubCount   int
	Entrants    []Entrant
	Conflicts   map[uuid.UUID][]Conflict
	Unavailable []UnavailableLeague
}

// QualifyField is the engine input: the cup and its cup_qualification rows.
type QualifyField struct {
	Cup   CompetitionRef
	Bands []Band
}

// StandingsProvider returns a league's most recent COMPLETED-season table. A
// nil table with a nil error means the league has no completed season yet. The
// engine never falls back to an in-progress table.
type StandingsProvider interface {
	LastCompletedStandings(ctx context.Context, leagueID uuid.UUID) (*Table, error)
}

// ReigningChampionProvider returns the champion of the cup's most recent
// completed campaign (nil when the cup has never completed one).
type ReigningChampionProvider interface {
	ReigningChampion(ctx context.Context, cupID uuid.UUID) (*Reigning, error)
}

// SiblingCupProvider lists the world's other continental cups (with their
// qualification bands) for conflict detection.
type SiblingCupProvider interface {
	SiblingCups(ctx context.Context, worldID, cupID uuid.UUID) ([]SiblingCup, error)
}

// ComputeField is the IM07 qualification engine's pure core. Given a cup and
// its cup_qualification bands it computes the exact field from the last
// completed season per banded league, applies the reigning-champion +1, and
// flags cross-cup overlaps for IM09. It writes nothing and never reads the
// wall clock: identical inputs + identical provider answers ⇒ identical fields.
func ComputeField(ctx context.Context, in QualifyField, st StandingsProvider, rp ReigningChampionProvider, cp SiblingCupProvider) (*Field, error) {
	bands := sortedBands(in.Bands)
	for _, b := range bands {
		if err := validateQualBand(b); err != nil {
			return nil, err
		}
	}

	// Load each banded league's most recent completed table in band order;
	// leagues with none are recorded as unavailable, never guessed at.
	field := &Field{Cup: in.Cup, Conflicts: map[uuid.UUID][]Conflict{}}
	tables := map[uuid.UUID]*Table{}
	for _, lid := range uniqueLeagueIDs(bands) {
		t, err := st.LastCompletedStandings(ctx, lid)
		if err != nil {
			return nil, fmt.Errorf("last completed standings for %s: %w", lid, err)
		}
		if t == nil {
			field.Unavailable = append(field.Unavailable, UnavailableLeague{LeagueID: lid, CupID: in.Cup.ID})
			continue
		}
		tables[lid] = t
	}

	reigning, err := rp.ReigningChampion(ctx, in.Cup.ID)
	if err != nil {
		return nil, fmt.Errorf("reigning champion of %s: %w", in.Cup.ID, err)
	}

	entrants, _ := coreEntrants(tables, bands, reigning)

	// Conflict flags, one level deep: only the primary cup's field is compared
	// against each sibling's (itself computed with the shared league tables).
	if len(entrants) > 0 {
		siblings, err := cp.SiblingCups(ctx, in.Cup.WorldID, in.Cup.ID)
		if err != nil {
			return nil, fmt.Errorf("sibling cups: %w", err)
		}
		for _, sib := range siblings {
			sibChamp, err := rp.ReigningChampion(ctx, sib.Ref.ID)
			if err != nil {
				return nil, fmt.Errorf("reigning champion of sibling %s: %w", sib.Ref.ID, err)
			}
			sibEntrants, _ := coreEntrants(tables, sortedBands(sib.Bands), sibChamp)
			overlap := map[uuid.UUID]bool{}
			for _, e := range sibEntrants {
				overlap[e.ClubID] = true
			}
			if len(overlap) == 0 {
				continue
			}
			for _, e := range entrants {
				if overlap[e.ClubID] {
					field.Conflicts[e.ClubID] = append(field.Conflicts[e.ClubID],
						Conflict{OtherCupID: sib.Ref.ID, Tier: sib.Ref.Tier})
				}
			}
		}
		for cl, list := range field.Conflicts {
			field.Conflicts[cl] = sortedConflicts(list)
		}
	}

	if len(entrants) < 2 {
		return nil, fmt.Errorf("%w (cup %s)", ErrQualificationField, in.Cup.ID)
	}
	field.Entrants = entrants
	field.ClubCount = len(entrants)
	return field, nil
}

// coreEntrants resolves the band cuts plus one champion case against the given
// tables. It is reused for sibling fields (conflicts disabled by construction).
func coreEntrants(tables map[uuid.UUID]*Table, bands []Band, reigning *Reigning) ([]Entrant, map[uuid.UUID]bool) {
	out := []Entrant{}
	clubSet := map[uuid.UUID]bool{}
	add := func(clubID uuid.UUID, origin string, rank int) {
		if clubSet[clubID] {
			return
		}
		clubSet[clubID] = true
		out = append(out, Entrant{ClubID: clubID, Origin: origin, Rank: rank})
	}

	for _, b := range bands {
		t := tables[b.LeagueID]
		if t == nil {
			continue
		}
		for i, clubID := range bandRanks(t.Ranks, b) {
			add(clubID, OriginPosition, b.From+i)
		}
	}

	if reigning == nil {
		return out, clubSet
	}

	bandIdx := -1
	for i, b := range bands {
		if b.LeagueID == reigning.LeagueID {
			bandIdx = i
			break
		}
	}
	// Champion's league has no band row — or its table is unavailable — so the
	// champion enters directly; the unavailable league is still reported.
	if bandIdx < 0 {
		add(reigning.ClubID, OriginChampionDirect, 0)
		return out, clubSet
	}

	t := tables[reigning.LeagueID]
	if t == nil {
		add(reigning.ClubID, OriginChampionDirect, 0)
		return out, clubSet
	}

	rank := positionOf(t.Ranks, reigning.ClubID)
	if rank == 0 {
		// Defensive: the champion is missing from its league's completed
		// table; the entitlement is preserved by treating it as outside its
		// band (it still enters on top of the band).
		add(reigning.ClubID, OriginChampionOutOfBand, 0)
		return out, clubSet
	}

	toEff := effectiveTo(t.Ranks, bands[bandIdx])
	if rank >= bands[bandIdx].From && rank <= toEff {
		// In-band: the band cut already ships the champion, so the +1 is the
		// next-best club (highest rank past the cut, skipping any entrant).
		for i := toEff; i < len(t.Ranks); i++ {
			if clubSet[t.Ranks[i]] {
				continue
			}
			add(t.Ranks[i], OriginChampionNextBest, i+1)
			break
		}
		return out, clubSet
	}

	add(reigning.ClubID, OriginChampionOutOfBand, rank)
	return out, clubSet
}

// validateQualBand is the service-level glue mirroring the 0052 row CHECKs
// (from_position >= 1; to_position == NULL | to_position >= from_position).
func validateQualBand(b Band) error {
	if b.LeagueID == uuid.Nil {
		return fmt.Errorf("qualification band has no league")
	}
	if b.From < 1 {
		return fmt.Errorf("qualification band from_position must be >= 1 (got %d)", b.From)
	}
	if b.To != nil && *b.To < b.From {
		return fmt.Errorf("qualification band to_position (got %d) must be >= from_position (%d)", *b.To, b.From)
	}
	return nil
}

// bandRanks cuts a league's ranked table to a band (1-based from..to; to nil
// means through last place). A band beyond the table length yields an empty
// set, which is a warning the preview surfaces — never an error.
func bandRanks(ranks []uuid.UUID, b Band) []uuid.UUID {
	lo := b.From - 1
	hi := effectiveTo(ranks, b)
	if lo >= hi {
		return nil
	}
	return ranks[lo:hi]
}

// effectiveTo is the band's 1-based inclusive upper bound clamped to the
// table length.
func effectiveTo(ranks []uuid.UUID, b Band) int {
	hi := len(ranks)
	if b.To != nil && *b.To < hi {
		hi = *b.To
	}
	return hi
}

// positionOf returns the 1-based rank of a club in a table, 0 when absent.
func positionOf(ranks []uuid.UUID, clubID uuid.UUID) int {
	for i, id := range ranks {
		if id == clubID {
			return i + 1
		}
	}
	return 0
}

// sortedBands orders bands by (league, from) so the field is independent of
// the row insertion order.
func sortedBands(bands []Band) []Band {
	out := append([]Band(nil), bands...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].LeagueID != out[j].LeagueID {
			return out[i].LeagueID.String() < out[j].LeagueID.String()
		}
		return out[i].From < out[j].From
	})
	return out
}

// uniqueLeagueIDs returns the distinct banded league ids in band order.
func uniqueLeagueIDs(bands []Band) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(bands))
	seen := map[uuid.UUID]bool{}
	for _, b := range bands {
		if seen[b.LeagueID] {
			continue
		}
		seen[b.LeagueID] = true
		out = append(out, b.LeagueID)
	}
	return out
}

// sortedConflicts sorts a club's conflict list then dedupes it.
func sortedConflicts(list []Conflict) []Conflict {
	out := append([]Conflict(nil), list...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].OtherCupID != out[j].OtherCupID {
			return out[i].OtherCupID.String() < out[j].OtherCupID.String()
		}
		return out[i].Tier < out[j].Tier
	})
	deduped := out[:0]
	for i, c := range out {
		if i > 0 && c == out[i-1] {
			continue
		}
		deduped = append(deduped, c)
	}
	return deduped
}

// ---------------------------------------------------------------------------
// Pool-backed providers (also used by IM08/IM09 and the integration tests)
// ---------------------------------------------------------------------------

// LastCompletedStandings implements StandingsProvider over the real pool: the
// most recent completed season's final table in the canonical tie-break order.
func (s *Service) LastCompletedStandings(ctx context.Context, leagueID uuid.UUID) (*Table, error) {
	var seasonID uuid.UUID
	var number int
	err := s.pool.QueryRow(ctx, `
		SELECT id, season_number FROM competition.seasons
		WHERE competition_id = $1 AND status = 'completed'
		ORDER BY season_number DESC LIMIT 1`, leagueID).Scan(&seasonID, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("completed season of %s: %w", leagueID, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id
		FROM competition.standings st
		JOIN club.clubs c ON c.id = st.club_id
		WHERE st.season_id = $1
		ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC, st.goals_for DESC, c.name`,
		seasonID)
	if err != nil {
		return nil, fmt.Errorf("standings of %s: %w", leagueID, err)
	}
	defer rows.Close()
	t := &Table{LeagueID: leagueID, SeasonNumber: number}
	for rows.Next() {
		var clubID uuid.UUID
		if err := rows.Scan(&clubID); err != nil {
			return nil, fmt.Errorf("scan rank of %s: %w", leagueID, err)
		}
		t.Ranks = append(t.Ranks, clubID)
	}
	return t, rows.Err()
}

// ReigningChampion implements ReigningChampionProvider over the real pool: the
// champion entry of the cup's most recent completed campaign, plus the club's
// current league membership.
func (s *Service) ReigningChampion(ctx context.Context, cupID uuid.UUID) (*Reigning, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT e.club_id
		FROM competition.seasons cse
		JOIN competition.competition_entries e ON e.season_id = cse.id
		WHERE cse.competition_id = $1 AND cse.status = 'completed' AND e.status = 'champion'
		ORDER BY cse.season_number DESC LIMIT 1`, cupID).Scan(&clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reigning champion of %s: %w", cupID, err)
	}

	var leagueID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT competition_id FROM competition.club_competitions
		WHERE club_id = $1 AND role = 'league' LIMIT 1`, clubID).Scan(&leagueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Reigning{ClubID: clubID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("league membership of %s: %w", clubID, err)
	}
	return &Reigning{ClubID: clubID, LeagueID: leagueID}, nil
}

// SiblingCups implements SiblingCupProvider over the real pool: every other
// continental cup in the world with its qualification bands.
func (s *Service) SiblingCups(ctx context.Context, worldID, cupID uuid.UUID) ([]SiblingCup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.country_id, c.region_id, c.name, c.competition_type, COALESCE(c.tier, 0)
		FROM competition.competitions c
		WHERE c.world_id = $1 AND c.competition_type = 'continental' AND c.id <> $2
		ORDER BY c.id`, worldID, cupID)
	if err != nil {
		return nil, fmt.Errorf("sibling cups: %w", err)
	}
	defer rows.Close()

	out := []SiblingCup{}
	sibByID := map[uuid.UUID]int{}
	for rows.Next() {
		var ref CompetitionRef
		var countryID, regionID *uuid.UUID
		if err := rows.Scan(&ref.ID, &countryID, &regionID, &ref.Name, &ref.Type, &ref.Tier); err != nil {
			return nil, fmt.Errorf("scan sibling cup: %w", err)
		}
		ref.WorldID = worldID
		if regionID != nil {
			ref.Scope = ScopeRegion
			ref.RegionID = *regionID
		} else {
			ref.Scope = ScopeCountry
			if countryID != nil {
				ref.CountryID = *countryID
			}
		}
		sibByID[ref.ID] = len(out)
		out = append(out, SiblingCup{Ref: ref})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	bandRows, err := s.pool.Query(ctx, `
		SELECT cup_id, league_id, from_position, to_position
		FROM competition.cup_qualification
		WHERE cup_id = ANY($1::uuid[])
		ORDER BY cup_id, league_id, from_position`, siblingIDs(out))
	if err != nil {
		return nil, fmt.Errorf("sibling qualification bands: %w", err)
	}
	defer bandRows.Close()
	for bandRows.Next() {
		var cupIDRow uuid.UUID
		var b Band
		if err := bandRows.Scan(&cupIDRow, &b.LeagueID, &b.From, &b.To); err != nil {
			return nil, fmt.Errorf("scan sibling band: %w", err)
		}
		if idx, ok := sibByID[cupIDRow]; ok {
			out[idx].Bands = append(out[idx].Bands, b)
		}
	}
	return out, bandRows.Err()
}

func siblingIDs(cups []SiblingCup) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(cups))
	for _, c := range cups {
		out = append(out, c.Ref.ID)
	}
	return out
}

// ComputeCupField runs the IM07 engine for one cup through the real pool:
// loads the cup's scope + qualification bands, then ComputeField with this
// service as all three providers. Writes nothing; used by IM08/IM09 entry
// points and the tests.
func (s *Service) ComputeCupField(ctx context.Context, cupID uuid.UUID) (*Field, error) {
	ref, err := s.qualCupRef(ctx, cupID)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT league_id, from_position, to_position
		FROM competition.cup_qualification WHERE cup_id = $1
		ORDER BY league_id, from_position`, cupID)
	if err != nil {
		return nil, fmt.Errorf("qualification bands of %s: %w", cupID, err)
	}
	defer rows.Close()
	bands := []Band{}
	for rows.Next() {
		var b Band
		if err := rows.Scan(&b.LeagueID, &b.From, &b.To); err != nil {
			return nil, fmt.Errorf("scan qualification band: %w", err)
		}
		bands = append(bands, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ComputeField(ctx, QualifyField{Cup: ref, Bands: bands}, s, s, s)
}

// qualCupRef loads a competition's engine-facing identity and geographic scope.
func (s *Service) qualCupRef(ctx context.Context, cupID uuid.UUID) (CompetitionRef, error) {
	var ref CompetitionRef
	var countryID, regionID *uuid.UUID
	var tier *int
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.region_id, c.name, c.competition_type, c.tier
		FROM competition.competitions c WHERE c.id = $1`, cupID).
		Scan(&ref.ID, &ref.WorldID, &countryID, &regionID, &ref.Name, &ref.Type, &tier)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompetitionRef{}, ErrCompetitionNotFound
	}
	if err != nil {
		return CompetitionRef{}, err
	}
	if tier != nil {
		ref.Tier = *tier
	}
	if regionID != nil {
		ref.Scope = ScopeRegion
		ref.RegionID = *regionID
	} else {
		ref.Scope = ScopeCountry
		if countryID != nil {
			ref.CountryID = *countryID
		}
	}
	return ref, nil
}
