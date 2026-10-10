package scout

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/matchsim"
)

// briefSampleSize is how many of the opponent's latest completed matches the
// assistant reads.
const briefSampleSize = 5

// OpponentProfile is the real, persisted evidence the assistant reasons from:
// the opponent's saved style and per-game averages over its latest completed
// matches. XG fields are nil when none of those matches carried xG (pre-IM58).
type OpponentProfile struct {
	Style        string   `json:"style"`
	Matches      int      `json:"matches"`
	GoalsFor     float64  `json:"goals_for"`
	GoalsAgainst float64  `json:"goals_against"`
	XGFor        *float64 `json:"xg_for,omitempty"`
	XGAgainst    *float64 `json:"xg_against,omitempty"`
}

// FitFactor is one signed "Why?" row (same shape as the outlook factors).
type FitFactor struct {
	Label string `json:"label"`
	Delta int    `json:"delta"`
}

// TacticalFit scores the club's saved instructions against the suggestion.
type TacticalFit struct {
	Score   int         `json:"score"`
	Factors []FitFactor `json:"factors"`
}

// TacticalBrief is GET /api/clubs/:id/tactics/advice (IM61): the assistant
// manager's plan for the next fixture, the evidence behind it, and how well
// the club's current instructions fit it.
type TacticalBrief struct {
	FixtureID   uuid.UUID                `json:"fixture_id"`
	ScheduledAt time.Time                `json:"scheduled_at"`
	HomeOrAway  string                   `json:"home_or_away"`
	Opponent    apiref.ClubRef           `json:"opponent"`
	Profile     OpponentProfile          `json:"profile"`
	Headline    string                   `json:"headline"`
	Summary     string                   `json:"summary"`
	Suggested   tactics.InstructionsView `json:"suggested"`
	Current     tactics.InstructionsView `json:"current"`
	Fit         TacticalFit              `json:"fit"`
}

// advice is one dial recommendation with the evidence that drove it.
type advice struct {
	dial   string // mentality | pressing | width | tempo
	value  int    // -1 / +1
	weight int    // 1..10, how strongly the evidence points this way
	reason string // evidence, phrased for the headline
	match  string // Why? label when the club already follows it
	miss   string // Why? label when it does not
}

// TacticalBrief builds the assistant's plan for the club's next fixture. A nil
// brief (nil, nil) means there is no upcoming fixture.
func (s *Service) TacticalBrief(ctx context.Context, worldID, clubID uuid.UUID) (*TacticalBrief, error) {
	f, err := s.comp.NextClubFixture(ctx, worldID, clubID)
	if err != nil || f == nil {
		return nil, err
	}
	homeOrAway, opp := "away", f.HomeClub
	if f.HomeClub.ID == clubID {
		homeOrAway, opp = "home", f.AwayClub
	}
	store := squad.NewStore(s.pool)
	oppTactics, err := store.LoadTactics(ctx, opp.ID)
	if err != nil {
		return nil, err
	}
	own, err := store.LoadTactics(ctx, clubID)
	if err != nil {
		return nil, err
	}
	profile, err := s.opponentProfile(ctx, opp.ID)
	if err != nil {
		return nil, err
	}
	// An unsaved or unknown style plays balanced (squad.ResolveTactics).
	profile.Style = matchsim.StyleBalanced
	if matchsim.IsStyle(oppTactics.Style) {
		profile.Style = oppTactics.Style
	}

	adv := recommend(opp.Name, profile)
	suggested := own.Instructions
	for _, a := range adv {
		setDial(&suggested, a.dial, a.value)
	}
	headline, summary := headlineFor(opp.Name, adv)
	return &TacticalBrief{
		FixtureID:   f.ID,
		ScheduledAt: f.ScheduledAt,
		HomeOrAway:  homeOrAway,
		Opponent:    opp,
		Profile:     profile,
		Headline:    headline,
		Summary:     summary,
		Suggested:   tactics.ViewInstructions(suggested),
		Current:     tactics.ViewInstructions(own.Instructions),
		Fit:         fitOf(own.Instructions, adv),
	}, nil
}

