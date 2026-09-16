package training

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlayerAttributeChanges is the S08-01 weekly delta log
// (player.player_attribute_changes). The training sweep is the only writer:
// every non-zero net attribute movement in a week becomes one
// (player_id, applied_week, attribute_key, delta) row, plus a single 'morale'
// pseudo-key row for the weekly morale swing (delta in 0..100 integer points).

// MoraleDeltaKey is the pseudo attribute_key carrying a week's morale swing in
// the same table as real attribute deltas, so read models can render the same
// signed "movement" block for motivation and skills alike.
const MoraleDeltaKey = "morale"

// recordDeltas writes one row per net-moved key for a player's week inside the
// caller's transaction. Zero deltas are skipped. writeMorale=true (and
// moraleDelta != 0) additionally records the weekly morale swing. Rows are
// upserted so a redelivered weekly tick never double-counts.
func recordDeltas(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, weekTick int64, net map[string]int, moraleDelta int, writeMorale bool) error {
	if len(net) == 0 && (!writeMorale || moraleDelta == 0) {
		return nil
	}
	for key, delta := range net {
		if delta == 0 {
			continue
		}
		if err := upsertDelta(ctx, tx, playerID, weekTick, key, delta); err != nil {
			return err
		}
	}
	if writeMorale && moraleDelta != 0 {
		if err := upsertDelta(ctx, tx, playerID, weekTick, MoraleDeltaKey, moraleDelta); err != nil {
			return err
		}
	}
	return nil
}

func upsertDelta(ctx context.Context, tx pgx.Tx, playerID uuid.UUID, weekTick int64, key string, delta int) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO player.player_attribute_changes (player_id, applied_week, attribute_key, delta)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (player_id, applied_week, attribute_key)
		DO UPDATE SET delta = EXCLUDED.delta, recorded_at = now()`,
		playerID, weekTick, key, delta); err != nil {
		return fmt.Errorf("delta %s: %w", key, err)
	}
	return nil
}

// lastMoraleValue returns the most recently recorded morale value index (in
// 0..100 points) and true, or 0/false when the player has no recorded baseline
// yet. The value index is rebuilt as current_k = prior_k + sum of intervening
// swings, so each week's delta is the true distance from its own baseline.
func lastMoraleValue(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) (int, bool) {
	var value int
	err := tx.QueryRow(ctx, `
		SELECT value
		FROM (
			SELECT applied_week,
			       sum(delta) OVER (ORDER BY applied_week) AS value
			FROM player.player_attribute_changes
			WHERE player_id = $1 AND attribute_key = $2
		) t
		ORDER BY applied_week DESC
		LIMIT 1`, playerID, MoraleDeltaKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false
	}
	if err != nil {
		return 0, false // never fail the week over a parallelism race on the log
	}
	return value, true
}

// moraleSwing computes a player's weekly morale swing in integer points
// (0 bad .. 100 great) relative to their last recorded value. writeMorale is
// false until a prior baseline exists — the seed week records nothing, so a
// brand-new academy graduate doesn't show a fake "changed" badge.
func moraleSwing(lastValue int, hasBaseline bool, current float64) (int, bool) {
	if !hasBaseline {
		return 0, false
	}
	currentIdx := int(math.Round(math.Min(1, math.Max(0, current)) * 100))
	return currentIdx - lastValue, true
}
