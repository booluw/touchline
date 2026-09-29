package competition

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// ---------------------------------------------------------------------------
// Knockout result application (the format branch of ApplyResult)
// ---------------------------------------------------------------------------

// applyKnockoutResult records a decided cup tie and advances the bracket. The
// fixture is already stamped 'completed' with its score (ApplyResult does that
// for every format); this step moves entries to qualified/eliminated/champion,
// closes the cup season on the final, and — when the round is the last
// unapplied one — materializes the next round in the same transaction. It
// never writes competition.standings and never triggers country rollover.
func (s *Service) applyKnockoutResult(ctx context.Context, tx pgx.Tx, fixtureID uuid.UUID,
	worldID, cupID uuid.UUID, homeClub, awayClub uuid.UUID, home, away int) error {
	if home == away {
		return ErrCupDraw
	}
	winnerClub, loserClub := homeClub, awayClub
	if away > home {
		winnerClub, loserClub = awayClub, homeClub
	}

	season, err := s.activeSeason(ctx, tx, cupID, worldID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'qualified'
		WHERE season_id = $1 AND club_id = $2`, season.ID, winnerClub); err != nil {
		return fmt.Errorf("cup entry qualified: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'eliminated'
		WHERE season_id = $1 AND club_id = $2`, season.ID, loserClub); err != nil {
		return fmt.Errorf("cup entry eliminated: %w", err)
	}

	// The round's other ties may still be pending; advance only when this was
	// the last unapplied fixture of the round.
	var round int
	if err := tx.QueryRow(ctx,
		`SELECT matchday FROM match.fixtures WHERE id = $1`, fixtureID).Scan(&round); err != nil {
		return fmt.Errorf("cup fixture round: %w", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.fixtures
		WHERE competition_id = $1 AND matchday = $2 AND standings_applied_at IS NULL`,
		cupID, round).Scan(&remaining); err != nil {
		return fmt.Errorf("cup round remaining: %w", err)
	}
	if remaining > 0 {
		return nil
	}

	// The final is exactly two clubs (one tie, no bye). A 3-team round also has
	// a single tie but a bye too — a semifinal, not the final.
	var qual json.RawMessage
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(qualification_rules, '{}'::jsonb) FROM competition.competition_rules
		 WHERE competition_id = $1`, cupID).Scan(&qual); err != nil {
		return fmt.Errorf("cup plan: %w", err)
	}
	plan, ok := planFromQual(qual)
	if !ok || len(plan.Ladder) == 0 {
		return fmt.Errorf("cup campaign plan missing: %w", ErrStagingInvalid)
	}
	if round-1 >= len(plan.Ladder) {
		return fmt.Errorf("cup ladder exhausted at round %d: %w", round, ErrStagingInvalid)
	}
	if plan.Ladder[round-1].N == 2 {
		if _, err := tx.Exec(ctx, `
			UPDATE competition.competition_entries SET status = 'champion'
			WHERE season_id = $1 AND club_id = $2`, season.ID, winnerClub); err != nil {
			return fmt.Errorf("cup champion: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE competition.seasons SET status = 'completed', end_date = now()
			WHERE id = $1`, season.ID); err != nil {
			return fmt.Errorf("complete cup season: %w", err)
		}
		if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
			WorldID:   worldID,
			EventType: "CUP_COMPLETED",
			Payload: mustJSON(map[string]any{
				"competition_id":   cupID,
				"season_id":        season.ID,
				"champion_club_id": winnerClub,
			}),
		}); err != nil {
			return err
		}
		return nil
	}

	// Materialize the next round from the advancing set, driven by the stored
	// ladder so landing-round sizes and the join trigger stay exact.
	nextIdx := round - 1 + 1 // round is 1-based; ladder[0] is round 1
	if nextIdx >= len(plan.Ladder) {
		return fmt.Errorf("cup ladder exhausted at round %d: %w", round, ErrStagingInvalid)
	}
	next := plan.Ladder[nextIdx]

	advancing, err := cupAdvancing(ctx, tx, season.ID, cupID, round)
	if err != nil {
		return err
	}

	// Join: the top-N late entrants join exactly when X survivors remain and
	// the join has not yet materialized.
	n, x := stagingFromRules(qual)
	joined := plan.Joined
	if len(advancing) == x && n > 0 && !joined {
		advancing = append(advancing, plan.TopNClubIDs...)
		if err := s.setEntryQualified(ctx, tx, season.ID, plan.TopNClubIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE competition.competition_rules
			SET qualification_rules = jsonb_set(
				COALESCE(qualification_rules, '{}'::jsonb),
				'{campaign,joined}', 'true'::jsonb)
			WHERE competition_id = $1`, cupID); err != nil {
			return fmt.Errorf("persist join flag: %w", err)
		}
	}

	if len(advancing) != next.N {
		return fmt.Errorf("cup round %d advancing set %d, ladder plans %d: %w",
			round, len(advancing), next.N, ErrStagingInvalid)
	}
	sort.Slice(advancing, func(i, j int) bool { return advancing[i].String() < advancing[j].String() })

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return fmt.Errorf("load world seed: %w", err)
	}
	var worldRef time.Time
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1`, worldID).Scan(&worldRef); err != nil {
		return fmt.Errorf("load world ref: %w", err)
	}
	if err := s.materializeRound(ctx, tx, worldID, cupID, season.ID, next, advancing, worldRef, seed); err != nil {
		return err
	}
	return nil
}

// cupAdvancing returns the clubs that advance from a completed round: the
// winners of the round's ties plus the clubs that carried a bye (cup_bracket
// is_bye). Winners are read from the applied fixtures; for dedup with byes the
// set is unioned and canonically sorted by the caller.
func cupAdvancing(ctx context.Context, tx pgx.Tx, seasonID uuid.UUID, cupID uuid.UUID, round int) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT CASE WHEN ht_score > at_score THEN home_club_id ELSE away_club_id END
		FROM match.fixtures
		WHERE competition_id = $1 AND matchday = $2 AND standings_applied_at IS NOT NULL`, cupID, round)
	if err != nil {
		return nil, fmt.Errorf("cup winners: %w", err)
	}
	out := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan cup winner: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byes, err := tx.Query(ctx, `
		SELECT club_id FROM competition.cup_bracket
		WHERE season_id = $1 AND round = $2 AND is_bye`, seasonID, round)
	if err != nil {
		return nil, fmt.Errorf("cup byes: %w", err)
	}
	defer byes.Close()
	for byes.Next() {
		var id uuid.UUID
		if err := byes.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan cup bye: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, byes.Err()
}

// setEntryQualified flips a batch of entries to 'qualified' (the late entrants
// when they join the bracket).
func (s *Service) setEntryQualified(ctx context.Context, tx pgx.Tx, seasonID uuid.UUID, clubs []uuid.UUID) error {
	if len(clubs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'qualified'
		WHERE season_id = $1 AND club_id = ANY($2::uuid[])`, seasonID, clubs); err != nil {
		return fmt.Errorf("cup entrants qualified: %w", err)
	}
	return nil
}
