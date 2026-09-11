package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internalauth "github.com/touchline/backend/internal/auth"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

type server struct {
	svc           *internalauth.Service
	jwtCfg        pkgjwt.JWTConfig
	pool          *pgxpool.Pool
	cookiesSecure bool
	appOrigin     string
}

func main() {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	pool, err := connectDB()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	log.Printf("connected to PostgreSQL")

	jwtCfg := pkgjwt.JWTConfig{
		Secret:     os.Getenv("JWT_SECRET"),
		AccessTTL:  envDuration("JWT_ACCESS_TTL", 15*time.Minute),
		RefreshTTL: envDuration("JWT_REFRESH_TTL", 720*time.Hour),
	}
	if jwtCfg.Secret == "" {
		log.Fatalf("JWT_SECRET is required — set it in .env (see docs/development.md)")
	}

	appOrigin := envOr("APP_ORIGIN", "http://localhost:3000")

	s := &server{
		svc:           internalauth.NewService(pool, jwtCfg),
		jwtCfg:        jwtCfg,
		pool:          pool,
		cookiesSecure: os.Getenv("ENV") != "development",
		appOrigin:     appOrigin,
	}

	r := s.router()

	log.Printf("API server starting on :%s (cors origin %s)", port, appOrigin)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

// connectDB requires DATABASE_URL and fails fast with an actionable error if
// the variable is missing or the database is unreachable.
func connectDB() (*pgxpool.Pool, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required — set it in .env (see docs/development.md)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("cannot reach PostgreSQL: %w", err)
	}
	return pool, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("warning: invalid %s %q, using %s", key, v, fallback)
	}
	return fallback
}
