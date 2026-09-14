//go:build integration

// Package testdb provides the shared integration-test harness: a Postgres
// pool with migrations applied, plus fixtures for auth/session tests. Mirrors
// the harness established in pkg/eventbus (TEST_DATABASE_URL or testcontainers).
package testdb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
)

// New returns a pooled, migrated, truncated database for integration tests.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		req := testcontainers.ContainerRequest{
			Image:        "postgres:16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "touchline",
				"POSTGRES_PASSWORD": "touchline",
				"POSTGRES_DB":       "touchline",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections"),
		}
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("start postgres container: %v", err)
		}
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })

		host, err := container.Host(ctx)
		if err != nil {
			t.Fatalf("container host: %v", err)
		}
		port, err := container.MappedPort(ctx, "5432/tcp")
		if err != nil {
			t.Fatalf("container port: %v", err)
		}
		dbURL = fmt.Sprintf("postgres://touchline:touchline@%s:%s/touchline?sslmode=disable", host, port.Port())
	}

	m, err := migrate.New("file://../../migrations", dbURL)
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("apply migrations: %v", err)
	}
	_, _ = m.Close()

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(),
		`TRUNCATE TABLE river.river_job, world.events, world.worlds, world.countries,
		 manager.managers, manager.manager_history, manager.job_security_snapshots,
		 manager.manager_reputation_events, manager.job_offers, club.clubs, club.club_dna,
		 club.form_state,
		 competition.competitions, competition.seasons, competition.standings,
		 match.fixtures, match.matches, match.match_events, match.match_inputs,
		 auth.sessions, auth.users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test tables: %v", err)
	}
	return pool
}

// MakeAdmin promotes a user account to is_admin (for admin-route tests).
func MakeAdmin(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE auth.users SET is_admin = TRUE WHERE id = $1`, userID); err != nil {
		t.Fatalf("make admin: %v", err)
	}
}

// SeedRefData ensures ref.nationalities + ref.name_pool contain the minimal
// data a bootstrap/player-generation test needs (eng + br). It mirrors
// cmd/ref-seed's semantics: upsert nationalities, delete+reinsert the name
// pool for the seeded codes — so tests stay hermetic regardless of whether the
// full 21-code curated set has been seeded in this database.
func SeedRefData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	var codes = []string{"eng", "br"}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ref.nationalities (code, name, generation_weight) VALUES
			('eng', 'England', 10.0),
			('br',  'Brazil',  9.0)
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name, generation_weight = EXCLUDED.generation_weight`); err != nil {
		t.Fatalf("seed nationalities: %v", err)
	}

	firstNames := map[string][]string{ // code -> first names (curated data/names)
		"eng": {
			"Aaron", "Adam", "Alfie", "Archie", "Arthur", "Benjamin", "Charlie", "Daniel",
			"David", "Edward", "Ellis", "Felix", "George", "Harry", "Henry", "Jacob",
			"James", "Jamie", "Jack", "Joseph", "Joshua", "Leo", "Lewis", "Liam",
			"Logan", "Louis", "Luca", "Mason", "Max", "Michael", "Nathan", "Noah",
			"Oliver", "Oscar", "Owen", "Reece", "Riley", "Ryan", "Samuel", "Toby",
			"Tom", "Tommy", "Tyler", "William",
		},
		"br": {
			"Ana", "Beatriz", "Bruno", "Caio", "Carla", "Carlos", "Daniel", "Diego",
			"Eduardo", "Felipe", "Fernando", "Gabriel", "Gustavo", "Henrique", "Igor",
			"Joao", "Jonas", "Jorge", "Jose", "Julio", "Kaique", "Lucas", "Luana",
			"Luciano", "Marcelo", "Marcos", "Matheus", "Murilo", "Paula", "Patricia",
			"Paulo", "Pedro", "Rafael", "Renato", "Ricardo", "Rodrigo", "Samuel",
			"Thiago", "Vinicius", "Vitor",
		},
	}
	lastNames := map[string][]string{ // code -> last names (curated data/names)
		"eng": {
			"Adams", "Allen", "Anderson", "Atkinson", "Bailey", "Baker", "Ball", "Barker",
			"Barnes", "Bell", "Bennett", "Booth", "Brooks", "Brown", "Butler", "Carter",
			"Chapman", "Clarke", "Cole", "Collins", "Cook", "Cooper", "Cox", "Davies",
			"Davis", "Dixon", "Edwards", "Ellis", "Evans", "Fisher", "Foster", "Fox",
			"Gibson", "Graham", "Grant", "Green", "Griffiths", "Hall", "Harris", "Harrison",
			"Hayes", "Hill", "Hughes", "Hunter", "Jackson", "James", "Johnson", "Jones",
			"Kelly", "Lewis", "Marshall", "Martin", "Mason", "Matthews", "Miller", "Mitchell",
			"Moore", "Morgan", "Morris", "Murphy", "Murray", "Owen", "Parker", "Pearce",
			"Phillips", "Price", "Reed", "Richards", "Roberts", "Robinson", "Russell", "Shaw",
			"Simpson", "Smith", "Taylor", "Thompson", "Turner", "Walker", "Walsh", "Ward",
			"Watson", "Webb", "White", "Williams", "Wilson", "Wood", "Wright",
		},
		"br": {
			"Almeida", "Alves", "Araujo", "Barbosa", "Campos", "Cardoso", "Carvalho",
			"Castilho", "Costa", "Dias", "Duarte", "Fernandes", "Ferreira", "Freitas",
			"Gomes", "Lima", "Lopes", "Martins", "Melo", "Monteiro", "Moraes", "Moreira",
			"Nascimento", "Nunes", "Oliveira", "Pereira", "Pinto", "Ramos", "Ribeiro",
			"Rocha", "Rodrigues", "Sales", "Santana", "Santos", "Silva", "Souza",
			"Tavares", "Teixeira", "Vieira",
		},
	}

	for _, code := range codes {
		if _, err := pool.Exec(ctx,
			`DELETE FROM ref.name_pool WHERE nationality_code = $1`, code); err != nil {
			t.Fatalf("clean name pool %s: %v", code, err)
		}
	}
	// batch-insert all (code, type, name) rows
	sqlStr := `INSERT INTO ref.name_pool (nationality_code, name_type, name, frequency_weight)
		VALUES ($1, $2, $3, 1.0)`
	for _, code := range codes {
		for _, name := range firstNames[code] {
			if _, err := pool.Exec(ctx, sqlStr, code, "first", name); err != nil {
				t.Fatalf("insert name pool %s first: %v", code, err)
			}
		}
		for _, name := range lastNames[code] {
			if _, err := pool.Exec(ctx, sqlStr, code, "last", name); err != nil {
				t.Fatalf("insert name pool %s last: %v", code, err)
			}
		}
	}
}

// SeedClubNameParts seeds the global club-name pools (ref.club_name_parts)
// with a small deterministic set so S04-01 seeding can draw names. It upserts
// (mirroring cmd/ref-seed's contract) so re-runs and admin additions coexist.
func SeedClubNameParts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	var stems = []string{"Athletic", "Olympique", "Union", "City", "Racing", "Metropolitan"}
	var suffixes = []string{"FC", "United", "City", "SC", "Rovers", "Wanderers"}

	for _, s := range stems {
		if _, err := pool.Exec(ctx, `
			INSERT INTO ref.club_name_parts (kind, value, frequency_weight)
			VALUES ('stem', $1, 1.0)
			ON CONFLICT (kind, value) DO NOTHING`, s); err != nil {
			t.Fatalf("seed club stem %q: %v", s, err)
		}
	}
	for _, s := range suffixes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO ref.club_name_parts (kind, value, frequency_weight)
			VALUES ('suffix', $1, 1.0)
			ON CONFLICT (kind, value) DO NOTHING`, s); err != nil {
			t.Fatalf("seed club suffix %q: %v", s, err)
		}
	}
}

