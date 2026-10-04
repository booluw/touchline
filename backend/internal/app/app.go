// Package app is the single-process composition of the Touchline backend. It
// wires the database pool, the river event bus, the realtime broker/hub, and
// every game service once, then runs any combination of the three subsystems —
// API (HTTP + WS), scheduler (world clock), and worker (event consumer + live
// match engine) — from the same binary. `cmd/touchline serve` runs all three in
// one process; the subcommands run them individually for prod isolation.
//
// File layout:
//   - config.go     environment → Config
//   - app.go        App, Build (service wiring), Close
//   - infra.go      database, realtime broker, advisory-lock plumbing
//   - runners.go    RunAPI / RunScheduler / RunAll and the health probe
//   - worker.go     RunWorker, event subscriptions, background loops
//   - worldtick.go  the daily WORLD_TICK dispatch and its cadence passes
package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	internalacademy "github.com/touchline/backend/internal/academy"
	internaladmin "github.com/touchline/backend/internal/admin"
	internalauth "github.com/touchline/backend/internal/auth"
	internalboard "github.com/touchline/backend/internal/board"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internaldashboard "github.com/touchline/backend/internal/dashboard"
	internalfaction "github.com/touchline/backend/internal/faction"
	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/httpapi"
	internallifecycle "github.com/touchline/backend/internal/lifecycle"
	internalmanager "github.com/touchline/backend/internal/manager"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/matchday"
	internalplayer "github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/policybot"
	internalscout "github.com/touchline/backend/internal/scout"
	internalsocial "github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/squad"
	internaltactics "github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/training"
	internaltransfer "github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
	"github.com/touchline/backend/pkg/realtime"
)

// App is one process bound to one database and one event bus. Every subsystem
// runner shares the same services so the game is coherent whether it runs as a
// single process (serve) or as split pods.
type App struct {
	Pool *pgxpool.Pool
	Bus  *eventbus.RiverBus

	AppOrigin  string
	APIPort    string
	Poll       time.Duration
	RepairTick time.Duration

	// Realtime transport shared by the API hub and the worker's world-tick push
	// so fan-out is coherent in a single process.
	Broker realtime.Broker

	// Services the worker and scheduler drive directly (built once).
	CompSvc   *internalcompetition.Service
	Training  *training.Service
	Finance   *finance.Service
	Transfers *internaltransfer.Service
	Manager   *internalmanager.Service
	Players   *internalplayer.Service
	Board     *internalboard.Service
	Social    *internalsocial.Service
	Policy    *policybot.Service
	Dashboard *internaldashboard.Service
	Academy   *internalacademy.Service
	Lifecycle *internallifecycle.Service
	World     *internalworld.Service
	Runner    *matchday.Runner

	// runnerEnabled mirrors the acquired match-runner advisory lock in
	// RunWorker: the live-match subsystem runs only in the leader pod, while
	// the (non-live) daily/weekly/monthly passes run in every worker. Kept on
	// the struct so the extracted per-tick dispatch handler is testable without
	// running the full bus loop.
	runnerEnabled bool

	http *httpapi.Server
}

