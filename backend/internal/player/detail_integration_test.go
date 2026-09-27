//go:build integration

package player_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/squad"
	internalworld "github.com/touchline/backend/internal/world"
)

// TestPlayerDetailCarriesAbilityAndCareer is the service contract of the player
// detail card (IM20): identity, the same two ability numbers the squad roster
// shows, the career totals, and the wage **only** for the caller's own club.
func TestPlayerDetailCarriesAbilityAndCareer(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "player-detail")
	ctx := context.Background()
	playerID := pickPlayer(t, pool, tw.HumanClub)

	// Pin the identity fields the assertions below read, so the test does not
	// depend on generated values.
	if _, err := pool.Exec(ctx,
		`UPDATE player.players SET primary_position = 'ST' WHERE id = $1`, playerID); err != nil {
		t.Fatalf("set position: %v", err)
	}
	setWage(t, pool, playerID, 4242.50)

	// Pin the ability block to known per-category values so the expected means
	// are exact, and check the card against the EAV rather than against itself.
	if _, err := pool.Exec(ctx, `DELETE FROM player.player_attributes WHERE player_id = $1`, playerID); err != nil {
		t.Fatalf("clear attributes: %v", err)
	}
	values := map[string][]int{
		"technical":   {80, 90},     // mean 85
		"physical":    {70},         // mean 70
		"mental":      {60, 70, 80}, // mean 70
		"tactical":    {65, 66},     // mean 66 (round half up)
		"positional":  {55, 56, 57}, // mean 56
		"goalkeeping": nil,          // an outfield player carries no goalkeeping keys → 0
	}
	for category, vals := range values {
		for _, v := range vals {
			if _, err := pool.Exec(ctx, `
				INSERT INTO player.player_attributes (player_id, attribute_category, key, value)
				VALUES ($1, $2, $3, $4)`, playerID, category, category+".0", v); err != nil {
				t.Fatalf("seed %s: %v", category, err)
			}
		}
	}

	// Two completed-match appearances: one rated, one not (pre-attribution).
	seedAppearance(t, pool, playerID, tw.WorldID, tw.HumanClub, 90, 8, 2, 1)
	seedUnratedAppearance(t, pool, playerID, tw.WorldID, tw.HumanClub, 60)

	card, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, playerID)
	if err != nil {
		t.Fatalf("player detail: %v", err)
	}
	if card.Player == nil || card.Player.ID != playerID {
		t.Fatalf("player ref = %+v, want %s", card.Player, playerID)
	}
	if card.DisplayName == "" || card.Club == nil || card.Club.ID != tw.HumanClub {
		t.Fatalf("identity incomplete: name=%q club=%+v", card.DisplayName, card.Club)
	}
	if card.Position != "ST" {
		t.Fatalf("position = %q, want ST", card.Position)
	}

	// Ability: the EAV means, and the canonical overall recipe on top of them.
	want := rosterCategoryMeans(t, pool, playerID)
	if card.Attributes != want {
		t.Fatalf("attributes\n got: %+v\nwant: %+v", card.Attributes, want)
	}
	if card.Attributes.Tactical != 66 || card.Attributes.Goalkeeping != 0 {
		t.Fatalf("category means = %+v, want tactical 66 and goalkeeping 0", card.Attributes)
	}
	wantOverall := squad.PositionalOverall(card.Position, squad.AttributeSnapshot{
		Technical: card.Attributes.Technical, Physical: card.Attributes.Physical,
		Mental: card.Attributes.Mental, Tactical: card.Attributes.Tactical,
		Goalkeeping: card.Attributes.Goalkeeping, Positional: card.Attributes.Positional,
	})
	if card.Overall != wantOverall {
		t.Fatalf("overall = %d, want %d", card.Overall, wantOverall)
	}
	if card.Overall < 1 || card.Overall > 99 {
		t.Fatalf("overall = %d, outside 1..99", card.Overall)
	}

	// Career: both matches count as appearances, only the rated one moves the
	// average, and the goals/assists sum.
	if card.Career.Appearances != 2 || card.Career.Goals != 3 || card.Career.Assists != 2 {
		t.Fatalf("career = %+v, want 2 appearances / 3 goals / 2 assists", card.Career)
	}
	if card.Career.AverageRating != 8 {
		t.Fatalf("average rating = %v, want 8 (the unrated match must not drag it)", card.Career.AverageRating)
	}

	// The player's own club's manager sees the wage.
	if card.WeeklyWage == nil || *card.WeeklyWage <= 0 {
		t.Fatalf("weekly wage = %v, want the own-club wage", card.WeeklyWage)
	}
	ownWage := *card.WeeklyWage
	if ownWage != 4242 {
		t.Fatalf("weekly wage = %d, want 4242 (the contract's 4242.50 truncated like the club finances read)", ownWage)
	}
}

