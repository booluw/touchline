package playerpool

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/playergen"
)

// PersistGeneratedPlayer writes one generated player as person.people +
// player.players rows plus the full football profile inside the caller's tx.
// It is the cluster-external seam for academy/street intakes (the free-agent
// pool and drafts use the unexported helper directly); see it for semantics.
func PersistGeneratedPlayer(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, clubID, countryID *uuid.UUID, squadNumber int, gp *playergen.GeneratedPlayer, ref time.Time) (playerID, personID uuid.UUID, err error) {
	return persistGeneratedPlayer(ctx, tx, worldID, clubID, countryID, squadNumber, gp, ref)
}

// persistGeneratedPlayer writes one generated player as person.people +
// player.players rows plus the full football profile (attribute EAV, hidden
// traits, personality, initial emotional state) inside the caller's tx. It is
// the single place pool free agents and academy/street intakes are persisted,
// so person/player/game-profile material always lands together. clubID nil
// means the player is created as a free agent (status 'free_agent');
// otherwise status is 'active' and squad_number may be set.
func persistGeneratedPlayer(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, clubID, countryID *uuid.UUID, squadNumber int, gp *playergen.GeneratedPlayer, ref time.Time) (playerID, personID uuid.UUID, err error) {
	var pid uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO person.people
			(world_id, first_name, last_name, display_name, date_of_birth, nationality_code)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		worldID, gp.FirstName, gp.LastName, gp.DisplayName, dobFor(ref, gp.Age), gp.NationalityCode,
	).Scan(&pid); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert person: %w", err)
	}

	status := "free_agent"
	if clubID != nil {
		status = "active"
	}

	var num *int
	if clubID != nil {
		num = &squadNumber
	}

	var plID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO player.players
			(world_id, person_id, club_id, primary_position, squad_number, status,
			 is_academy_product, origin, country_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		worldID, pid, clubID, gp.PrimaryPosition, num, status,
		gp.AcademyProduct, gp.Origin, countryID,
	).Scan(&plID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert player: %w", err)
	}

	if err := persistPlayerProfile(ctx, tx, plID, gp); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	log.Printf("playerpool: + %s (%s) %s %s world=%s country=%v club=%v squad=%v",
		gp.DisplayName, gp.NationalityCode, gp.PrimaryPosition, gp.Origin,
		worldID, countryID, clubID, num)
	return plID, pid, nil
}

// persistPlayerProfile writes a generated player's football data — the
// attribute EAV, hidden traits, personality, and the initial emotional state —
// inside the caller's transaction.
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

// daysTruncate strips the time component of the world's season reference date,
// using the UTC calendar day so the anchor is timezone-independent and matches
// the matchday world-day counter (see matchday.worldDate).
func daysTruncate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dobFor derives the player's date_of_birth from the world reference date.
func dobFor(seasonRef time.Time, age int) time.Time {
	return seasonRef.AddDate(-age, 0, 0)
}

// ageFor derives a player's age in years from the world reference date. It is
// exact for players whose DOB was derived via dobFor.
func ageFor(ref time.Time, dob time.Time) int {
	return ref.Year() - dob.Year()
}
