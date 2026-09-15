package player

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/pkg/explanation"
)

// WeeklyTick is the player-processing round baked into the world's weekly
// pass (wired beside the board review in app). Order matters: resolve roles
// first so the roster pass grades against its entitlement, then recover morale,
// then check promises, then (for human-managed clubs) let deep unhappiness
// escalate into a transfer request.
func (s *Service) WeeklyTick(ctx context.Context, worldID uuid.UUID, worldTick int64) error {
	clubs, err := worldClubs(ctx, s.pool, worldID)
	if err != nil {
		return err
	}
	for _, clubID := range clubs {
		if err := s.clubWeeklyPass(ctx, worldID, worldTick, clubID); err != nil {
			return err
		}
	}
	if s.transfers != nil {
		return s.autoListStale(ctx, worldID, worldTick)
	}
	return nil
}

func (s *Service) clubWeeklyPass(ctx context.Context, worldID uuid.UUID, worldTick int64, clubID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Lazy squad-role resolution for contracts signed without one.
	missing, err := unresolvedRoles(ctx, tx, clubID)
	if err != nil {
		return err
	}
	for _, pid := range missing {
		exp, err := expectationRole(ctx, tx, pid)
		if err != nil {
			return err
		}
		if role := SquadRoleFromExpectation(exp); role != "" {
			if err := setSquadRole(ctx, tx, pid, role); err != nil {
				return err
			}
		}
	}

	// 2. Morale recovery toward neutral (personality-weighted).
	roster, err := weeklyRoster(ctx, tx, clubID)
	if err != nil {
		return err
	}
	recovered := make(map[uuid.UUID]gameRow, len(roster))
	for _, r := range roster {
		next := WeeklyRecovery(r.Morale, r.Pro)
		recovered[r.PlayerID] = gameRow{PlayerID: r.PlayerID, Role: r.Role, Morale: next, Share: r.Share, Pro: r.Pro}
		if err := upsertMorale(ctx, tx, r.PlayerID, next); err != nil {
			return err
		}
	}

	// 3. Playing-time promises: met ones are kept, overdue ones are broken.
	if err := s.evaluatePromises(ctx, tx, worldID, clubID); err != nil {
		return err
	}

	// 4. Deep, persistent unhappiness at a human-managed club becomes an
	// official request (deterministic: morale ≤ threshold AND a real shortfall
	// AND no open request AND cooled down).
	mgr, ai, err := clubManager(ctx, tx, clubID)
	if err != nil {
		return err
	}
	if !ai && mgr != uuid.Nil {
		for _, r := range recovered {
			expected := ExpectedShareForRole(r.Role)
			if r.Morale > UnhappyMoraleThreshold || !DeepShortfall(r.Share, expected) {
				continue
			}
			ok, err := cooldownPast(ctx, tx, r.PlayerID)
			if err != nil {
				return err
			}
			open, err := openTransferRequest(ctx, tx, r.PlayerID)
			if err != nil {
				return err
			}
			if !ok || open != nil {
				continue
			}
			if err := s.createRequest(ctx, tx, worldID, worldTick, r.PlayerID, clubID, mgr, r.Morale, r.Share, expected); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

// createRequest files a playing-time transfer request and emits its event in
// the club's transaction.
func (s *Service) createRequest(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64,
	playerID, clubID, managerID uuid.UUID, morale, share, expected float64,
) error {
	req := &TransferRequest{
		PlayerID:  playerID,
		ClubID:    clubID,
		ManagerID: managerID,
		Status:    TransferRequestPending,
		Reason:    TransferReasonPlayingTime,
	}
	if err := insertRequest(ctx, tx, req); err != nil {
		return err
	}
	exp := requestExplanation(playerID, morale, share, expected)
	rawExp, err := json.Marshal(exp)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"request_id": req.ID, "player_id": playerID, "club_id": clubID,
		"reason": req.Reason, "status": req.Status,
	})
	if err != nil {
		return err
	}
	_, err = s.recordEvent(ctx, tx, worldID, worldTick, "manager", managerID,
		EventPlayerTransferRequested, payload, rawExp)
	return err
}

// evaluatePromises grades the club's open increase_playing_time promises: a
// share that meets the role entitlement is fulfilled (memory +10); one that is
// overdue is broken (memory −30) and the player is enabled to ask again.
func (s *Service) evaluatePromises(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, clubID uuid.UUID) error {
	pro, err := pendingPlayingTimePromises(ctx, tx, clubID)
	if err != nil {
		return err
	}
	for _, p := range pro {
		expected := ExpectedShareForRole(p.Role)
		switch {
		case Satisfied(p.Share, expected):
			if err := resolvePromises(ctx, tx, p.PlayerID, "fulfilled"); err != nil {
				return err
			}
			if err := recordRelationshipEvent(ctx, tx, worldID, p.PlayerID, p.ManagerID,
				RelationshipPromiseKept, SentimentPromiseKept, nil); err != nil {
				return err
			}
		case p.Created.Before(time.Now().UTC().AddDate(0, 0, -PromiseEvaluationWeeks*7)):
			if err := resolvePromises(ctx, tx, p.PlayerID, "broken"); err != nil {
				return err
			}
			if err := recordRelationshipEvent(ctx, tx, worldID, p.PlayerID, p.ManagerID,
				RelationshipPromiseBroken, SentimentPromiseBroken, nil); err != nil {
				return err
			}
			if err := setCooldown(ctx, tx, p.PlayerID, 0, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// autoListStale advances every request still pending beyond the TTL to
// auto_listed and puts the player on the market (one listing per stale
// request, in the transfer service's own transaction).
func (s *Service) autoListStale(ctx context.Context, worldID uuid.UUID, worldTick int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	stale, err := stalePendingRequests(ctx, tx)
	if err != nil {
		return err
	}
	resolved := make([]TransferRequest, 0, len(stale))
	for _, r := range stale {
		if err := resolveRequest(ctx, tx, r.ID, TransferRequestAutoListed); err != nil {
			return err
		}
		exp := explanation.New("transfer_request_auto_listed", 0).Add("request unaddressed", 0)
		rawExp, err := json.Marshal(exp)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"request_id": r.ID, "player_id": r.PlayerID, "club_id": r.ClubID,
		})
		if err != nil {
			return err
		}
		if _, err := s.recordEvent(ctx, tx, worldID, worldTick, "system", uuid.Nil,
			EventTransferRequestAutoListed, payload, rawExp); err != nil {
			return err
		}
		resolved = append(resolved, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	for _, r := range resolved {
		mgr, _, err := clubManager(ctx, s.pool, r.ClubID)
		if err != nil || mgr == uuid.Nil {
			continue
		}
		val, err := marketValue(ctx, s.pool, r.PlayerID)
		if err != nil {
			continue
		}
		_, _ = s.transfers.CreateListing(ctx,
			transfer.Actor{ManagerID: mgr},
			worldID,
			transfer.CreateListingInput{
				PlayerID:    r.PlayerID,
				AskingPrice: &val,
				ListingType: transfer.ListingOpenToOffers,
			})
	}
	return nil
}
