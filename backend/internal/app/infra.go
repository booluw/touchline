package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/realtime"
)

// defaultMaxConns sizes the pool when DATABASE_URL does not.
const defaultMaxConns = 20

func connectDB(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// pgxpool's default (max(4, CPUs)) is too small for `serve`: the scheduler
	// leader lock, the match-runner lock and River's listener each hold one
	// connection for the life of the process, which left a 2-CPU host one
	// connection for everything else and failed /health/db. A pool_max_conns in
	// DATABASE_URL still wins.
	if !strings.Contains(databaseURL, "pool_max_conns") {
		cfg.MaxConns = defaultMaxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect DATABASE_URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("cannot reach PostgreSQL: %w", err)
	}
	return pool, nil
}

// newRealtimeBroker builds the Redis pub/sub transport for realtime fan-out,
// degrading gracefully to the in-process broker when Redis is unavailable.
func newRealtimeBroker(ctx context.Context, redisURL string) realtime.Broker {
	if redisURL == "" {
		return realtime.NewLocalBroker()
	}
	broker, err := realtime.NewRedisBroker(ctx, redisURL)
	if err != nil {
		log.Printf("warning: REDIS_URL unreachable (%v); falling back to in-process realtime fan-out", err)
		return realtime.NewLocalBroker()
	}
	log.Printf("realtime fan-out via Redis (%s)", broker.ChannelName())
	return broker
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
