package manager

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/squad"
)

// The offer's recruitment context is deliberately decoupled from the main
// JobOffer struct: each block stays a sibling nested object (never flattened),
// writes nothing, and degrades to omitted when the underlying data does not
// exist yet (e.g. form before the club has played a match).

// BoardExpectations is the recruitment preview of a club's board targets for
// the current season, computed deterministically from the club's DNA and
// persona. A candidate has not joined yet, so no mandate rows are written.
// The targets mirror the mandate set internal/board seeds on assignment, but
// neither that package nor its constants are importable here (board imports
// manager for Sack — an import cycle), so the deterministic recipe lives in
// this file with internal/board/numerics.go as its source of truth.
type BoardExpectations struct {
	Season   int            `json:"season"`
	Persona  string         `json:"persona,omitempty"`
	Mandates []BoardMandate `json:"mandates"`
}

// BoardMandate is one computed board target in the recruitment preview.
type BoardMandate struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	TargetType  string `json:"target_type"`
	TargetValue string `json:"target_value"`
}

// SquadOfferSummary is the senior-squad preview: headcount plus the
// position-weighted highest-rated player (squad.PositionalOverall).
type SquadOfferSummary struct {
	Size      int             `json:"size"`
	TopPlayer *OfferTopPlayer `json:"top_player,omitempty"`
}

// OfferTopPlayer is the squad's highest-rated player in the preview.
type OfferTopPlayer struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name,omitempty"`
	Position string    `json:"position,omitempty"`
	Overall  int       `json:"overall"`
}

// FormOfferState is the club's recent form streak. Omitted entirely when the
// club has not played a recorded match (no club.form_state row yet).
type FormOfferState struct {
	Rating     float64 `json:"rating"`
	FormString string  `json:"form_string,omitempty"`
}

// SupportersOfferRef is the club's supporter archetype plus current sentiment.
type SupportersOfferRef struct {
	Type      string `json:"type,omitempty"`
	Sentiment int    `json:"sentiment,omitempty"`
}

// OfferLeagueRef is the club's domestic league and its current-season table
// line. Position/record are omitted until a standings row exists (matches
// have been played); the league identity itself is always present because
// offers are only issued to league-membership clubs.
type OfferLeagueRef struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name,omitempty"`
	Tier     int       `json:"tier,omitempty"`
	Position *int      `json:"position,omitempty"`
	Played   int       `json:"played"`
	Won      int       `json:"won"`
	Drawn    int       `json:"drawn"`
	Lost     int       `json:"lost"`
	Points   int       `json:"points"`
}

// decorateOfferContext enriches each offer with the club context the candidate
// needs to weigh the move: the club's league table line, board expectations,
// squad headcount/top player, recent form, supporter archetype, and the full
// financial picture. Reads are batched by club and run on the pool
// (enrichment only — never writes).
func (s *Service) decorateOfferContext(ctx context.Context, offers ...*JobOffer) error {
	byClub := make(map[uuid.UUID]*JobOffer, len(offers))
	for _, o := range offers {
		if o != nil && o.ClubID != uuid.Nil {
			byClub[o.ClubID] = o
		}
	}
	if len(byClub) == 0 {
		return nil
	}
	if err := s.decorateOfferLeagues(ctx, byClub); err != nil {
		return err
	}
	if err := s.decorateOfferBoards(ctx, byClub); err != nil {
		return err
	}
	if err := s.decorateOfferSquads(ctx, byClub); err != nil {
		return err
	}
	if err := s.decorateOfferForms(ctx, byClub); err != nil {
		return err
	}
	if err := s.decorateOfferSupporters(ctx, byClub); err != nil {
		return err
	}
	if err := s.decorateOfferFinances(ctx, byClub); err != nil {
		return err
	}
	return nil
}

