// Free-agent signing and release (A08): a free agent can be signed to an
// active professional contract + wage commitment (mirroring the academy
// signing pattern), and a club can terminate a contract and return the player
// to the country pool. Both run inside the caller's transaction with the
// event-log write in the same tx (OPD-23).
package playerpool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/pkg/eventbus"
)

// EventPlayerReleased fires when a club terminates a player's contract and
// returns them to the country free-agent pool (A08).
const EventPlayerReleased = "PLAYER_RELEASED"

// Sentinel errors for sign/release.
var (
	// ErrNotFreeAgent means the player is not currently a free agent.
	ErrNotFreeAgent = errors.New("player is not a free agent")
	// ErrStreetUnder18 means a street-origin player is younger than 18 and
	// cannot be signed (A07 eligibility gate mirrored at signing time).
	ErrStreetUnder18 = errors.New("street-origin player must be at least 18 to sign")
	// ErrClubCannotAfford means the requested wage exceeds the club's remaining
	// committed-wage capacity for the season.
	ErrClubCannotAfford = errors.New("club cannot afford the requested wage")
	// ErrNoActiveContract means release found no active contract to terminate.
	ErrNoActiveContract = errors.New("player has no active contract to release")
)

// SignFreeAgent signs a free agent to an active professional contract inside
// the caller's transaction: club assignment, contract + wage commitment rows,
// and PLAYER_SIGNED event. Guards: the player must be a free agent, street-
// origin players must be 18+, and the requested weekly wage must fit the
// club's remaining wage budget for its current season.
func SignFreeAgent(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID, playerID, clubID uuid.UUID, weeklyWage int64, years int, ref time.Time,
) error {
	if weeklyWage <= 0 {
		return fmt.Errorf("sign free agent: weekly_wage must be positive")
	}
	if years <= 0 {
		return fmt.Errorf("sign free agent: years must be positive")
	}

	var status string
	var club *uuid.UUID
	var origin string
	var age int
	if err := tx.QueryRow(ctx, `
		SELECT p.status, p.club_id, p.origin,
		       COALESCE(EXTRACT(YEAR FROM age($2::date, pe.date_of_birth::date))::int, 0)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		WHERE p.id = $1 AND p.world_id = $2`,
		playerID, worldID, ref).Scan(&status, &club, &origin, &age); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("sign free agent: player not found")
		}
		return fmt.Errorf("sign free agent: load player: %w", err)
	}
	switch err := checkSignEligible(status, club, origin, age); {
	case err != nil:
		return fmt.Errorf("sign free agent: %w", err)
	}

	affordable, err := wageFits(ctx, tx, clubID, weeklyWage)
	if err != nil {
		return err
	}
	if !affordable {
		return fmt.Errorf("sign free agent: %w", ErrClubCannotAfford)
	}

	start := ref
	end := ref.AddDate(years, 0, 0)
	var contractID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO player.contracts
			(player_id, club_id, contract_type, weekly_wage, signing_bonus, start_date, end_date, status)
		VALUES ($1, $2, 'senior', $3, 0, $4, $5, 'active')
		RETURNING id`,
		playerID, clubID, weeklyWage, start, end).Scan(&contractID); err != nil {
		return fmt.Errorf("sign free agent: insert contract: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO finance.wage_commitments (contract_id, club_id, weekly_wage, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5)`,
		contractID, clubID, weeklyWage, start, end); err != nil {
		return fmt.Errorf("sign free agent: insert wage commitment: %w", err)
	}
	if _, err := finance.EnsureAccount(ctx, tx, worldID, clubID); err != nil {
		return fmt.Errorf("sign free agent: ensure finance account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE player.players SET club_id = $2, status = 'active' WHERE id = $1`,
		playerID, clubID); err != nil {
		return fmt.Errorf("sign free agent: assign club: %w", err)
	}

	actor := "system"
	payload, _ := json.Marshal(map[string]any{
		"player_id":   playerID,
		"club_id":     clubID,
		"weekly_wage": weeklyWage,
		"origin":      origin,
		"years":       years,
	})
	if err := eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: EventPlayerSigned,
		ActorType: &actor,
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("sign free agent: record %s: %w", EventPlayerSigned, err)
	}
	return nil
}

// checkSignEligible is the pure, testable gate mirroring A07's squad
// eligibility for the signing command: the player must be a free agent with
// no club, and street-origin players must be at least 18.
func checkSignEligible(status string, club *uuid.UUID, origin string, age int) error {
	if status != "free_agent" || club != nil {
		return ErrNotFreeAgent
	}
	if origin == "street" && age < 18 {
		return ErrStreetUnder18
	}
	return nil
}

// ReleasePlayer terminates the player's active contracts, ends their wage
// commitments today, returns the player to the country pool (club_id NULL,
// status 'free_agent'), and emits PLAYER_RELEASED.
func ReleasePlayer(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	playerID uuid.UUID, reason string,
) error {
	var club *uuid.UUID
	var origin string
	var worldID uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT p.world_id, p.status, p.club_id, p.origin
		FROM player.players p WHERE p.id = $1`,
		playerID).Scan(&worldID, &status, &club, &origin); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("release player: player not found")
		}
		return fmt.Errorf("release player: load player: %w", err)
	}
	if club == nil {
		return fmt.Errorf("release player: %w", ErrNoActiveContract)
	}
	previousClub := *club

	tag, err := tx.Exec(ctx, `
		UPDATE player.contracts SET status = 'terminated', end_date = CURRENT_DATE
		WHERE player_id = $1 AND status = 'active'`, playerID)
	if err != nil {
		return fmt.Errorf("release player: terminate contracts: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("release player: %w", ErrNoActiveContract)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE finance.wage_commitments w
		SET end_date = CURRENT_DATE
		FROM player.contracts c
		WHERE w.contract_id = c.id AND c.player_id = $1
		  AND c.status = 'terminated'`, playerID); err != nil {
		return fmt.Errorf("release player: end wage commitments: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE player.players SET club_id = NULL, status = 'free_agent' WHERE id = $1`,
		playerID); err != nil {
		return fmt.Errorf("release player: return to pool: %w", err)
	}

	actor := "system"
	payload, _ := json.Marshal(map[string]any{
		"player_id":        playerID,
		"previous_club_id": previousClub,
		"reason":           reason,
		"origin":           origin,
	})
	if err := eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: EventPlayerReleased,
		ActorType: &actor,
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("release player: record %s: %w", EventPlayerReleased, err)
	}
	return nil
}

// wageFits reports whether adding weeklyWage keeps the club's committed wage
// bill at or under its allocated wage budget for the current season. Clubs
// without a seeded budget are treated as unconstrained (no budget row yet).
func wageFits(ctx context.Context, tx pgx.Tx, clubID uuid.UUID, weeklyWage int64) (bool, error) {
	var allocated, committed int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(allocated_amount)::bigint, 0), COALESCE(SUM(committed_amount)::bigint, 0)
		FROM finance.budgets b
		WHERE b.club_id = $1 AND b.budget_type = 'wage'
		  AND b.season = (SELECT COALESCE(MAX(season), 0) FROM finance.budgets WHERE club_id = $1)`,
		clubID).Scan(&allocated, &committed)
	if err != nil {
		return false, fmt.Errorf("sign free agent: check wage budget: %w", err)
	}
	if allocated == 0 {
		return true, nil // no seeded budget — permissive during bootstrap
	}
	return committed+weeklyWage <= allocated, nil
}
