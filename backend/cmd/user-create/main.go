// Command user-create provisions a login account and, optionally, its first
// manager row in a world (one invocation = one world that account inhabits).
//
// This is a development/bootstrap tool, NOT the product registration flow
// (registration policy is an open decision, OPD-02). On an empty database the
// first account is the world's first ADMIN (no -world-id): it must be able to
// log into the console before any world exists. Pass -world-id to also join
// that world as an unemployed manager.
//
// Usage:
//
//	user-create -email m@example.com -password 'secret' -display-name 'M' [-admin]
//	user-create -email m@example.com -password 'secret' -world-id <uuid> [-admin]
//	user-create ... -password-env LOGIN_PW   # read the password from an env var
//
// DATABASE_URL (or -database) must point at a reachable Postgres.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	var (
		email       = flag.String("email", "", "account email (unique)")
		password    = flag.String("password", "", "plaintext password (prefer -password-env)")
		passwordEnv = flag.String("password-env", "", "name of an env var holding the password")
		displayName = flag.String("display-name", "", "display name (defaults to the email local part)")
		worldID     = flag.String("world-id", "", "existing world UUID this account joins")
		admin       = flag.Bool("admin", false, "mark the account is_admin")
	)
	databaseURL := flag.String("database", "", "postgres URL (defaults to $DATABASE_URL)")
	flag.Parse()

	if *email == "" {
		log.Fatal("-email is required")
	}
	database := *databaseURL
	if database == "" {
		database = os.Getenv("DATABASE_URL")
	}
	if database == "" {
		log.Fatal("DATABASE_URL is required — set it in .env (see docs/development.md)")
	}

	pass := *password
	if *passwordEnv != "" {
		pass = os.Getenv(*passwordEnv)
	}
	if pass == "" {
		log.Fatal("a password is required via -password or -password-env")
	}

	name := *displayName
	if name == "" {
		name = strings.SplitN(lowerEmail(*email), "@", 2)[0]
	}

	var world *uuid.UUID
	if *worldID != "" {
		parsed, err := uuid.Parse(*worldID)
		if err != nil {
			log.Fatalf("-world-id is not a valid UUID: %v", err)
		}
		world = &parsed
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, database)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("connect: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if world != nil {
		var worldExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM world.worlds WHERE id = $1)`, *world).Scan(&worldExists); err != nil {
			log.Fatalf("check world: %v", err)
		}
		if !worldExists {
			log.Fatalf("world %s does not exist (an admin creates the world first — see docs/development.md, Worlds and job offers)", *world)
		}
	}

	var userID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO auth.users (email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		lowerEmail(*email), string(hash), name, *admin,
	).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			log.Fatalf("an account for %s already exists", *email)
		}
		log.Fatalf("insert user: %v", err)
	}

	if world != nil {
		err = tx.QueryRow(ctx, `
			INSERT INTO manager.managers (world_id, user_id, status)
			VALUES ($1, $2, 'unemployed')
			RETURNING id`, *world, userID,
		).Scan(new(uuid.UUID))
		if err != nil {
			if isUniqueViolation(err) {
				log.Fatalf("this account is already joined to world %s (one manager row per world per account)", *world)
			}
			log.Fatalf("insert manager: %v", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}

	if world != nil {
		fmt.Printf("created account %s (id %s) as unemployed manager in world %s\n", lowerEmail(*email), userID, *world)
	} else {
		fmt.Printf("created console account %s (id %s) with no world — create a world and join it with -world-id later\n", lowerEmail(*email), userID)
	}
	fmt.Println("note: registration is OPD-02 (open) — this CLI is the dev bootstrap, not the product signup flow")
}

func lowerEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