// decorateOfferLeagues resolves each club's league and its current-season
// table line. The best season wins: an in_progress one over a completed one,
// then the standings row with the most matches. Position is computed from the
// league table by points (id tiebreak), the recipe competition standings use.
func (s *Service) decorateOfferLeagues(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	ids := clubIDs(byClub)
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,
		       comp.id, comp.name, COALESCE(comp.tier, 0),
		       pos.played, pos.won, pos.drawn, pos.lost, pos.points, pos.position
		FROM club.clubs c
		JOIN competition.club_competitions cc
		  ON cc.club_id = c.id AND cc.role = 'league'
		JOIN competition.competitions comp ON comp.id = cc.competition_id
		LEFT JOIN LATERAL (
			SELECT st.played, st.won, st.drawn, st.lost, st.points,
			       (SELECT COUNT(*) FROM competition.standings s2
			         WHERE s2.season_id = ss.id
			           AND (s2.points > st.points OR (s2.points = st.points AND s2.id < st.id))) + 1 AS position
			FROM competition.seasons ss
			JOIN competition.standings st ON st.season_id = ss.id AND st.club_id = c.id
			WHERE ss.competition_id = comp.id AND ss.status IN ('in_progress', 'completed')
			ORDER BY CASE ss.status WHEN 'in_progress' THEN 0 ELSE 1 END, st.played DESC
			LIMIT 1
		) pos ON TRUE
		WHERE c.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("offer league context: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			clubID, leagueID                 uuid.UUID
			leagueName                       string
			tier                             int
			played, won, drawn, lost, points *int
			position                         *int
		)
		if err := rows.Scan(&clubID, &leagueID, &leagueName, &tier,
			&played, &won, &drawn, &lost, &points, &position); err != nil {
			return fmt.Errorf("scan offer league: %w", err)
		}
		l := &OfferLeagueRef{ID: leagueID, Name: leagueName, Tier: tier, Position: position}
		if played != nil {
			l.Played = *played
		}
		if won != nil {
			l.Won = *won
		}
		if drawn != nil {
			l.Drawn = *drawn
		}
		if lost != nil {
			l.Lost = *lost
		}
		if points != nil {
			l.Points = *points
		}
		byClub[clubID].League = l
	}
	return rows.Err()
}

// decorateOfferBoards resolves each club's current season, board persona and
// DNA ambition/patience in one pass and derives the deterministic target set.
func (s *Service) decorateOfferBoards(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	ids := clubIDs(byClub)
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,
		       EXTRACT(YEAR FROM COALESCE(w.launched_at, w.created_at) + w.current_day * INTERVAL '1 day')::int,
		       COALESCE(b.personality_type, ''),
		       COALESCE(d.competitive_ambition, 50),
		       COALESCE(d.patience, 50)
		FROM club.clubs c
		JOIN world.worlds w ON w.id = c.world_id
		LEFT JOIN club.boards b ON b.club_id = c.id
		LEFT JOIN club.club_dna d ON d.club_id = c.id
		WHERE c.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("offer board context: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			clubID   uuid.UUID
			season   int
			persona  string
			ambition int
			patience int
		)
		if err := rows.Scan(&clubID, &season, &persona, &ambition, &patience); err != nil {
			return fmt.Errorf("scan offer board: %w", err)
		}
		finish := offerExpectedFinish(ambition, patience)
		points := offerExpectedPoints(finish)
		byClub[clubID].Board = &BoardExpectations{
			Season:   season,
			Persona:  persona,
			Mandates: offerMandateSet(finish, points),
		}
	}
	return rows.Err()
}

