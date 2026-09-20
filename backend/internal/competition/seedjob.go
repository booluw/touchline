package competition

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// SeedQueue holds the long-running world-seed jobs so they never starve (or
// get starved by) the event dispatch queue. One worker at a time keeps a full
// multi-league seed from competing with itself.
const SeedQueue = "seed"

// SeedWorldJobArgs enqueues an async world seed. Jobs are unique by args, so
// duplicate POSTs for the same world coalesce while one is pending/running —
// a second submission waits for the first to finish, and the follow-up is a
// no-op because SeedWorld is idempotent.
type SeedWorldJobArgs struct {
	WorldID uuid.UUID `json:"world_id"`
}

func (SeedWorldJobArgs) Kind() string { return "seed_world" }

func (SeedWorldJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       SeedQueue,
		Priority:    2,
		MaxAttempts: 5,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

// SeedWorldWorker runs a queued seed. The `run` func is wired by the app once
// the competition service exists (the river client is built before compSvc), so
// an unwired worker is a programming error, not a retryable job failure.
//
// Note the args are registered as the value type SeedWorldJobArgs (not a
// pointer): river's AddWorker instantiates args via `var jobArgs T` and calls
// its value-receiver Kind(), which would panic on a nil *SeedWorldJobArgs.
type SeedWorldWorker struct {
	river.WorkerDefaults[SeedWorldJobArgs]
	run func(ctx context.Context, worldID uuid.UUID) error
}

func NewSeedWorldWorker() *SeedWorldWorker { return &SeedWorldWorker{} }

func (w *SeedWorldWorker) SetRun(run func(ctx context.Context, worldID uuid.UUID) error) {
	w.run = run
}

// Timeout gives the seed job a generous per-attempt deadline. Without this the
// worker would inherit river's 1-minute default JobTimeout, which a whole-world
// seed (one big transaction, tens of thousands of sequential round-trips)
// legitimately exceeds — the ctx would be cancelled mid-run, the tx rolled
// back, and the retry would then block on the previous attempt's world-row
// lock until its own deadline expired.
func (w *SeedWorldWorker) Timeout(*river.Job[SeedWorldJobArgs]) time.Duration {
	return 30 * time.Minute
}

func (w *SeedWorldWorker) Work(ctx context.Context, job *river.Job[SeedWorldJobArgs]) error {
	if w.run == nil {
		return errors.New("seed worker: run not wired")
	}
	log.Printf("seed job %d: running world %s (attempt %d)", job.ID, job.Args.WorldID, job.Attempt)
	if err := w.run(ctx, job.Args.WorldID); err != nil {
		log.Printf("seed job %d: failed: %v", job.ID, err)
		return err
	}
	log.Printf("seed job %d: completed world %s", job.ID, job.Args.WorldID)
	return nil
}