// TestPlayerDetailHidesOtherClubsWage pins the one private field: any player in
// the caller's world is readable (the page is for opponents and targets too), but
// a rival's contract is not the caller's business.
func TestPlayerDetailHidesOtherClubsWage(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "player-detail-wage")
	ctx := context.Background()
	rival := pickPlayer(t, pool, tw.AIOneClub)
	// The rival really does have a contract, so a withheld wage is a decision
	// rather than an accident of missing data.
	setWage(t, pool, rival, 9999)

	card, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, rival)
	if err != nil {
		t.Fatalf("player detail: %v", err)
	}
	if card.Club == nil || card.Club.ID != tw.AIOneClub {
		t.Fatalf("club = %+v, want the rival's club (a world player must be readable)", card.Club)
	}
	if card.WeeklyWage != nil {
		t.Fatalf("weekly wage = %d for another club's player; it must be withheld", *card.WeeklyWage)
	}
}

// TestPlayerDetailIsWorldScoped pins the isolation rule: a player of another
// world is reported exactly like a player that does not exist, so the endpoint
// can never be used to discover another world's players.
func TestPlayerDetailIsWorldScoped(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "player-detail-scope")
	ctx := context.Background()
	playerID := pickPlayer(t, pool, tw.HumanClub)

	// players.world_id references world.worlds, so the "another world" has to be a
	// real second world — a bare uuid.New() would fail the foreign key and the
	// test would never reach the assertion it exists for.
	elsewhere, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "player-detail-elsewhere")
	if err != nil {
		t.Fatalf("create second world: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE player.players SET world_id = $2 WHERE id = $1`, playerID, elsewhere.ID); err != nil {
		t.Fatalf("move player to another world: %v", err)
	}
	if _, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, playerID); !errors.Is(err, player.ErrPlayerNotFound) {
		t.Fatalf("err = %v, want ErrPlayerNotFound for a foreign-world player", err)
	}
	if _, err := svc.GetPlayerDetail(ctx, tw.WorldID, tw.HumanMgr, uuid.New()); !errors.Is(err, player.ErrPlayerNotFound) {
		t.Fatalf("err = %v, want ErrPlayerNotFound for an unknown player", err)
	}
}

// setWage pins the player's active contract wage so the assertions read a known
// number instead of a generated one. Exactly one active contract is expected, and
// a silent zero-row update would surface much later as a baffling "wage is nil".
func setWage(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID, wage float64) {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `
		UPDATE player.contracts SET weekly_wage = $2
		WHERE player_id = $1 AND status = 'active'`, playerID, wage)
	if err != nil {
		t.Fatalf("set wage of %s: %v", playerID, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("set wage of %s: updated %d active contracts, want 1", playerID, tag.RowsAffected())
	}
}

// seedAppearance writes one rated appearance for the player, tied to a completed
// match so the career aggregate reads it exactly as the product does.
func seedAppearance(t *testing.T, pool *pgxpool.Pool, playerID, worldID, clubID uuid.UUID, minutes, rating, goals, assists int) {
	t.Helper()
	insertAppearance(t, pool, playerID, seedCompletedMatch(t, pool, worldID, clubID), minutes, &rating, goals, assists)
}

// seedUnratedAppearance writes an appearance with no rating, the shape a match
// that predates the attribution pass leaves behind. It must count as an
// appearance and must not move the rating average.
func seedUnratedAppearance(t *testing.T, pool *pgxpool.Pool, playerID, worldID, clubID uuid.UUID, minutes int) {
	t.Helper()
	insertAppearance(t, pool, playerID, seedCompletedMatch(t, pool, worldID, clubID), minutes, nil, 1, 1)
}

func insertAppearance(t *testing.T, pool *pgxpool.Pool, playerID, matchID uuid.UUID, minutes int, rating *int, goals, assists int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO player.player_appearances (player_id, match_id, started, minutes, rating, goals, assists)
		VALUES ($1, $2, true, $3, $4, $5, $6)
		ON CONFLICT (player_id, match_id) DO UPDATE
		SET minutes = EXCLUDED.minutes, rating = EXCLUDED.rating,
		    goals = EXCLUDED.goals, assists = EXCLUDED.assists`,
		playerID, matchID, minutes, rating, goals, assists); err != nil {
		t.Fatalf("seed appearance in %s: %v", matchID, err)
	}
}