// decorateOfferSquads resolves each club's senior squad headcount and the
// position-weighted highest-rated player. Club with no senior players keep a
// headcount-only squad block.
func (s *Service) decorateOfferSquads(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	ids := clubIDs(byClub)
	rows, err := s.pool.Query(ctx, `
		SELECT p.club_id, p.id, pe.display_name, p.primary_position,
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'technical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'physical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'mental'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'tactical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'goalkeeping'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = p.id AND pa.attribute_category = 'positional'), 50)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		WHERE p.club_id = ANY($1::uuid[])
		  AND p.status IN ('active', 'injured', 'suspended')
		ORDER BY p.club_id, p.id`, ids)
	if err != nil {
		return fmt.Errorf("offer squad context: %w", err)
	}
	defer rows.Close()
	squads := make(map[uuid.UUID]*SquadOfferSummary)
	for rows.Next() {
		var (
			clubID, playerID                                               uuid.UUID
			name, position                                                 string
			technical, physical, mental, tactical, goalkeeping, positional int
		)
		if err := rows.Scan(&clubID, &playerID, &name, &position,
			&technical, &physical, &mental, &tactical, &goalkeeping, &positional); err != nil {
			return fmt.Errorf("scan offer player: %w", err)
		}
		ovr := squad.PositionalOverall(position, squad.AttributeSnapshot{
			Technical: technical, Physical: physical, Mental: mental,
			Tactical: tactical, Goalkeeping: goalkeeping, Positional: positional,
		})
		sc := squads[clubID]
		if sc == nil {
			sc = &SquadOfferSummary{}
			squads[clubID] = sc
		}
		sc.Size++
		if sc.TopPlayer == nil || ovr > sc.TopPlayer.Overall {
			sc.TopPlayer = &OfferTopPlayer{ID: playerID, Name: name, Position: position, Overall: ovr}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for clubID, sc := range squads {
		byClub[clubID].Squad = sc
	}
	return nil
}

// decorateOfferForms resolves each club's recorded form streak. Clubs that
// have not played yet keep no form block ("if available").
func (s *Service) decorateOfferForms(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	ids := clubIDs(byClub)
	rows, err := s.pool.Query(ctx, `
		SELECT club_id, current_rating, COALESCE(form_string, '')
		FROM club.form_state WHERE club_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("offer form context: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			clubID  uuid.UUID
			rating  float64
			formStr string
		)
		if err := rows.Scan(&clubID, &rating, &formStr); err != nil {
			return fmt.Errorf("scan offer form: %w", err)
		}
		byClub[clubID].Form = &FormOfferState{Rating: rating, FormString: formStr}
	}
	return rows.Err()
}

// decorateOfferSupporters resolves each club's supporter archetype and current
// sentiment in one pass.
func (s *Service) decorateOfferSupporters(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	ids := clubIDs(byClub)
	rows, err := s.pool.Query(ctx, `
		SELECT club_id, identity, current_sentiment
		FROM club.supporter_groups WHERE club_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("offer supporter context: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			clubID    uuid.UUID
			identity  *string
			sentiment int
		)
		if err := rows.Scan(&clubID, &identity, &sentiment); err != nil {
			return fmt.Errorf("scan offer supporters: %w", err)
		}
		ref := &SupportersOfferRef{Sentiment: sentiment}
		if identity != nil {
			ref.Type = *identity
		}
		byClub[clubID].Supporters = ref
	}
	return rows.Err()
}

// decorateOfferFinances attaches each club's full financial picture
// (finance.FinanceSummary). The read path is ownership-free (a candidate is
// not the club's manager yet) so the finance store helpers are used directly
// rather than the gated route.
func (s *Service) decorateOfferFinances(ctx context.Context, byClub map[uuid.UUID]*JobOffer) error {
	fs := finance.NewService(s.pool, nil)
	for clubID := range byClub {
		sum, err := fs.GetSummary(ctx, clubID)
		if err != nil {
			return fmt.Errorf("offer finance context: %w", err)
		}
		byClub[clubID].Finance = sum
	}
	return nil
}

// clubIDs returns the sorted club ids of a decorate map (map iteration order
// is fine for a WHERE ANY lookup).
func clubIDs(byClub map[uuid.UUID]*JobOffer) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(byClub))
	for id := range byClub {
		ids = append(ids, id)
	}
	return ids
}

// The following mirror internal/board/numerics.go mandated targets. A club
// whose DNA says nothing resolves to the neutral 50 ambition/patience.

func offerExpectedFinish(ambition, patience int) int {
	finish := float64(110-ambition) / 8.0
	if patience >= 70 {
		finish += 1.5
	}
	if finish < 1 {
		finish = 1
	}
	if finish > 24 {
		finish = 24
	}
	return int(math.Round(finish))
}

func offerExpectedPoints(finish int) int {
	p := int(math.Round(88 - 3.5*float64(finish)))
	if p < 10 {
		p = 10
	}
	if p > 95 {
		p = 95
	}
	return p
}

func offerMandateSet(finish, points int) []BoardMandate {
	return []BoardMandate{
		{Category: "primary", Description: fmt.Sprintf("finish the league season at or above position %d", finish),
			TargetType: "league_finish", TargetValue: strconv.Itoa(finish)},
		{Category: "secondary", Description: fmt.Sprintf("collect at least %d league points", points),
			TargetType: "points_target", TargetValue: strconv.Itoa(points)},
		{Category: "strategic", Description: "maintain a break-even operating balance",
			TargetType: "operating_balance", TargetValue: "0"},
		{Category: "financial", Description: "keep the wage bill within the board-allocated structure",
			TargetType: "wage_structure", TargetValue: "0"},
	}
}
