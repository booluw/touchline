package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/realtime"
)

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