// Build constructs the process: pool, event bus, realtime transport, and every
// game service once, wiring the HTTP surface from them. Close must be called on
// shutdown.
func Build(ctx context.Context, cfg Config) (*App, error) {
	var seedWorker *internalcompetition.SeedWorldWorker

	pool, err := connectDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	log.Printf("connected to PostgreSQL")

	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{
		RegisterJobWorkers: func(workers *river.Workers) error {
			seedWorker = internalcompetition.NewSeedWorldWorker()
			river.AddWorker(workers, seedWorker)
			return nil
		},
		ExtraQueues: map[string]river.QueueConfig{
			internalcompetition.SeedQueue: {MaxWorkers: 1},
		},
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init event bus: %w", err)
	}

	jwt := pkgjwt.JWTConfig{
		Secret:     cfg.JWTSecret,
		AccessTTL:  cfg.JWTAccessTTL,
		RefreshTTL: cfg.JWTRefreshTTL,
	}

	broker := newRealtimeBroker(ctx, cfg.RedisURL)
	hub := realtime.NewHub(broker, realtime.WithOriginPatterns(httpapi.OriginHostPattern(cfg.AppOrigin)))
	go func() {
		if err := hub.Run(ctx); err != nil {
			log.Printf("realtime hub stopped: %v", err)
		}
	}()

	squadStore := squad.NewStore(pool)
	formStore := form.NewStore(pool)

	tacticsSvc := internaltactics.NewService(pool, bus, squadStore)

	matches := match.NewService(pool, bus, squadStore, formStore)
	compSvc := internalcompetition.NewService(pool, bus)
	seedWorker.SetRun(func(ctx context.Context, worldID uuid.UUID) error {
		_, err := compSvc.SeedWorld(ctx, worldID)
		return err
	})
	trainingSvc := training.NewService(pool, bus)
	financeSvc := finance.NewService(pool, bus)
	transfersSvc := internaltransfer.NewService(pool, bus)
	managerSvc := internalmanager.NewService(pool, bus)
	playerSvc := internalplayer.NewService(pool, bus, transfersSvc)
	transfersSvc.WithPlayerLifecycle(playerSvc)
	factionSvc := internalfaction.NewService(pool, bus)
	transfersSvc.WithSquadDynamics(factionSvc)
	matches.WithPlayers(playerSvc)
	socialSvc := internalsocial.NewService(pool, bus)
	socialSvc.WithRealtime(broker)
	matches.WithSocial(socialSvc)
	boardSvc := internalboard.NewService(pool, bus, managerSvc)
	matches.WithBoard(boardSvc)
	policySvc := policybot.NewService(pool, bus, squadStore, tacticsSvc, trainingSvc, transfersSvc)
	matches.WithPolicyBot(policySvc)
	dashSvc := internaldashboard.NewService(pool, bus)
	dashSvc.WithRealtime(broker)
	scoutSvc := internalscout.NewService(pool, compSvc)
	academySvc := internalacademy.NewService(pool, bus)
	adminSvc := internaladmin.NewService(pool, bus)
	lifecycleSvc := internallifecycle.NewService(pool, bus, academySvc)
	worldSvc := internalworld.NewService(pool, bus)
	runner := matchday.NewRunner(pool, matches, compSvc)
	runner.WithRealtime(broker)

	httpSrv := httpapi.New(httpapi.Options{
		Auth:        internalauth.NewService(pool, jwt),
		World:       worldSvc,
		Manager:     managerSvc,
		Club:        internalclub.NewService(pool),
		Bootstrap:   internalbootstrap.NewService(pool, bus),
		Competition: compSvc,
		Match:       matches,
		Tactics:     tacticsSvc,
		Training:    trainingSvc,
		Finance:     financeSvc,
		Transfers:   transfersSvc,
		Board:       boardSvc,
		Player:      playerSvc,
		Social:      socialSvc,
		Policy:      policySvc,
		Dashboard:   dashSvc,
		Academy:     academySvc,
		Faction:     factionSvc,
		Admin:       adminSvc,
		Scout:       scoutSvc,
		SeedJobs: func(ctx context.Context, worldID uuid.UUID) (int64, error) {
			return bus.InsertJob(ctx, &internalcompetition.SeedWorldJobArgs{WorldID: worldID}, nil)
		},
		JWT:           jwt,
		Pool:          pool,
		CookiesSecure: cfg.Env != "development",
		AppOrigin:     cfg.AppOrigin,
		Hub:           hub,
		Bus:           bus,
	})

	return &App{
		Pool:       pool,
		Bus:        bus,
		AppOrigin:  cfg.AppOrigin,
		APIPort:    cfg.APIPort,
		Poll:       cfg.SchedulerPoll,
		RepairTick: cfg.RepairInterval,
		Broker:     broker,
		CompSvc:    compSvc,
		Training:   trainingSvc,
		Finance:    financeSvc,
		Transfers:  transfersSvc,
		Manager:    managerSvc,
		Players:    playerSvc,
		Board:      boardSvc,
		Social:     socialSvc,
		Policy:     policySvc,
		Dashboard:  dashSvc,
		Academy:    academySvc,
		Lifecycle:  lifecycleSvc,
		World:      worldSvc,
		Runner:     runner,
		http:       httpSrv,
	}, nil
}

// HTTPHandler returns the fully wired API engine.
func (a *App) HTTPHandler() http.Handler {
	return a.http.Handler()
}

// HTTPServer returns the wired API server (integration tests reach its hub).
func (a *App) HTTPServer() *httpapi.Server {
	return a.http
}

// Close releases the pool and the realtime broker.
func (a *App) Close() {
	if a.Broker != nil {
		_ = a.Broker.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}
