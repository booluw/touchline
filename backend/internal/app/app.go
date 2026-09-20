// Package app is the single-process composition of the Touchline backend. It
// wires the database pool, the river event bus, the realtime broker/hub, and
// every game service once, then runs any combination of the three subsystems —
// API (HTTP + WS), scheduler (world clock), and worker (event consumer + live
// match engine) — from the same binary. `cmd/touchline serve` runs all three in
// one process; the subcommands run them individually for prod isolation.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/google/uuid"
	internalacademy "github.com/touchline/backend/internal/academy"
	internaladmin "github.com/touchline/backend/internal/admin"
	internalauth "github.com/touchline/backend/internal/auth"
	internalboard "github.com/touchline/backend/internal/board"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internaldashboard "github.com/touchline/backend/internal/dashboard"
	"github.com/touchline/backend/internal/eventoutbox"
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
	"github.com/touchline/backend/internal/scheduler"
	internalsocial "github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/squad"
	internaltactics "github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/training"
	internaltransfer "github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
	pkgjwt "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
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

// App is one process bound to one database and one event bus. Every subsystem
// runner shares the same services so the game is coherent whether it runs as a
// single process (serve) or as split pods.
type App struct {
	Pool *pgxpool.Pool
	Bus  *eventbus.RiverBus

	JWT        pkgjwt.JWTConfig
	AppOrigin  string
	APIPort    string
	Poll       time.Duration
	RepairTick time.Duration

	// Realtime transport shared by the API hub and the worker's world-tick push
	// so fan-out is coherent in a single process.
	Broker realtime.Broker
	Hub    *realtime.Hub

	// Services (built once).
	Matches   *match.Service
	CompSvc   *internalcompetition.Service
	Training  *training.Service
	Finance   *finance.Service
	Transfers *internaltransfer.Service
	Players   *internalplayer.Service
	Board     *internalboard.Service
	Social    *internalsocial.Service
	Policy    *policybot.Service
	Dashboard *internaldashboard.Service
	Academy   *internalacademy.Service
	Admin     *internaladmin.Service
	Lifecycle *internallifecycle.Service
	Runner    *matchday.Runner
	http      *httpapi.Server
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

	broker := newRealtimeBroker(ctx)
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
	policySvc := policybot.NewService(pool, bus, squadStore, tacticsSvc, trainingSvc, transfersSvc)
	matches.WithPolicyBot(policySvc)
	dashSvc := internaldashboard.NewService(pool, bus)
	dashSvc.WithRealtime(broker)
	academySvc := internalacademy.NewService(pool, bus)
	adminSvc := internaladmin.NewService(pool, bus)
	lifecycleSvc := internallifecycle.NewService(pool, bus, academySvc)
	runner := matchday.NewRunner(pool, matches, compSvc)
	runner.WithRealtime(broker)

	httpSrv := httpapi.New(httpapi.Options{
		Auth:        internalauth.NewService(pool, jwt),
		World:       internalworld.NewService(pool, bus),
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
		JWT:        jwt,
		AppOrigin:  cfg.AppOrigin,
		APIPort:    cfg.APIPort,
		Poll:       cfg.SchedulerPoll,
		RepairTick: cfg.RepairInterval,
		Broker:     broker,
		Hub:        hub,
		Matches:    matches,
		CompSvc:    compSvc,
		Training:   trainingSvc,
		Finance:    financeSvc,
		Transfers:  transfersSvc,
		Players:    playerSvc,
		Board:      boardSvc,
		Social:     socialSvc,
		Policy:     policySvc,
		Dashboard:  dashSvc,
		Academy:    academySvc,
		Admin:      adminSvc,
		Lifecycle:  lifecycleSvc,
		Runner:     runner,
		http:       httpSrv,
	}, nil
}

// HTTPHandler returns the fully wired API engine.
func (a *App) HTTPHandler() http.Handler {
	return a.http.Handler()
}

// Close releases the pool, the realtime broker, and the event bus.
func (a *App) Close() {
	if a.Broker != nil {
		_ = a.Broker.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}

// RunAPI serves the HTTP + WebSocket surface until ctx is cancelled, draining
// in-flight requests on shutdown.
func (a *App) RunAPI(ctx context.Context) error {
	srv := &http.Server{
		Addr:              ":" + a.APIPort,
		Handler:           a.HTTPHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("API server starting on :%s (cors origin %s)", a.APIPort, a.AppOrigin)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return nil
}

// RunScheduler drives the configurable world clock until ctx is cancelled. A
// scheduler advisory lock elects a single leader.
func (a *App) RunScheduler(ctx context.Context) error {
	log.Printf("world clock starting (cadence sync every %s)", a.Poll)
	if err := scheduler.NewService(a.Pool, a.Bus).Run(ctx, a.Poll); err != nil {
		return fmt.Errorf("world clock: %w", err)
	}
	log.Printf("world clock stopped")
	return nil
}

// RunWorker consumes the event bus: realtime world-tick fan-out, daily
// kickoffs + live match pacing, weekly training, monthly wages, the outbox
// repair sweep, and the live-match startup rehydration.
func (a *App) RunWorker(ctx context.Context) error {
	bus := a.Bus

	runnerEnabled, releaseRunnerLock, err := acquireMatchRunnerLock(ctx, a.Pool)
	if err != nil {
		return err
	}
	if !runnerEnabled {
		log.Printf("another worker holds the match-runner lock; live subsystem disabled in this pod")
	} else {
		defer releaseRunnerLock()
	}

	if err := bus.Subscribe(ctx, "WORLD_TICK", func(ev eventbus.Event) error {
		var payload struct {
			Granularity string `json:"granularity"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			log.Printf("world tick %s: unreadable payload (%v); skipping", ev.ID, err)
			return nil
		}
		log.Printf("handled event %s (%s, granularity %s) for world %s at tick %d",
			ev.ID, ev.EventType, payload.Granularity, ev.WorldID, ev.WorldTick)

		tickEvent, err := realtime.BuildWorldTick(ev.WorldID, ev.ID.String(), payload.Granularity, ev.WorldTick)
		if err != nil {
			return nil
		}
		if err := a.Broker.Publish(ctx, tickEvent); err != nil {
			return nil
		}

		if payload.Granularity == "daily" && runnerEnabled {
			sum, err := a.Runner.KickoffDue(ctx, ev.WorldID)
			if err != nil {
				log.Printf("world %s daily tick: kickoff: %v", ev.WorldID, err)
				return err
			}
			if sum != nil && sum.Kicked > 0 {
				log.Printf("world %s daily tick: kicked %d matchday(s), %d fixture(s)",
					ev.WorldID, sum.Matchdays, sum.Kicked)
			}
			go func() {
				if err := a.Runner.RunLive(ctx, ev.WorldID); err != nil {
					log.Printf("world %s live runner: %v", ev.WorldID, err)
				}
			}()
		}
		if payload.Granularity == "daily" {
			if err := a.Policy.RespondToBidsForAbsent(ctx, ev.WorldID); err != nil {
				return fmt.Errorf("world %s daily policy bids: %w", ev.WorldID, err)
			}
			if err := a.Transfers.DailyTick(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s daily transfer market: %w", ev.WorldID, err)
			}
		}
		if payload.Granularity == "weekly" {
			if err := a.Policy.EnsureTraining(ctx, ev.WorldID); err != nil {
				return fmt.Errorf("world %s weekly policy training: %w", ev.WorldID, err)
			}
			if _, err := a.Training.ApplyWeekly(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s weekly training: %w", ev.WorldID, err)
			}
			if err := a.Players.WeeklyTick(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s weekly player pass: %w", ev.WorldID, err)
			}
			if reconciled, err := a.Social.ReconcileRivalries(ctx, ev.WorldID); err != nil {
				return fmt.Errorf("world %s rivalries reconcile: %w", ev.WorldID, err)
			} else if reconciled > 0 {
				log.Printf("world %s rivalries reconciled: %d fixtures backfilled", ev.WorldID, reconciled)
			}
			if reviewed, sacked, err := a.Board.WeeklyReview(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s weekly board review: %w", ev.WorldID, err)
			} else {
				log.Printf("world %s weekly board review: %d reviewed, %d sacked", ev.WorldID, reviewed, sacked)
			}
		}
		if payload.Granularity == "monthly" {
			if _, err := a.Finance.ApplyMonthlyWages(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s monthly wages: %w", ev.WorldID, err)
			}
			if _, err := a.Academy.Maintenance(ctx, ev.WorldID, ev.WorldTick); err != nil {
				return fmt.Errorf("world %s academy maintenance: %w", ev.WorldID, err)
			}
		}
		// Seasonal fallback (S08-01): a world without leagues never emits
		// SEASON_COMPLETED, so the seasonal tick drives the full lifecycle
		// once per season — intake, retirement, pool replenish (A06). The
		// hooks dedup per season.
		if payload.Granularity == "seasonal" {
			season, ref, err := a.worldSeason(ctx, ev.WorldID)
			if err != nil {
				return fmt.Errorf("world %s seasonal lifecycle: %w", ev.WorldID, err)
			}
			if _, err := a.Lifecycle.OnSeasonCompleted(ctx, ev.WorldID, nil, season, ref); err != nil {
				return fmt.Errorf("world %s seasonal lifecycle: %w", ev.WorldID, err)
			}
		}
		// Home dashboard realtime sweep (S07-01): after the cadence passes have
		// run, re-snapshot every managed club and push newly surfaced items to
		// the affected managers' socket feeds. Best-effort; the GET read stays
		// authoritative.
		if err := a.Dashboard.PushWorldDelta(ctx, ev.WorldID); err != nil {
			return fmt.Errorf("world %s dashboard sweep: %w", ev.WorldID, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	// Transfer-market events push an urgent dashboard item to the selling
	// club's manager the moment a bid lands/counters/resolves (S07-01). The
	// eventbus is single-handler-per-type and nobody else consumes these types.
	for _, bidType := range []string{
		internaltransfer.EventBidPlaced,
		internaltransfer.EventBidCountered,
		internaltransfer.EventBidAccepted,
		internaltransfer.EventBidRejected,
	} {
		if err := bus.Subscribe(ctx, bidType, func(ev eventbus.Event) error {
			var payload struct {
				BidID         uuid.UUID `json:"bid_id"`
				SellingClubID uuid.UUID `json:"selling_club_id"`
			}
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				log.Printf("bid event %s: unreadable payload (%v); skipping", ev.ID, err)
				return nil
			}
			if payload.SellingClubID == uuid.Nil {
				return nil
			}
			managerID, err := a.Dashboard.ManagerForClub(ctx, ev.WorldID, payload.SellingClubID)
			if err != nil {
				return nil
			}
			if err := a.Dashboard.PushCategory(ctx, ev.WorldID, managerID, internaldashboard.PriorityUrgent); err != nil {
				log.Printf("world %s dashboard bid push: %v", ev.WorldID, err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", bidType, err)
		}
	}

	// Season rollover drives the player lifecycle (S08-01, A06): the completed
	// league's country gets its street discovery plus every club academy's youth
	// cohort, then the world's retirement pass and pool replenishment run. The
	// eventbus is single-handler-per-type and nobody else consumes SEASON_COMPLETED.
	if err := bus.Subscribe(ctx, "SEASON_COMPLETED", func(ev eventbus.Event) error {
		var payload struct {
			CountryID *uuid.UUID `json:"country_id"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			log.Printf("season completed %s: unreadable payload (%v); skipping", ev.ID, err)
			return nil
		}
		season, ref, err := a.worldSeason(ctx, ev.WorldID)
		if err != nil {
			return fmt.Errorf("world %s season completed lifecycle: %w", ev.WorldID, err)
		}
		if _, err := a.Lifecycle.OnSeasonCompleted(ctx, ev.WorldID, payload.CountryID, season, ref); err != nil {
			return fmt.Errorf("world %s season completed lifecycle: %w", ev.WorldID, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("subscribe season completed: %w", err)
	}

	if err := bus.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	log.Printf("worker started; consuming events from the event bus")

	// Outbox repair sweep (OPD-23). Idempotent re-enqueue by original id.
	go a.sweep(ctx)

	// Startup sweep (OPD-21): resume any in-progress match after a restart.
	if runnerEnabled {
		go a.rehydrate(ctx)
	}

	<-ctx.Done()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := bus.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("stop: %v", err)
	}
	log.Printf("worker stopped")
	return nil
}

// RunAll runs the API, scheduler, and worker in one process until ctx is
// cancelled or one of them fails. All three share the same pool, bus, and
// realtime transport built by Build.
func (a *App) RunAll(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Printf("---- touchline serve: api :%s + scheduler + worker in one process ----", a.APIPort)

	errCh := make(chan error, 3)
	go func() { errCh <- a.RunAPI(ctx) }()
	go func() { errCh <- a.RunScheduler(ctx) }()
	go func() { errCh <- a.RunWorker(ctx) }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("touchline serve: subsystem failed (%v); shutting down", err)
		}
		cancel()
		// Give the other subsystems a moment to drain after cancellation.
		timeout := time.NewTimer(20 * time.Second)
		defer timeout.Stop()
		for range 2 {
			select {
			case <-errCh:
			case <-timeout.C:
			}
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// worldSeason derives the canonical season number and world reference date
// from the day counter (worldDate = COALESCE(launched_at, created_at) +
// current_day days; OPD-24). Used by the seasonal academy-intake hooks.
func (a *App) worldSeason(ctx context.Context, worldID uuid.UUID) (int, time.Time, error) {
	var day int64
	var ref time.Time
	err := a.Pool.QueryRow(ctx, `
		SELECT w.current_day,
		       COALESCE(w.launched_at, w.created_at) + make_interval(days => w.current_day::int)
		FROM world.worlds w WHERE w.id = $1`, worldID).Scan(&day, &ref)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("world season: %w", err)
	}
	return internalacademy.SeasonForDay(day), ref, nil
}

// sweep re-enqueues committed world.events rows that never got a dispatch job.
func (a *App) sweep(ctx context.Context) {
	ticker := time.NewTicker(a.RepairTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rep, err := eventoutbox.Sweep(ctx, a.Pool, a.Bus, eventoutbox.Options{})
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("event_repair_sweep: error: %v", err)
				continue
			}
			if rep.Repaired > 0 || rep.OldestLagSeconds > 0 {
				log.Printf("event_repair_sweep scanned=%d repaired=%d oldest_lag_s=%.0f",
					rep.Scanned, rep.Repaired, rep.OldestLagSeconds)
			}
		}
	}
}

// rehydrate resumes every world's in-progress matches after a pod restart.
func (a *App) rehydrate(ctx context.Context) {
	worlds, err := a.Runner.WorldsWithLiveMatches(ctx)
	if err != nil {
		log.Printf("live startup sweep: %v", err)
		return
	}
	for _, w := range worlds {
		go func(worldID uuid.UUID) {
			if err := a.Runner.RunLive(ctx, worldID); err != nil {
				log.Printf("world %s live rehydrate: %v", worldID, err)
			}
		}(w)
	}
}

// acquireMatchRunnerLock elects a single worker pod to run the live match
// subsystem (OPD-21) via a Postgres advisory lock. The lock is bound to one
// dedicated connection held for the worker's lifetime.
func acquireMatchRunnerLock(ctx context.Context, pool *pgxpool.Pool) (bool, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("match runner: acquire lock connection: %w", err)
	}
	for {
		var got bool
		if err := conn.QueryRow(ctx,
			`SELECT pg_try_advisory_lock(hashtext('touchline:match_runner'))`).Scan(&got); err != nil {
			conn.Release()
			return false, nil, fmt.Errorf("match runner: acquire lock: %w", err)
		}
		if got {
			return true, func() { conn.Release() }, nil
		}
		log.Printf("match runner: waiting for another worker to release the runner lock")
		select {
		case <-ctx.Done():
			conn.Release()
			return false, nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// newRealtimeBroker builds the Redis pub/sub transport for realtime fan-out,
// degrading gracefully to the in-process broker when Redis is unavailable.
func newRealtimeBroker(ctx context.Context) realtime.Broker {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return realtime.NewLocalBroker()
	}
	broker, err := realtime.NewRedisBroker(ctx, url)
	if err != nil {
		log.Printf("warning: REDIS_URL unreachable (%v); falling back to in-process realtime fan-out", err)
		return realtime.NewLocalBroker()
	}
	log.Printf("realtime fan-out via Redis (%s)", broker.ChannelName())
	return broker
}

func connectDB(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
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
