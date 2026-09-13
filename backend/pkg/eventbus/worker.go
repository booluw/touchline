package eventbus

import (
	"context"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// EventJobArgs carries only the event ID. The worker re-reads the full row from
// world.events, so a handler sees exactly what was persisted.
type EventJobArgs struct {
	EventID uuid.UUID `json:"event_id"`
}

// Kind identifies the job type in the river_job table.
func (*EventJobArgs) Kind() string { return "touchline_event" }

// InsertOpts makes enqueues unique by args: a non-nil EventID fully encodes the
// event identity, so double-publishing the same event cannot double-enqueue.
// Delivery itself remains at-least-once (see EventHandler contract).
func (*EventJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

// EventWorker is the river worker that turns a queued event job back into an
// Event and dispatches it. Errors are returned to river, which retries the job
// with backoff (at-least-once, no silent discards).
type EventWorker struct {
	// WorkerDefaults pins Middleware/NextRetry/Timeout to river defaults so the
	// client-level retry policy applies.
	river.WorkerDefaults[*EventJobArgs]
	loader     func(ctx context.Context, eventID uuid.UUID) (*Event, error)
	dispatcher *EventDispatcher
}

// NewEventWorker builds an EventWorker over a loader (typically RiverBus) and a
// shared dispatcher.
func NewEventWorker(loader func(context.Context, uuid.UUID) (*Event, error), dispatcher *EventDispatcher) *EventWorker {
	return &EventWorker{loader: loader, dispatcher: dispatcher}
}

// Work loads the event row and hands it to the dispatcher.
func (w *EventWorker) Work(ctx context.Context, job *river.Job[*EventJobArgs]) error {
	event, err := w.loader(ctx, job.Args.EventID)
	if err != nil {
		return err
	}
	return w.dispatcher.Dispatch(ctx, event)
}