// CreateWorld inserts a world row and returns its id.
func CreateWorld(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO world.worlds (name, status) VALUES ($1, 'active') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("create world: %v", err)
	}
	return id
}

// Join describes one manager row for a user account.
type Join struct {
	WorldID  uuid.UUID
	Status   string // manager.managers.status; default 'unemployed'
	Employed bool   // sets status='active' + a current club
}

// CreateUser inserts an account plus one manager row per join and returns the
// account id. Employment is represented by a minimal club row.
func CreateUser(t *testing.T, pool *pgxpool.Pool, email, password string, joins []Join) uuid.UUID {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	ctx := context.Background()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO auth.users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		email, string(hash), email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i, j := range joins {
		status := j.Status
		if status == "" {
			status = "unemployed"
		}
		if j.Employed {
			status = "active"
		}

		var clubID *uuid.UUID
		if j.Employed {
			id, err := createClub(ctx, pool, j.WorldID, fmt.Sprintf("club-%s-%d", email, i))
			if err != nil {
				t.Fatalf("create club: %v", err)
			}
			clubID = &id
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO manager.managers (world_id, user_id, current_club_id, status)
			VALUES ($1, $2, $3, $4)`, j.WorldID, userID, clubID, status); err != nil {
			t.Fatalf("create manager: %v", err)
		}
	}
	return userID
}

func createClub(ctx context.Context, pool *pgxpool.Pool, worldID uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO club.clubs (world_id, name, short_name, country)
		VALUES ($1, $2, $2, 'testland') RETURNING id`, worldID, name).Scan(&id)
	return id, err
}

// CreateClub inserts a minimal club row and returns its id.
func CreateClub(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := createClub(context.Background(), pool, worldID, "fixture-club")
	if err != nil {
		t.Fatalf("create club: %v", err)
	}
	return id
}

// CreateClubWithAIManager inserts a club whose current manager is its AI
// policy-bot: the fixture shape for "AI club offering a job". Returns the club
// id and the bot manager id.
func CreateClubWithAIManager(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	id, err := createClub(ctx, pool, worldID, "ai-fixture-club")
	if err != nil {
		t.Fatalf("create club: %v", err)
	}

	var botID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO manager.managers (world_id, is_policy_bot, status, current_club_id)
		VALUES ($1, TRUE, 'active', $2) RETURNING id`, worldID, id).Scan(&botID); err != nil {
		t.Fatalf("create AI manager: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET current_manager_id = $2, is_ai_controlled = TRUE WHERE id = $1`, id, botID); err != nil {
		t.Fatalf("assign AI manager: %v", err)
	}
	return id, botID
}
