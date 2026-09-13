package bootstrap

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/touchline/backend/pkg/playergen"
)

// persistPlayerProfile writes a generated player's football data — the
// attribute EAV, hidden traits, personality, and the initial emotional state —
// inside the caller's transaction. It is called once per squad member by
// GenerateAIClub so person/player/game-profile material always lands together
// (the club-creation tx can never half-materialise a squad).
func persistPlayerProfile(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, gp *playergen.GeneratedPlayer) error {
	if err := persistAttributes(ctx, tx, playerID, gp.Attributes); err != nil {
		return err
	}
	if err := persistHiddenTraits(ctx, tx, playerID, gp.HiddenTraits); err != nil {
		return err
	}
	if err := persistPersonality(ctx, tx, playerID, gp.Personality); err != nil {
		return err
	}
	return persistEmotionalState(ctx, tx, playerID, gp.EmotionalState)
}

// persistAttributes inserts the EAV rows. Insertion order follows the stable
// catalogue order across categories so a full regenerate is reproducible.
func persistAttributes(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, attrs map[string]int) error {
	if len(attrs) == 0 {
		return nil
	}
	// Traceability: only persist keys we know; orphan categories would pollute
	// the EAV with rows no consumer reads.
	for _, cat := range playergen.CatalogOrder {
		for _, key := range playergen.KeysForCategory(cat) {
			val, ok := attrs[key]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (player_id, attribute_category, attribute_key) DO UPDATE SET value = EXCLUDED.value`,
				playerID, cat, key, val); err != nil {
				return fmt.Errorf("insert attribute %s/%s: %w", cat, key, err)
			}
		}
	}
	return nil
}

func persistHiddenTraits(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, t playergen.HiddenTraits) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO player.player_hidden_traits
			(player_id, potential, consistency, injury_susceptibility, adaptability,
			 professionalism, ambition, loyalty, temperament, pressure_handling, learning_speed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (player_id) DO UPDATE SET
			potential = EXCLUDED.potential, consistency = EXCLUDED.consistency,
			injury_susceptibility = EXCLUDED.injury_susceptibility, adaptability = EXCLUDED.adaptability,
			professionalism = EXCLUDED.professionalism, ambition = EXCLUDED.ambition,
			loyalty = EXCLUDED.loyalty, temperament = EXCLUDED.temperament,
			pressure_handling = EXCLUDED.pressure_handling, learning_speed = EXCLUDED.learning_speed`,
		playerID, t.Potential, t.Consistency, t.InjurySusceptibility, t.Adaptability,
		t.Professionalism, t.Ambition, t.Loyalty, t.Temperament, t.PressureHandling, t.LearningSpeed,
	)
	if err != nil {
		return fmt.Errorf("insert hidden traits: %w", err)
	}
	return nil
}

func persistPersonality(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, p playergen.Personality) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO player.player_personality
			(player_id, professionalism, ambition, loyalty, ego, sociability,
			 adaptability, patience, leadership, emotional_volatility)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (player_id) DO UPDATE SET
			professionalism = EXCLUDED.professionalism, ambition = EXCLUDED.ambition,
			loyalty = EXCLUDED.loyalty, ego = EXCLUDED.ego, sociability = EXCLUDED.sociability,
			adaptability = EXCLUDED.adaptability, patience = EXCLUDED.patience,
			leadership = EXCLUDED.leadership, emotional_volatility = EXCLUDED.emotional_volatility`,
		playerID, p.Professionalism, p.Ambition, p.Loyalty, p.Ego, p.Sociability,
		p.Adaptability, p.Patience, p.Leadership, p.EmotionalVolatility,
	)
	if err != nil {
		return fmt.Errorf("insert personality: %w", err)
	}
	return nil
}

func persistEmotionalState(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, e playergen.EmotionalState) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO player.player_emotional_states (player_id, emotional_state, cause, intensity)
		VALUES ($1, $2, $3, $4)`,
		playerID, e.State, e.Cause, e.Intensity)
	if err != nil {
		return fmt.Errorf("insert emotional state: %w", err)
	}
	return nil
}
