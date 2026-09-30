package board

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecordCompletedMatch rates both managers for one finished fixture (IM33): it
// stores the board's per-match rating, moves supporter sentiment, and publishes
// a fan-reaction story for human-managed clubs. It runs inside the match
// completion transaction, after the social hook (it reads the rivalry edge),
// and is idempotent per (fixture, club). Board confidence is not touched here;
// the monthly review reads the stored ratings.
func (s *Service) RecordCompletedMatch(ctx context.Context, tx pgx.Tx, worldID, fixtureID, matchEventID,
	homeClubID, awayClubID uuid.UUID, homeGoals, awayGoals int) error {
	home, err := s.store.loadMatchClub(ctx, tx, homeClubID)
	if err != nil {
		return err
	}
	away, err := s.store.loadMatchClub(ctx, tx, awayClubID)
	if err != nil {
		return err
	}
	strength, err := s.store.rivalryStrength(ctx, tx, homeClubID, awayClubID)
	if err != nil {
		return err
	}
	rivalry := strength >= RivalryStrengthThreshold

	sides := []struct {
		id             uuid.UUID
		own, opp       matchClub
		isHome         bool
		goalsF, goalsA int
	}{
		{homeClubID, home, away, true, homeGoals, awayGoals},
		{awayClubID, away, home, false, awayGoals, homeGoals},
	}
	for _, sd := range sides {
		if sd.own.managerID == nil {
			continue // vacant seat: nobody to rate
		}
		rating := matchRating(sd.goalsF, sd.goalsA, expectedResult(sd.own.reputation, sd.opp.reputation, sd.isHome))
		after := sentimentAfterMatch(sd.own.sentiment, rating, rivalry)
		tag, err := tx.Exec(ctx, `
			INSERT INTO manager.match_ratings
			    (fixture_id, club_id, manager_id, world_id, board_rating, sentiment_before, sentiment_after)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (fixture_id, club_id) DO NOTHING`,
			fixtureID, sd.id, *sd.own.managerID, worldID, rating, sd.own.sentiment, after)
		if err != nil {
			return fmt.Errorf("record match rating: %w", err)
		}
		if tag.RowsAffected() == 0 {
			continue // already recorded for this fixture
		}
		if err := s.store.updateSentiment(ctx, tx, sd.id, after); err != nil {
			return err
		}
		if sd.own.isPolicyBot {
			continue // fan stories are for human-managed clubs only
		}
		headline, body := buildFanReaction(fanSeed(fixtureID, sd.id), sd.own.name, sd.own.managerName,
			sd.opp.name, sd.goalsF, sd.goalsA, after, rating)
		// country_id resolves the club's free-text country against world.countries;
		// no match leaves it NULL, which the feed shows in every country.
		if _, err := tx.Exec(ctx, `
			INSERT INTO world.news_stories (world_id, headline, body, category, related_event_id, country_id)
			VALUES ($1, $2, $3, 'fan_reaction', $4,
			        (SELECT id FROM world.countries
			         WHERE world_id = $1 AND (lower(code) = lower($5) OR lower(name) = lower($5)) LIMIT 1))`,
			worldID, headline, body, matchEventID, sd.own.country); err != nil {
			return fmt.Errorf("publish fan reaction: %w", err)
		}
	}
	return nil
}
