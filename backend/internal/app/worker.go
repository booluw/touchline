package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/dashboard"
	"github.com/touchline/backend/internal/eventoutbox"
	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/realtime"
)

// RunWorker consumes the event bus: realtime world-tick fan-out, the daily
// kickoffs + live match pacing, the day-derived weekly/monthly passes, the
// outbox repair sweep, and the live-match startup rehydration.
func (a *App) RunWorker(ctx context.Context) error {
	runnerEnabled, releaseRunnerLock, err := acquireMatchRunnerLock(ctx, a.Pool)
	if err != nil {
		return err
	}
	a.runnerEnabled = runnerEnabled
	if !runnerEnabled {
		log.Printf("another worker holds the match-runner lock; live subsystem disabled in this pod")
	} else {
		defer releaseRunnerLock()
	}

	for _, subscribe := range []func(context.Context) error{
		a.subscribeWorldTick,
		a.subscribeBidEvents,
		a.subscribeSeasonCompleted,
	} {
		if err := subscribe(ctx); err != nil {
			return err
		}
	}

	if err := a.Bus.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	log.Printf("worker started; consuming events from the event bus")

	// Outbox repair sweep (OPD-23). Idempotent re-enqueue by original id.
	go a.sweep(ctx)

	// Startup sweep (OPD-21): resume any in-progress match after a restart.
	if runnerEnabled {
		go a.rehydrate(ctx)
		// IM16: kicks happen at scheduled_at moments, not just on daily ticks,
		// so the worker polls every playable world on its own cadence.
		go a.kickoffPoll(ctx)
	}

	<-ctx.Done()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := a.Bus.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("stop: %v", err)
	}
	log.Printf("worker stopped")
	return nil
}

// subscribeWorldTick fans each WORLD_TICK out to realtime subscribers
// (best-effort), then runs the day-derived gameplay dispatch (handleWorldTick).
func (a *App) subscribeWorldTick(ctx context.Context) error {
	err := a.Bus.Subscribe(ctx, "WORLD_TICK", func(ev eventbus.Event) error {
		var payload struct {
			Granularity string `json:"granularity"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			log.Printf("world tick %s: unreadable payload (%v); skipping", ev.ID, err)
			return nil
		}
		log.Printf("handled event %s (%s, granularity %s) for world %s at tick %d",
			ev.ID, ev.EventType, payload.Granularity, ev.WorldID, ev.WorldTick)

		// The socket push is best-effort (IM23): a realtime failure is logged
		// and the day's gameplay still runs — skipping it here would silently
		// drop wages, training, the market and kickoffs for that game day.
		if tickEvent, err := realtime.BuildWorldTick(ev.WorldID, ev.ID.String(), payload.Granularity, ev.WorldTick); err != nil {
			log.Printf("world tick %s: build realtime envelope: %v", ev.ID, err)
		} else if err := a.Broker.Publish(ctx, tickEvent); err != nil {
			log.Printf("world tick %s: realtime publish: %v", ev.ID, err)
		}

		return a.handleWorldTick(ctx, ev, payload.Granularity)
	})
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return nil
}

// subscribeBidEvents pushes the urgent dashboard section to both sides of a
// bid thread the moment a bid lands, is countered, accepted, rejected or
// withdrawn (S07-01, IM26): the seller learns of an incoming offer, the buyer
// of the answer. Every bid event carries buying_club_id + selling_club_id; a
// side with no human manager (an AI club) is skipped, and PushCategory only
// sends items that are new to that manager, so the acting side gets no noise.
// The eventbus is single-handler-per-type and nobody else consumes these types.
func (a *App) subscribeBidEvents(ctx context.Context) error {
	for _, bidType := range []string{
		transfer.EventBidPlaced,
		transfer.EventBidCountered,
		transfer.EventBidAccepted,
		transfer.EventBidRejected,
		transfer.EventBidWithdrawn,
	} {
		if err := a.Bus.Subscribe(ctx, bidType, func(ev eventbus.Event) error {
			var payload struct {
				BuyingClubID  uuid.UUID `json:"buying_club_id"`
				SellingClubID uuid.UUID `json:"selling_club_id"`
			}
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				log.Printf("bid event %s: unreadable payload (%v); skipping", ev.ID, err)
				return nil
			}
			for _, clubID := range []uuid.UUID{payload.SellingClubID, payload.BuyingClubID} {
				if clubID == uuid.Nil {
					continue
				}
				managerID, err := a.Dashboard.ManagerForClub(ctx, ev.WorldID, clubID)
				if err != nil {
					continue // AI club / no human manager to notify
				}
				if err := a.Dashboard.PushCategory(ctx, ev.WorldID, managerID, dashboard.PriorityUrgent); err != nil {
					log.Printf("world %s dashboard bid push: %v", ev.WorldID, err)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", bidType, err)
		}
	}
	return nil
}

// subscribeSeasonCompleted drives the player lifecycle on season rollover
// (S08-01, A06): the completed league's country gets its street discovery plus
// every club academy's youth cohort, then the world's retirement pass and pool
// replenishment run. The eventbus is single-handler-per-type and nobody else
// consumes SEASON_COMPLETED.
func (a *App) subscribeSeasonCompleted(ctx context.Context) error {
	err := a.Bus.Subscribe(ctx, "SEASON_COMPLETED", func(ev eventbus.Event) error {
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
	})
	if err != nil {
		return fmt.Errorf("subscribe season completed: %w", err)
	}
	return nil
}

// kickoffPoll is the IM16 intra-day kickoff pass: the continuous world clock
// matures scheduled_at moments between daily tick emissions, so the worker
// scans every playable world on its own cadence and plays what is due. This is
// what makes a "20:00" fixture simulate at 20:00 instead of whenever the daily
// tick lands.
func (a *App) kickoffPoll(ctx context.Context) {
	ticker := time.NewTicker(a.Poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worlds, err := a.Runner.PlayableWorlds(ctx)
			if err != nil {
				log.Printf("kickoff poll: playable worlds: %v", err)
				continue
			}
			for _, w := range worlds {
				if err := a.kickDueWorld(ctx, w); err != nil {
					log.Printf("kickoff poll: world %s: %v", w, err)
				}
			}
		}
	}
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
