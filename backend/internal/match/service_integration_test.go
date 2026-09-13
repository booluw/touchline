//go:build integration

package match

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/playergen"
)

const replayFixtureID = "22222222-2222-2222-2222-222222222222"

// worldFor builds a fresh world with a human-managed home club (persisted
// lineup) plus an AI away club, and returns their ids.
func worldFor(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldName, homeName, awayName string) (worldID, homeID, awayID uuid.UUID) {
	t.Helper()
	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, worldName)
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	worldID = w.ID

	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, worldID, homeName, "")
	if err != nil {
		t.Fatalf("bootstrap home: %v", err)
	}
	homeID = res.ClubID

	// The user-managed club owns its XI: flip the control flag and persist a
	// full 11-man lineup in the shared slot order (any 11 distinct players are
	// a legal manager set; position-fit fallbacks are covered unit-side).
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET is_ai_controlled = FALSE WHERE id = $1`, homeID); err != nil {
		t.Fatalf("make home human: %v", err)
	}
	rows, err := pool.Query(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY squad_number LIMIT 11`, homeID)
	if err != nil {
		t.Fatalf("load home players: %v", err)
	}
	var slot int
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan player: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO club.club_lineups (slot, club_id, player_id) VALUES ($1, $2, $3)`,
			slot, homeID, id); err != nil {
			t.Fatalf("write lineup slot %d: %v", slot, err)
		}
		slot++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate players: %v", err)
	}
	if slot != 11 {
		t.Fatalf("lineup has %d slots, want 11", slot)
	}

	awayID = generateAIClub(t, pool, ctx, worldID, awayName, 1234)
	return worldID, homeID, awayID
}

// generateAIClub persists one AI club inside its own tx with a fixed factory
// seed, so two calls with the same parameters produce identical squads.
func generateAIClub(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldID uuid.UUID, name string, seed int64) uuid.UUID {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin club tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	generator, natPool, err := bootstrap.LoadPools(ctx, tx)
	if err != nil {
		t.Fatalf("load name pools: %v", err)
	}
	factory := playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(seed))).
		WithRegistry(playergen.NewNameRegistry())
	club, err := bootstrap.GenerateAIClub(ctx, tx, worldID, name, "", "england", factory)
	if err != nil {
		t.Fatalf("generate %s: %v", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit club tx: %v", err)
	}
	return club.ClubID
}

// insertFixture writes a competition + one scheduled fixture. Pass a zero id to
// let the DB assign one.
func insertFixture(t *testing.T, pool *pgxpool.Pool, ctx context.Context, id uuid.UUID, worldID, homeID, awayID uuid.UUID, scheduledAt time.Time) uuid.UUID {
	t.Helper()
	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&compID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}

	var fixtureID uuid.UUID
	if id == uuid.Nil {
		err := pool.QueryRow(ctx, `
			INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
			VALUES ($1, $2, $3, $4, 1, $5, 'scheduled') RETURNING id`,
			worldID, compID, homeID, awayID, scheduledAt).Scan(&fixtureID)
		if err != nil {
			t.Fatalf("insert fixture: %v", err)
		}
	} else {
		err := pool.QueryRow(ctx, `
			INSERT INTO match.fixtures (id, world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
			VALUES ($1, $2, $3, $4, $5, 1, $6, 'scheduled') RETURNING id`,
			id, worldID, compID, homeID, awayID, scheduledAt).Scan(&fixtureID)
		if err != nil {
			t.Fatalf("insert fixture: %v", err)
		}
	}
	return fixtureID
}

func newMatchService(pool *pgxpool.Pool) *Service {
	return NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
}

func TestPlayFixtureIntegration(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	worldID, homeID, awayID := worldFor(t, pool, ctx, "match-world", "Harbour FC", "Riverside Albion")
	fixtureID := insertFixture(t, pool, ctx, uuid.Nil, worldID, homeID, awayID, time.Date(2030, 6, 1, 15, 0, 0, 0, time.UTC))

	svc := newMatchService(pool)
	mr, err := svc.PlayFixture(ctx, fixtureID)
	if err != nil {
		t.Fatalf("play fixture: %v", err)
	}

	// Match row: completed, valid seed, engine-versioned.
	var (
		homeScore, awayScore int
		status, version      string
		seed                 int64
		ended                bool
	)
	if err := pool.QueryRow(ctx, `
		SELECT home_score, away_score, status, seed, engine_version,
		       (ended_at IS NOT NULL AND started_at IS NOT NULL)
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&homeScore, &awayScore, &status, &seed, &version, &ended); err != nil {
		t.Fatalf("read match row: %v", err)
	}
	if homeScore < 0 || awayScore < 0 || status != "completed" || !ended {
		t.Fatalf("match row invalid: %d-%d %s ended=%v", homeScore, awayScore, status, ended)
	}
	if version == "" {
		t.Fatalf("engine version missing")
	}
	if seed != fixtureSeed(fixtureID) {
		t.Fatalf("persisted seed %d != fixture seed %d", seed, fixtureSeed(fixtureID))
	}

	// Fixture flips to completed; the same call is a read-only no-op.
	var fixtureStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM match.fixtures WHERE id = $1`, fixtureID).Scan(&fixtureStatus); err != nil {
		t.Fatalf("read fixture status: %v", err)
	}
	if fixtureStatus != "completed" {
		t.Fatalf("fixture status = %q, want completed", fixtureStatus)
	}

	// Events: contiguous seq, legal minutes, kickoff→full_time, and every
	// goal reconciled against the scoreline.
	evs, err := svc.GetMatchEvents(ctx, mr.Match.ID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no match events persisted")
	}
	if evs[0].Type != "kickoff" {
		t.Fatalf("first event %q, want kickoff", evs[0].Type)
	}
	if evs[len(evs)-1].Type != "full_time" {
		t.Fatalf("last event %q, want full_time", evs[len(evs)-1].Type)
	}
	for i, e := range evs {
		if i == 0 {
			if e.Sequence != evs[0].Sequence {
				t.Fatalf("sequence %d, want %d", e.Sequence, evs[0].Sequence)
			}
		} else if e.Sequence != evs[i-1].Sequence+1 {
			t.Fatalf("sequence %d, want %d after previous", e.Sequence, evs[i-1].Sequence)
		}
		if e.Minute < 0 || e.Minute > 180 {
			t.Fatalf("minute %d out of range", e.Minute)
		}
	}

	var goalsHome, goalsAway int
	if err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE club_id = $2 AND event_type = 'goal'),
			COUNT(*) FILTER (WHERE club_id = $3 AND event_type = 'goal')
		FROM match.match_events WHERE match_id = $1`,
		mr.Match.ID, homeID, awayID).Scan(&goalsHome, &goalsAway); err != nil {
		t.Fatalf("count goals: %v", err)
	}
	if goalsHome != homeScore || goalsAway != awayScore {
		t.Fatalf("goal events %d-%d vs scoreline %d-%d", goalsHome, goalsAway, homeScore, awayScore)
	}

	// The cast is legal: any event's player must belong to the event's club
	// (kickoff/card events may lack a player, never a foreign one).
	var illegalCast int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.match_events e
		WHERE e.match_id = $1 AND e.player_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM player.players p WHERE p.id = e.player_id AND p.club_id = e.club_id)`,
		mr.Match.ID).Scan(&illegalCast); err != nil {
		t.Fatalf("cast legality: %v", err)
	}
	if illegalCast != 0 {
		t.Fatalf("%d events cast to a wrong-club player", illegalCast)
	}

	// Both clubs now own a form row stamped with the W/D/L read model.
	var formRows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.form_state WHERE club_id IN ($1, $2)`, homeID, awayID).Scan(&formRows); err != nil {
		t.Fatalf("count form rows: %v", err)
	}
	if formRows != 2 {
		t.Fatalf("form rows = %d, want 2", formRows)
	}

	// MATCH_PLAYED is logged with the replay seed; every warning uses the
	// club as its actor domain.
	var matchPlayed struct {
		Seed int64
	}
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(random_seed, 0) FROM world.events
		WHERE world_id = $1 AND event_type = 'MATCH_PLAYED' AND payload->>'match_id' = $2`,
		worldID, mr.Match.ID.String()).Scan(&matchPlayed.Seed); err != nil {
		t.Fatalf("find MATCH_PLAYED: %v", err)
	}
	if matchPlayed.Seed != seed {
		t.Fatalf("MATCH_PLAYED seed %d != match seed %d", matchPlayed.Seed, seed)
	}

	// Idempotency: replaying the completed fixture mutates nothing.
	first := captureSnapshot(t, pool, ctx, fixtureID)
	second, err := svc.PlayFixture(ctx, fixtureID)
	if err != nil {
		t.Fatalf("replay completed fixture: %v", err)
	}
	if second.Match.HomeGoals != homeScore || second.Match.AwayGoals != awayScore {
		t.Fatalf("idempotent replay drifted: %d-%d vs %d-%d",
			second.Match.HomeGoals, second.Match.AwayGoals, homeScore, awayScore)
	}
	after := captureSnapshot(t, pool, ctx, fixtureID)
	if !reflect.DeepEqual(first, after) {
		t.Fatalf("replay mutated state:\nbefore:\n%s\nafter:\n%s", first, after)
	}
}

// TestPlayFixtureReplaysByteIdentical is the orchestration-level replay
// contract: wipe the produced match material and re-simulate the SAME fixture
// from the SAME world state. Scores and the full cast event feed must come
// back byte-identical (selection, casting, engine, form all derive from the
// fixture id + persisted state + world tick).
func TestPlayFixtureReplaysByteIdentical(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	worldID, homeID, awayID := worldFor(t, pool, ctx, "match-replay", "Port City", "Ocean Bay")
	fixtureID := insertFixture(t, pool, ctx, uuid.MustParse(replayFixtureID), worldID, homeID, awayID,
		time.Date(2030, 6, 1, 15, 0, 0, 0, time.UTC))

	svc := newMatchService(pool)
	if _, err := svc.PlayFixture(ctx, fixtureID); err != nil {
		t.Fatalf("first sim: %v", err)
	}
	first := captureSnapshot(t, pool, ctx, fixtureID)

	// Reset the match material exactly as a saved world would re-run the
	// matchday — same fixture, same squads, same form & tick.
	if _, err := pool.Exec(ctx,
		`DELETE FROM match.match_events WHERE match_id = (SELECT id FROM match.matches WHERE fixture_id = $1)`,
		fixtureID); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM match.matches WHERE fixture_id = $1`, fixtureID); err != nil {
		t.Fatalf("delete match: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE match.fixtures SET status = 'scheduled' WHERE id = $1`, fixtureID); err != nil {
		t.Fatalf("revert fixture: %v", err)
	}
	// Restore the pre-match world state (a saved world replays from the
	// moment before the matchday): form had not yet been applied.
	if _, err := pool.Exec(ctx, `
		DELETE FROM club.form_state
		WHERE club_id IN (SELECT home_club_id FROM match.fixtures WHERE id = $1)
		   OR club_id IN (SELECT away_club_id FROM match.fixtures WHERE id = $1)`, fixtureID); err != nil {
		t.Fatalf("clear form: %v", err)
	}

	if _, err := svc.PlayFixture(ctx, fixtureID); err != nil {
		t.Fatalf("second sim: %v", err)
	}
	second := captureSnapshot(t, pool, ctx, fixtureID)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("re-simulation diverged:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// captureSnapshot reads the full deterministic material of one fixture: the
// match row, every cast event, and both clubs' form state.
func captureSnapshot(t *testing.T, pool *pgxpool.Pool, ctx context.Context, fixtureID uuid.UUID) string {
	t.Helper()
	var s struct {
		home, away, status, version string
		seed                        int64
	}
	if err := pool.QueryRow(ctx, `
		SELECT home_score::text, away_score::text, status, seed, engine_version
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&s.home, &s.away, &s.status, &s.seed, &s.version); err != nil {
		t.Fatalf("read match: %v", err)
	}
	out := fmt.Sprintf("MATCH %s|%s|%s|%d|%s\n", s.home, s.away, s.status, s.seed, s.version)

	rows, err := pool.Query(ctx, `
		SELECT sequence, minute, event_type,
		       COALESCE(club_id::text, ''), COALESCE(player_id::text, ''),
		       COALESCE(related_player_id::text, ''), COALESCE(detail::text, '{}')
		FROM match.match_events
		WHERE match_id = (SELECT id FROM match.matches WHERE fixture_id = $1)
		ORDER BY sequence`, fixtureID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	for rows.Next() {
		var seq, min int
		var typ, club, player, related, detail string
		if err := rows.Scan(&seq, &min, &typ, &club, &player, &related, &detail); err != nil {
			rows.Close()
			t.Fatalf("scan event: %v", err)
		}
		out += fmt.Sprintf("EV %d|%d|%s|%s|%s|%s|%s\n", seq, min, typ, club, player, related, detail)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate events: %v", err)
	}

	fRows, err := pool.Query(ctx, `
		SELECT club_id::text, current_rating, last_updated_tick, COALESCE(form_string, '')
		FROM club.form_state
		WHERE club_id IN (SELECT home_club_id FROM match.fixtures WHERE id = $1)
		   OR club_id IN (SELECT away_club_id FROM match.fixtures WHERE id = $1)
		ORDER BY club_id`, fixtureID)
	if err != nil {
		t.Fatalf("query form: %v", err)
	}
	for fRows.Next() {
		var id string
		var rating float64
		var tick int64
		var str string
		if err := fRows.Scan(&id, &rating, &tick, &str); err != nil {
			fRows.Close()
			t.Fatalf("scan form: %v", err)
		}
		out += fmt.Sprintf("FORM %s|%f|%d|%s\n", id, rating, tick, str)
	}
	fRows.Close()
	if err := fRows.Err(); err != nil {
		t.Fatalf("iterate form: %v", err)
	}
	return out
}