package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internalauth "github.com/touchline/backend/internal/auth"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internalfinance "github.com/touchline/backend/internal/finance"
	internalform "github.com/touchline/backend/internal/form"
	internalmanager "github.com/touchline/backend/internal/manager"
	internalmatch "github.com/touchline/backend/internal/match"
	internalsquad "github.com/touchline/backend/internal/squad"
	internaltactics "github.com/touchline/backend/internal/tactics"
	internaltraining "github.com/touchline/backend/internal/training"
	internalworld "github.com/touchline/backend/internal/world"
	pkgjwt "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

type server struct {
	svc           *internalauth.Service
	worldSvc      *internalworld.Service
	mgrSvc        *internalmanager.Service
	clubSvc       *internalclub.Service
	bootSvc       *internalbootstrap.Service
	compSvc       *internalcompetition.Service
	matchSvc      *internalmatch.Service
	tacticsSvc    *internaltactics.Service
	trainingSvc   *internaltraining.Service
	financeSvc    *internalfinance.Service
	jwtCfg        pkgjwt.JWTConfig
	pool          *pgxpool.Pool
	cookiesSecure bool
	appOrigin     string
	hub           *realtime.Hub
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

	hub := newRealtimeHub(appOrigin)

	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		log.Fatalf("init event bus: %v", err)
	}
	squadStore := internalsquad.NewStore(pool)
	s := &server{
		svc:           internalauth.NewService(pool, jwtCfg),
		worldSvc:      internalworld.NewService(pool, bus),
		mgrSvc:        internalmanager.NewService(pool, bus),
		clubSvc:       internalclub.NewService(pool),
		bootSvc:       internalbootstrap.NewService(pool, bus),
		compSvc:       internalcompetition.NewService(pool, bus),
		matchSvc:      internalmatch.NewService(pool, bus, squadStore, internalform.NewStore(pool)),
		tacticsSvc:    internaltactics.NewService(pool, bus, squadStore),
		trainingSvc:   internaltraining.NewService(pool, bus),
		financeSvc:    internalfinance.NewService(pool, bus),
		jwtCfg:        jwtCfg,
		pool:          pool,
		cookiesSecure: os.Getenv("ENV") != "development",
		appOrigin:     appOrigin,
		hub:           hub,
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

// newRealtimeHub builds the realtime fan-out hub (S02-04). When REDIS_URL is
// set and reachable the hub uses Redis pub/sub so multiple API pods share one
// event stream; otherwise it degrades to an in-process broker so bare `go run`
// works without Redis. Redis is never authoritative gameplay storage.
func newRealtimeHub(appOrigin string) *realtime.Hub {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var broker realtime.Broker = realtime.NewLocalBroker()
	if url := os.Getenv("REDIS_URL"); url != "" {
		redisBroker, err := realtime.NewRedisBroker(ctx, url)
		if err != nil {
			log.Printf("warning: REDIS_URL unreachable (%v); falling back to in-process realtime fan-out", err)
		} else {
			broker = redisBroker
			log.Printf("realtime fan-out via Redis (%s)", redisBroker.ChannelName())
		}
	}

	hub := realtime.NewHub(broker, realtime.WithOriginPatterns(originHostPattern(appOrigin)))
	go func() {
		if err := hub.Run(context.Background()); err != nil {
			log.Printf("realtime hub stopped: %v", err)
		}
	}()
	return hub
}
