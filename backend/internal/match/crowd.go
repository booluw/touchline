package match

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
)

// Crowd tuning (product-owned, IM66). Proposal values; recalibration is a
// data-only change.
const (
	// Stadium capacity = crowdCapacityBase + league reputation (0..100) ×
	// crowdCapacityPerRep, times a fixed per-club jitter in ±crowdCapacityJitter.
	crowdCapacityBase   = 3000
	crowdCapacityPerRep = 400
	crowdCapacityJitter = 0.30
	// crowdNoise is the per-match ± swing on the fill rate (seeded by fixture).
	crowdNoise = 0.05
	// Fill is clamped to this floor; a stadium is never more than full.
	crowdMinFill = 0.10

	// Fill rate = crowdBaseFill + the adjustments below.
	crowdBaseFill      = 0.55
	crowdSentimentSpan = 0.40 // sentiment 0..100 moves fill by ±0.20
	crowdLoyaltySpan   = 0.20 // loyalty 0..100 moves fill by ±0.10
	crowdDerby         = 0.15
	crowdSixPointer    = 0.05
	crowdDeadRubber    = -0.10
	crowdCupTie        = -0.10
)

// CrowdInputs is everything the fill rate depends on, read at kickoff.
type CrowdInputs struct {
	Sentiment int // home supporters' current_sentiment, 0..100 (50 neutral)
	Loyalty   int // home supporter group loyalty, 0..100
	Fixture   squad.FixtureContext
}

// stadiumCapacity derives a club's capacity from its league's reputation. The
// jitter is hashed from the club id, so the same club always gets the same
// ground.
func stadiumCapacity(leagueRep int, clubID uuid.UUID) int {
	jitter := (float64(binary.BigEndian.Uint64(clubID[:8])%10001)/10000*2 - 1) * crowdCapacityJitter
	return int(math.Round(float64(crowdCapacityBase+leagueRep*crowdCapacityPerRep) * (1 + jitter)))
}

// crowdFill is the share of the stadium that turns up (before match noise).
// The engine clamps the result to [crowdMinFill, 1].
func crowdFill(in CrowdInputs) float64 {
	fill := crowdBaseFill +
		float64(in.Sentiment-50)/100*crowdSentimentSpan +
		float64(in.Loyalty-50)/100*crowdLoyaltySpan
	fc := in.Fixture
	if fc.IsDerby {
		fill += crowdDerby
	}
	if fc.IsSixPointer {
		fill += crowdSixPointer
	}
	if fc.IsDeadRubber {
		fill += crowdDeadRubber
	}
	if fc.IsCupTie {
		fill += crowdCupTie
	}
	return fill
}

// crowdAttendance turns capacity + inputs into a head count. Deterministic:
// the noise draws from the fixture seed, so a replay gets the same crowd.
func crowdAttendance(capacity int, in CrowdInputs, seed int64) int {
	noise := (rand.New(rand.NewSource(seed^0x63726f7764)).Float64()*2 - 1) * crowdNoise
	fill := math.Max(crowdMinFill, math.Min(1, crowdFill(in)+noise))
	return int(math.Round(float64(capacity) * fill))
}

// fixtureAttendance reads the home club's crowd inputs and returns the match
// attendance. A club with no stadium capacity yet gets one derived from its
// league's reputation, stored once in the same transaction.
func fixtureAttendance(ctx context.Context, tx pgx.Tx, f *Fixture, fc squad.FixtureContext, seed int64) (int, error) {
	var capacity *int
	var leagueRep int
	in := CrowdInputs{Sentiment: 50, Loyalty: 50, Fixture: fc}
	err := tx.QueryRow(ctx, `
		SELECT c.stadium_capacity,
		       COALESCE(sg.current_sentiment, 50), COALESCE(sg.loyalty, 50),
		       COALESCE((SELECT comp.reputation FROM competition.club_competitions cc
		                 JOIN competition.competitions comp ON comp.id = cc.competition_id
		                 WHERE cc.club_id = c.id AND cc.role = 'league'), 0)
		FROM club.clubs c
		LEFT JOIN club.supporter_groups sg ON sg.club_id = c.id
		WHERE c.id = $1`, f.HomeClub.ID).Scan(&capacity, &in.Sentiment, &in.Loyalty, &leagueRep)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("attendance: home club %s not found", f.HomeClub.ID)
	}
	if err != nil {
		return 0, fmt.Errorf("attendance: load home club: %w", err)
	}
	if capacity == nil {
		// COALESCE keeps a capacity a concurrent kickoff already stored.
		var c int
		if err := tx.QueryRow(ctx, `
			UPDATE club.clubs SET stadium_capacity = COALESCE(stadium_capacity, $2)
			WHERE id = $1 RETURNING stadium_capacity`,
			f.HomeClub.ID, stadiumCapacity(leagueRep, f.HomeClub.ID)).Scan(&c); err != nil {
			return 0, fmt.Errorf("attendance: set capacity: %w", err)
		}
		capacity = &c
	}
	return crowdAttendance(*capacity, in, seed), nil
}
