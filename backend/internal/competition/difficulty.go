package competition

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
)

// Difficulty rates one upcoming fixture from the perspective of one club
// (IM59). Level 1 is the easiest, 5 the hardest; Factors explain the score in
// the same {label, delta} shape as the "Why" cards.
type Difficulty struct {
	Level   int                `json:"level"`
	Label   string             `json:"label"`
	Factors []DifficultyFactor `json:"factors"`
}

// DifficultyFactor is one signed contribution to a difficulty score: positive
// makes the fixture harder.
type DifficultyFactor struct {
	Label  string `json:"label"`
	Delta  int    `json:"delta"`
	Detail string `json:"detail"`
}

// UpcomingFixture is a scheduled fixture with its difficulty for the club.
type UpcomingFixture struct {
	Fixture
	Difficulty Difficulty `json:"difficulty"`
}

// Difficulty tuning (product-owned, IM59). Every factor is in "overall rating
// points" so they add up on one scale.
const (
	// difficultyVenue is the home-advantage swing: playing away adds it,
	// playing at home removes it.
	difficultyVenue = 2
	// difficultyFormPerPoint converts the opponent's last-5 points above the
	// neutral 7 (e.g. W-W-D-L-L) into rating points.
	difficultyFormPerPoint = 0.5
	difficultyNeutralForm  = 7
)

// difficultyLevels are the inclusive upper score bounds for levels 1..4;
// anything above the last bound is level 5. "Even" (3) is centred on 0.
var difficultyLevels = []struct {
	max   int
	label string
}{
	{-8, "Very easy"},
	{-3, "Easy"},
	{2, "Even"},
	{7, "Hard"},
}

// rateDifficulty scores a fixture: opponent XI strength minus ours, plus
// venue, plus the opponent's recent form. oppForm is newest- or oldest-first
// W/D/L results; only the points matter.
func rateDifficulty(ourXI, oppXI float64, home bool, oppForm []string) Difficulty {
	gap := int(math.Round(oppXI - ourXI))
	venue, venueDetail := difficultyVenue, "Away"
	if home {
		venue, venueDetail = -difficultyVenue, "Home"
	}
	pts := 0
	for _, r := range oppForm {
		switch r {
		case "W":
			pts += 3
		case "D":
			pts++
		}
	}
	form := 0
	if len(oppForm) > 0 {
		form = int(math.Round(float64(pts-difficultyNeutralForm) * difficultyFormPerPoint))
	}
	formDetail := strings.Join(oppForm, "")
	if formDetail == "" {
		formDetail = "No results yet"
	}
	factors := []DifficultyFactor{
		{Label: "Strength gap", Delta: gap, Detail: fmt.Sprintf("Their best XI %.0f vs ours %.0f", oppXI, ourXI)},
		{Label: "Venue", Delta: venue, Detail: venueDetail},
		{Label: "Opponent form", Delta: form, Detail: formDetail},
	}
	score := gap + venue + form
	d := Difficulty{Level: 5, Label: "Very hard", Factors: factors}
	for i, b := range difficultyLevels {
		if score <= b.max {
			d.Level, d.Label = i+1, b.label
			break
		}
	}
	return d
}

// xiStrength is the mean positional overall of a club's best eleven
// available players: the strength a club can actually field on that date.
// An empty squad rates 0.
func xiStrength(players []squad.LoadedPlayer) float64 {
	ovr := make([]int, 0, len(players))
	for _, p := range players {
		if p.Available {
			ovr = append(ovr, squad.PositionalOverall(p.Position, p.Attributes))
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ovr)))
	if len(ovr) > 11 {
		ovr = ovr[:11]
	}
	if len(ovr) == 0 {
		return 0
	}
	sum := 0
	for _, o := range ovr {
		sum += o
	}
	return float64(sum) / float64(len(ovr))
}

// UpcomingClubFixtures returns the club's next scheduled fixtures (all
// competitions, kickoff order) each rated for difficulty against the squad
// both clubs can field on that matchday (IM59). World checks mirror
// ListClubFixtures.
func (s *Service) UpcomingClubFixtures(ctx context.Context, worldID, clubID uuid.UUID, limit int) ([]UpcomingFixture, error) {
	if err := s.checkClubWorld(ctx, worldID, clubID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name,
		       NULL::uuid
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.world_id = $1 AND (f.home_club_id = $2 OR f.away_club_id = $2)
		  AND f.status IN ('scheduled', 'postponed')
		ORDER BY f.scheduled_at, f.matchday, f.home_club_id
		LIMIT $3`, worldID, clubID, limit)
	if err != nil {
		return nil, fmt.Errorf("query upcoming fixtures: %w", err)
	}
	fixtures, err := scanFixtures(rows)
	if err != nil {
		return nil, err
	}
	return s.rateUpcoming(ctx, clubID, fixtures)
}

// rateUpcoming attaches a difficulty, relative to clubID, to each fixture.
func (s *Service) rateUpcoming(ctx context.Context, clubID uuid.UUID, fixtures []Fixture) ([]UpcomingFixture, error) {
	opps := make([]uuid.UUID, 0, len(fixtures))
	for _, f := range fixtures {
		opps = append(opps, opponentOf(f, clubID))
	}
	forms, err := s.recentForms(ctx, opps)
	if err != nil {
		return nil, err
	}

	store := squad.NewStore(s.pool)
	out := make([]UpcomingFixture, 0, len(fixtures))
	for i, f := range fixtures {
		ours, err := store.LoadSquad(ctx, clubID, f.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("load own squad: %w", err)
		}
		theirs, err := store.LoadSquad(ctx, opps[i], f.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("load opponent squad: %w", err)
		}
		out = append(out, UpcomingFixture{
			Fixture:    f,
			Difficulty: rateDifficulty(xiStrength(ours), xiStrength(theirs), f.HomeClub.ID == clubID, forms[opps[i]]),
		})
	}
	return out, nil
}

// checkClubWorld is the ListClubFixtures ownership check: ErrClubNotFound for
// a missing club, ErrClubWorldMismatch for a cross-world read.
func (s *Service) checkClubWorld(ctx context.Context, worldID, clubID uuid.UUID) error {
	var clubWorld uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT world_id FROM club.clubs WHERE id = $1`, clubID).Scan(&clubWorld)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClubNotFound
	}
	if err != nil {
		return fmt.Errorf("club world: %w", err)
	}
	if clubWorld != worldID {
		return ErrClubWorldMismatch
	}
	return nil
}

func opponentOf(f Fixture, clubID uuid.UUID) uuid.UUID {
	if f.HomeClub.ID == clubID {
		return f.AwayClub.ID
	}
	return f.HomeClub.ID
}

// recentForms reads each club's persisted all-competition form string
// (club.form_state, oldest first, '-' joined) as W/D/L slices. Clubs without
// a row map to nil (no results yet).
func (s *Service) recentForms(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	out := map[uuid.UUID][]string{}
	if len(clubIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT club_id, form_string FROM club.form_state WHERE club_id = ANY($1)`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("opponent form: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var fs string
		if err := rows.Scan(&id, &fs); err != nil {
			return nil, fmt.Errorf("scan opponent form: %w", err)
		}
		for _, r := range strings.Split(fs, "-") {
			if r == "W" || r == "D" || r == "L" {
				out[id] = append(out[id], r)
			}
		}
	}
	return out, rows.Err()
}
