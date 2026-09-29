package app

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Config mirrors the environment the backend reads. FromEnv fills it with the
// same defaults the old split binaries used.
type Config struct {
	DatabaseURL    string
	JWTSecret      string
	JWTAccessTTL   time.Duration
	JWTRefreshTTL  time.Duration
	AppOrigin      string
	Env            string
	RedisURL       string
	APIPort        string
	SchedulerPort  string
	WorkerPort     string
	SchedulerPoll  time.Duration
	RepairInterval time.Duration
}

// FromEnv reads Configuration from process environment variables and fails fast
// with an actionable error when a required variable is missing.
func FromEnv() (Config, error) {
	cfg := Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		JWTAccessTTL:   envDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:  envDuration("JWT_REFRESH_TTL", 720*time.Hour),
		AppOrigin:      envOr("APP_ORIGIN", "http://localhost:3000"),
		Env:            envOr("ENV", "development"),
		RedisURL:       os.Getenv("REDIS_URL"),
		APIPort:        envOr("API_PORT", "8080"),
		SchedulerPort:  envOr("SCHEDULER_PORT", "8081"),
		WorkerPort:     envOr("WORKER_PORT", "8082"),
		SchedulerPoll:  envDuration("SCHEDULER_POLL_INTERVAL", 15*time.Second),
		RepairInterval: envDuration("EVENT_REPAIR_SWEEP_INTERVAL", 60*time.Second),
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required — set it in .env (see docs/development.md)")
	}
	if cfg.JWTSecret == "" {
		return cfg, fmt.Errorf("JWT_SECRET is required — set it in .env (see docs/development.md)")
	}
	return cfg, nil
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
