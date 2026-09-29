package transfer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ---------- daily sweep ----------

// DailyTick runs the market's daily housekeeping for a world: expiring stale
// bids, refreshing every valuation, and letting AI clubs bid on unsold human
// listings. Every step is idempotent under redelivery.
func (s *Service) DailyTick(ctx context.Context, worldID uuid.UUID, worldTick int64) error {
	if _, err := s.ExpireStale(ctx, worldID, worldTick); err != nil {
		return err
	}
	if _, err := s.RecomputeValuations(ctx, worldID, worldTick); err != nil {
		return err
	}
	if _, err := s.AIBidActivity(ctx, worldID, worldTick); err != nil {
		return err
	}
	return nil
}

// ExpireStale resolves bids that have gone unanswered beyond BidTTLWorldDays.
func (s *Service) ExpireStale(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	ttl, err := bidTTL(ctx, s.pool, worldID)
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE transfer.bids SET status = 'expired', responded_at = now()
		WHERE world_id = $1 AND status IN ('pending','countered')
		  AND created_at < now() - make_interval(secs => $2)`, worldID, ttl.Seconds())
	if err != nil {
		return 0, fmt.Errorf("expire stale bids: %w", err)
	}
	n := tag.RowsAffected()
	if n == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire event tx: %w", err)
	}
	defer tx.Rollback(ctx)
	payload := mustJSON(map[string]any{"world_id": worldID, "world_tick": worldTick, "expired": n})
	if err := s.recordSystemEvent(ctx, tx, worldID, worldTick, EventBidExpired, payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expire event: %w", err)
	}
	return n, nil
}

// RecomputeValuations refreshes player.players.market_value for every active
// squad member in a world (filling the "never written" gap this slice closes).
func (s *Service) RecomputeValuations(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	players, err := s.store.ActiveAttrPlayers(ctx, worldID)
	if err != nil {
		return 0, err
	}
	if len(players) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin valuation tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var n int64
	for _, p := range players {
		v := Valuation(p)
		if v == p.MarketValue {
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player.players SET market_value = $1 WHERE id = $2`, v, p.PlayerID); err != nil {
			return 0, fmt.Errorf("set valuation: %w", err)
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	payload := mustJSON(map[string]any{"world_id": worldID, "world_tick": worldTick, "players": n})
	if err := s.recordSystemEvent(ctx, tx, worldID, worldTick, EventValuationsRefreshed, payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit valuations: %w", err)
	}
	return n, nil
}

// AIBidActivity has the highest-interest AI clubs bid on unsold human
// listings. Deterministic per (listing, worldTick); listing guards keep the
// market from stacking bids.
func (s *Service) AIBidActivity(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	targets, err := s.store.AIInterestTargets(ctx, worldID)
	if err != nil {
		return 0, err
	}
	if len(targets) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin ai activity tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var placed int64
	for _, t := range targets {
		attrs, err := s.attrsForPlayerTx(ctx, tx, t.PlayerID)
		if err != nil {
			return 0, err
		}
		val := Valuation(attrs)
		candidates, err := s.aiCandidatesTx(ctx, tx, worldID, t.SellingClubID, attrs.Position)
		if err != nil {
			return 0, err
		}
		for _, cand := range candidates {
			fee := aiBidFee(aiBidSeed(t.ID, worldTick), val, bidAsking(t.AskingPrice))
			if cand.Cash < int64(float64(fee)*aiBuyFundsMargin) {
				continue
			}
			ok, err := s.placeAIBid(ctx, tx, worldID, t, cand, fee, attrs)
			if err != nil {
				return 0, err
			}
			if ok {
				placed++
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit ai activity: %w", err)
	}
	return placed, nil
}
