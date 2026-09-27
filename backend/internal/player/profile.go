package player

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
)

// PlayerCareer is a player's all-time record in the world. The counts come
// from `player.player_appearances`, which the match-completion transaction
// writes, so a player's career only holds completed matches. `average_rating`
// is the mean of the per-match 1..10 ratings that exist — pre-attribution
// matches have no rating and count as appearances without moving the average.
type PlayerCareer struct {
	Appearances   int     `json:"appearances"`
	Goals         int     `json:"goals"`
	Assists       int     `json:"assists"`
	AverageRating float64 `json:"average_rating"`
}

// PlayerDetail is the one-player read: identity, current club, ability, career
// record, and — for the caller's own club only — the weekly wage.
//
// Ability is the same two numbers the squad screen shows (IM17): the six
// attribute-category means of the EAV and the position-weighted `overall` from
// the canonical `squad.PositionalOverall` recipe. Nothing is denormalized.
type PlayerDetail struct {
	Player      *apiref.PlayerRef `json:"player"`
	Club        *apiref.ClubRef   `json:"club,omitempty"`
	FirstName   string            `json:"first_name"`
	LastName    string            `json:"last_name"`
	DisplayName string            `json:"display_name"`
	Nationality string            `json:"nationality"`
	DateOfBirth string            `json:"date_of_birth"`
	Position    string            `json:"position"`
	SquadNumber *int              `json:"squad_number,omitempty"`
	Attributes  PlayerAttributes  `json:"attributes"`
	Overall     int               `json:"overall"`
	Career      PlayerCareer      `json:"career"`
	// WeeklyWage is the player's active contract wage, and is present **only**
	// when the player is at the caller's own club: another club's wage is not the
	// caller's business. Omitted (never zero) when it is not theirs.
	WeeklyWage *int64 `json:"weekly_wage,omitempty"`
}

// GetPlayerDetail returns one player's profile from the caller's world.
//
// The read is world-scoped, not ownership-scoped: a manager may open any player
// in their own world (a cup opponent, a rival's star, a signing target), which
// is why a wrong-world id is reported as ErrPlayerNotFound — the same answer as
// a missing id, so the endpoint never confirms another world's players exist.
// The wage is the single field that stays private, and it is resolved against
// the caller's own club.
func (s *Service) GetPlayerDetail(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*PlayerDetail, error) {
	prof, err := playerProfile(ctx, s.pool, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.WorldID != worldID {
		return nil, ErrPlayerNotFound
	}

	attrs, err := playerAttributeMeans(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	career, err := playerCareer(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}

	out := &PlayerDetail{
		Player:      &apiref.PlayerRef{ID: prof.ID, Name: prof.DisplayName},
		Club:        prof.Club,
		FirstName:   prof.FirstName,
		LastName:    prof.LastName,
		DisplayName: prof.DisplayName,
		Nationality: prof.Nationality,
		DateOfBirth: prof.DateOfBirth,
		Position:    prof.PrimaryPosition,
		SquadNumber: prof.SquadNumber,
		Attributes:  attrs,
		Overall: squad.PositionalOverall(prof.PrimaryPosition, squad.AttributeSnapshot{
			Technical: attrs.Technical, Physical: attrs.Physical, Mental: attrs.Mental,
			Tactical: attrs.Tactical, Goalkeeping: attrs.Goalkeeping, Positional: attrs.Positional,
		}),
		Career: career,
	}

	// The wage is the caller's own club's business only. A manager without a
	// club (jobless, or between jobs) simply has no wage to see — that is not an
	// error on a read that is otherwise allowed.
	ownClubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil && !errors.Is(err, ErrManagerHasNoClub) {
		return nil, err
	}
	if ownClubID != uuid.Nil && prof.ClubID != nil && *prof.ClubID == ownClubID {
		wage, err := playerWeeklyWage(ctx, s.pool, playerID)
		if err != nil {
			return nil, err
		}
		out.WeeklyWage = wage
	}
	return out, nil
}

// playerAttributeMeans is the one-player twin of squadAttributeMeans: the
// round-half-up integer mean of each attribute category of the EAV. The
// arithmetic is the shared `categoryMean`, so a player's profile, the squad
// screen and the match engine's overall all read the same numbers.
func playerAttributeMeans(ctx context.Context, q dbtx, playerID uuid.UUID) (PlayerAttributes, error) {
	var attrs PlayerAttributes
	rows, err := q.Query(ctx,
		`SELECT attribute_category, value FROM player.player_attributes WHERE player_id = $1`, playerID)
	if err != nil {
		return attrs, err
	}
	defer rows.Close()

	buckets := make(map[string][]int, 6)
	for rows.Next() {
		var category string
		var value int
		if err := rows.Scan(&category, &value); err != nil {
			return attrs, err
		}
		buckets[category] = append(buckets[category], value)
	}
	if err := rows.Err(); err != nil {
		return attrs, err
	}
	attrs.Technical = categoryMean(buckets["technical"])
	attrs.Physical = categoryMean(buckets["physical"])
	attrs.Mental = categoryMean(buckets["mental"])
	attrs.Tactical = categoryMean(buckets["tactical"])
	attrs.Goalkeeping = categoryMean(buckets["goalkeeping"])
	attrs.Positional = categoryMean(buckets["positional"])
	return attrs, nil
}

// playerCareer totals the player's appearance rows. AVG ignores NULL ratings, so
// a match that predates attribution is still an appearance but not an entry in
// the rating average.
func playerCareer(ctx context.Context, q dbtx, playerID uuid.UUID) (PlayerCareer, error) {
	var c PlayerCareer
	err := q.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(a.goals), 0),
		       COALESCE(SUM(a.assists), 0),
		       COALESCE(ROUND(AVG(a.rating)::numeric, 2), 0)
		FROM player.player_appearances a WHERE a.player_id = $1`, playerID,
	).Scan(&c.Appearances, &c.Goals, &c.Assists, &c.AverageRating)
	return c, err
}

// playerWeeklyWage reads the player's active contract wage in whole units, the
// same `::bigint` cast the club finances read uses, so a player page and the
// wage bill never disagree. Nil when the player has no active contract (a free
// agent, or a released player).
func playerWeeklyWage(ctx context.Context, q dbtx, playerID uuid.UUID) (*int64, error) {
	var wage int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT c.weekly_wage::bigint
			FROM player.contracts c
			WHERE c.player_id = $1 AND c.status = 'active'
			ORDER BY c.start_date DESC LIMIT 1
		), 0)`, playerID).Scan(&wage)
	if err != nil {
		return nil, err
	}
	if wage == 0 {
		return nil, nil
	}
	return &wage, nil
}
