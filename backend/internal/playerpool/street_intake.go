package playerpool

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// EventStreetIntake fires when a country's seasonal street discovery runs
// (A05): very young, unaffiliated prospects join the country free-agent pool.
const EventStreetIntake = "COUNTRY_ACADEMY_INTAKE"

// StreetIntakeConfig holds the per-country seasonal street-intake band.
type StreetIntakeConfig struct {
	MinCount int
	MaxCount int
	MinAge   int
	MaxAge   int
}

// DefaultStreetIntakeConfig is the shipped band: 10–20 discoveries aged 13–15.
var DefaultStreetIntakeConfig = StreetIntakeConfig{MinCount: 10, MaxCount: 20, MinAge: 13, MaxAge: 15}

// streetTalentOdds skews street discoveries heavily toward journeymen with the
// occasional raw gem — enough that a fresh world is never barren, rare enough
// that a street kid is a story.
var streetTalentOdds = playergen.TalentOdds{Journeyman: 975, TopProspect: 22, Wonderkid: 3, Generational: 0}

// StreetIntake discovers a season's cohort of street-origin free agents in one
// country and persists them into the country pool (club_id NULL,
// status='free_agent'). It is idempotent per (world, country, season) via the
// world.country_academy_intakes claim row, so a redelivered ROLLOVER /
// SEASON_COMPLETED hook returns the empty slice rather than duplicating the
// cohort. Returns the new player ids.
func StreetIntake(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID, countryID uuid.UUID, seasonNumber int,
	factory *playergen.PlayerFactory, ref time.Time, cfg StreetIntakeConfig,
) ([]uuid.UUID, error) {
	if cfg.MinCount <= 0 {
		cfg = DefaultStreetIntakeConfig
	}
	if cfg.MaxAge < cfg.MinAge {
		cfg.MaxAge = cfg.MinAge
	}

	claimed, err := claimCountryIntake(ctx, tx, worldID, countryID, seasonNumber)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil // another delivery of this season already ran it
	}

	// Best-effort nationality: the country's code only forces generation when
	// it is a real ref.nationalities code (world.countries codes are
	// admin-defined and need not match ISO).
	code := countryNationality(ctx, tx, worldID, countryID)

	rng := factory.Rng()
	count := cfg.MinCount + rng.Intn(cfg.MaxCount-cfg.MinCount+1)
	ids := make([]uuid.UUID, 0, count)
	for i := 0; i < count; i++ {
		age := cfg.MinAge + rng.Intn(cfg.MaxAge-cfg.MinAge+1)
		gp, err := factory.CreatePlayerWithOptions(playergen.CreatePlayerOptions{
			MinAge:      age,
			MaxAge:      age,
			Nationality: code,
			Origin:      "street",
			TalentOdds:  streetTalentOdds,
		})
		if err != nil {
			return nil, fmt.Errorf("generate street player %d: %w", i, err)
		}
		playerID, _, err := persistGeneratedPlayer(ctx, tx, worldID, nil, &countryID, 0, gp, ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, playerID)
	}

	if err := finishCountryIntake(ctx, tx, worldID, countryID, seasonNumber, len(ids)); err != nil {
		return nil, err
	}
	if pub != nil {
		actor := "system"
		_ = eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
			WorldID:   worldID,
			EventType: EventStreetIntake,
			ActorType: &actor,
			Payload: mustJSON(map[string]any{
				"country_id":    countryID,
				"season_number": seasonNumber,
				"player_count":  len(ids),
				"player_ids":    ids,
			}),
		})
	}
	return ids, nil
}

// claimCountryIntake inserts the (world, country, season) idempotency row and
// reports whether this caller won the claim.
func claimCountryIntake(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID, season int) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO world.country_academy_intakes (world_id, country_id, season_number, player_count)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (world_id, country_id, season_number) DO NOTHING`,
		worldID, countryID, season)
	if err != nil {
		return false, fmt.Errorf("claim street intake: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func finishCountryIntake(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID, season, count int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE world.country_academy_intakes SET player_count = $4
		WHERE world_id = $1 AND country_id = $2 AND season_number = $3`,
		worldID, countryID, season, count); err != nil {
		return fmt.Errorf("finish street intake: %w", err)
	}
	return nil
}

// countryNationality resolves a valid ISO nationality code for a country,
// returning "" when the world country code is absent or unknown to
// ref.nationalities (callers then fall back to weighted selection).
func countryNationality(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) string {
	var code string
	if err := tx.QueryRow(ctx,
		`SELECT code FROM world.countries WHERE id = $1 AND world_id = $2`,
		countryID, worldID).Scan(&code); err != nil {
		return ""
	}
	var valid bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM ref.nationalities WHERE code = $1)`, code).Scan(&valid); err != nil || !valid {
		return ""
	}
	return code
}