// opponentProfile averages the opponent's latest completed matches.
func (s *Service) opponentProfile(ctx context.Context, clubID uuid.UUID) (OpponentProfile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.home_club_id = $1, m.home_score, m.away_score, m.home_xg::float8, m.away_xg::float8
		FROM match.matches m JOIN match.fixtures f ON f.id = m.fixture_id
		WHERE m.status = 'completed' AND (f.home_club_id = $1 OR f.away_club_id = $1)
		ORDER BY m.ended_at DESC NULLS LAST
		LIMIT $2`, clubID, briefSampleSize)
	if err != nil {
		return OpponentProfile{}, fmt.Errorf("opponent profile: %w", err)
	}
	defer rows.Close()
	var p OpponentProfile
	var xgFor, xgAgainst float64
	xgN := 0
	for rows.Next() {
		var home bool
		var hs, as int
		var hx, ax *float64
		if err := rows.Scan(&home, &hs, &as, &hx, &ax); err != nil {
			return OpponentProfile{}, fmt.Errorf("opponent profile: %w", err)
		}
		if !home {
			hs, as, hx, ax = as, hs, ax, hx
		}
		p.Matches++
		p.GoalsFor += float64(hs)
		p.GoalsAgainst += float64(as)
		if hx != nil && ax != nil {
			xgN++
			xgFor += *hx
			xgAgainst += *ax
		}
	}
	if err := rows.Err(); err != nil {
		return OpponentProfile{}, fmt.Errorf("opponent profile: %w", err)
	}
	if p.Matches > 0 {
		p.GoalsFor = round2(p.GoalsFor / float64(p.Matches))
		p.GoalsAgainst = round2(p.GoalsAgainst / float64(p.Matches))
	}
	if xgN > 0 {
		f, a := round2(xgFor/float64(xgN)), round2(xgAgainst/float64(xgN))
		p.XGFor, p.XGAgainst = &f, &a
	}
	return p, nil
}

// recommend turns the opponent's evidence into dial advice. Every rule maps to
// a lever the engine really has (matchsim StyleSpec + instructions), so the
// advice is what actually moves the simulation:
//   - leaky opponent (they concede more than they create) → positive
//     mentality: chance volume pays against weak defences; dangerous
//     opponent → cautious: cut how dangerous your concessions are;
//   - low block: their concessions are rarely dangerous (low conceded
//     conversion), so volume beats them → wide;
//   - gegenpress: their own concessions are lethal and they tire fastest →
//     direct tempo for more shots, low press to stay fresh late;
//   - possession: they live on the ball → high press to win it back.
func recommend(oppName string, p OpponentProfile) []advice {
	var out []advice
	if p.Matches > 0 {
		created, conceded, unit := p.GoalsFor, p.GoalsAgainst, "goals"
		if p.XGFor != nil {
			created, conceded, unit = *p.XGFor, *p.XGAgainst, "xG"
		}
		gap := conceded - created
		w := int(math.Min(10, math.Round(math.Abs(gap)*10)))
		switch {
		case gap >= 0.3:
			out = append(out, advice{"mentality", 1, w,
				fmt.Sprintf("%s concede %.1f %s a game and create %.1f", oppName, conceded, unit, created),
				"Positive mentality vs a leaky defence", "Not attacking a leaky defence"})
		case gap <= -0.3:
			out = append(out, advice{"mentality", -1, w,
				fmt.Sprintf("%s create %.1f %s a game and concede %.1f", oppName, created, unit, conceded),
				"Cautious mentality vs a dangerous attack", "Open against a dangerous attack"})
		}
	}
	switch p.Style {
	case matchsim.StyleLowBlock:
		out = append(out, advice{"width", 1, 6, oppName + " sit in a low block",
			"Wide play stretches their low block", "Narrow play into their low block"})
	case matchsim.StyleGegenpress:
		out = append(out,
			advice{"tempo", 1, 6, oppName + " gegenpress and leave space behind",
				"Direct tempo exploits space behind their press", "Slow build-up into their press"},
			advice{"pressing", -1, 4, oppName + " tire late from pressing",
				"Low press keeps you fresher than them late", "Matching their press burns your legs"})
	case matchsim.StylePossession:
		out = append(out, advice{"pressing", 1, 6, oppName + " build through possession",
			"High press disrupts their possession", "Letting them keep the ball"})
	}
	return out
}

// headlineFor phrases the strongest evidence as the assistant's headline.
func headlineFor(oppName string, adv []advice) (string, string) {
	if len(adv) == 0 {
		return "No clear edge against " + oppName + " yet",
			"Too little evidence to change your plan; keep your current instructions."
	}
	top := adv[0]
	for _, a := range adv[1:] {
		if a.weight > top.weight {
			top = a
		}
	}
	summary := ""
	for i, a := range adv {
		if i > 0 {
			summary += "; "
		}
		summary += a.match
	}
	return top.reason, summary + "."
}

// fitOf scores the saved dials: 50 ± each advice's weight (follow / ignore).
func fitOf(own matchsim.Instructions, adv []advice) TacticalFit {
	fit := TacticalFit{Score: 50, Factors: []FitFactor{}}
	for _, a := range adv {
		f := FitFactor{Label: a.miss, Delta: -a.weight}
		if dialOf(own, a.dial) == a.value {
			f = FitFactor{Label: a.match, Delta: a.weight}
		}
		fit.Score += f.Delta * 2
		fit.Factors = append(fit.Factors, f)
	}
	fit.Score = max(0, min(100, fit.Score))
	return fit
}

func dialOf(in matchsim.Instructions, dial string) int {
	switch dial {
	case "mentality":
		return in.Mentality
	case "pressing":
		return in.Pressing
	case "width":
		return in.Width
	default:
		return in.Tempo
	}
}

func setDial(in *matchsim.Instructions, dial string, v int) {
	switch dial {
	case "mentality":
		in.Mentality = v
	case "pressing":
		in.Pressing = v
	case "width":
		in.Width = v
	default:
		in.Tempo = v
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
