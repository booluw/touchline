package player

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/pkg/explanation"
)

type requestView struct {
	Request *TransferRequest  `json:"request"`
	Listing *transfer.Listing `json:"listing,omitempty"`
}

// ApproveTransferRequest honors a player's open request: the request closes as
// approved, the player↔manager memory records it, and the player is listed on
// the market at their market value (the listing lives in its own transaction
// after the approval commits, mirroring the transfer seller flow).
func (s *Service) ApproveTransferRequest(ctx context.Context, worldID, managerID, requestID uuid.UUID) (requestView, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return requestView{}, err
	}
	req, err := s.resolveAs(ctx, worldID, managerID, clubID, requestID, TransferRequestApproved,
		EventTransferRequestApproved, RelationshipTransferApproved, SentimentTransferApproved,
		"transfer_request_approved", nil)
	if err != nil {
		return requestView{}, err
	}

	if s.transfers != nil {
		val, err := marketValue(ctx, s.pool, req.PlayerID)
		if err != nil {
			return requestView{Request: req}, err
		}
		listing, lerr := s.transfers.CreateListing(ctx,
			transfer.Actor{ManagerID: managerID},
			worldID,
			transfer.CreateListingInput{
				PlayerID:    req.PlayerID,
				AskingPrice: &val,
				ListingType: transfer.ListingOpenToOffers,
			})
		if lerr != nil {
			return requestView{Request: req}, fmt.Errorf("list approved player: %w", lerr)
		}
		return requestView{Request: req, Listing: listing}, nil
	}
	return requestView{Request: req}, nil
}

// DenyTransferRequest rejects the request in the same transaction as the
// morale hit + cooldown, then records the denial on the player's memory.
func (s *Service) DenyTransferRequest(ctx context.Context, worldID, managerID, requestID uuid.UUID) (*TransferRequest, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	return s.resolveAs(ctx, worldID, managerID, clubID, requestID, TransferRequestDenied,
		EventTransferRequestDenied, RelationshipTransferDenied, SentimentTransferDenied,
		"transfer_request_denied", func(ctx context.Context, tx pgx.Tx, req *TransferRequest) error {
			if err := setCooldown(ctx, tx, req.PlayerID, DenyCooldownDays, false); err != nil {
				return err
			}
			vars, err := loadMoraleVars(ctx, tx, req.PlayerID)
			if err != nil {
				return err
			}
			next := vars.Current - DenyMoraleDrop
			if next < 0 {
				next = 0
			}
			return upsertMorale(ctx, tx, req.PlayerID, round4(next))
		})
}

// ReassurePlayer answers the request with a playing-time promise: the request
// pauses, a deadline-anchored increase_playing_time promise lands on the
// player, and the memory records the gesture.
func (s *Service) ReassurePlayer(ctx context.Context, worldID, managerID, requestID uuid.UUID) (*TransferRequest, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	return s.resolveAs(ctx, worldID, managerID, clubID, requestID, TransferRequestReassured,
		EventTransferRequestReassured, RelationshipReassured, SentimentReassured,
		"transfer_request_reassured", func(ctx context.Context, tx pgx.Tx, req *TransferRequest) error {
			if err := reassureRequest(ctx, tx, req.ID, ReassureCooldownDays); err != nil {
				return err
			}
			if err := setCooldown(ctx, tx, req.PlayerID, ReassureCooldownDays, false); err != nil {
				return err
			}
			return insertPromise(ctx, tx, worldID, req.PlayerID, req.ManagerID, PromiseEvaluationWeeks*7)
		})
}

// resolveAs is the shared request-close path: validate ownership/openness,
// flip the status, run optional side effects (cooldowns, promises, morale) and
// emit the audit event + boundary memory — all in the same transaction.
func (s *Service) resolveAs(ctx context.Context, worldID, managerID, clubID, requestID uuid.UUID,
	status, eventType, relType string, delta int, subject string,
	adjust func(ctx context.Context, tx pgx.Tx, req *TransferRequest) error,
) (*TransferRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	req, err := transferRequestByID(ctx, tx, requestID, true)
	if err != nil {
		return nil, err
	}
	if req.ClubID != clubID {
		return nil, ErrPlayerNotInClub
	}
	if req.Status != TransferRequestPending {
		return nil, ErrRequestResolved
	}
	if adjust != nil {
		if err := adjust(ctx, tx, req); err != nil {
			return nil, err
		}
	}
	if err := resolveRequest(ctx, tx, req.ID, status); err != nil {
		return nil, err
	}

	exp := explanation.New(subject, delta).Add("transfer request", delta)
	rawExp, err := json.Marshal(exp)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"request_id": req.ID, "player_id": req.PlayerID, "club_id": req.ClubID, "reason": req.Reason,
	})
	if err != nil {
		return nil, err
	}
	tick, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	eventID, err := s.recordEvent(ctx, tx, worldID, tick, "manager", managerID, eventType, payload, rawExp)
	if err != nil {
		return nil, err
	}
	if err := recordRelationshipEvent(ctx, tx, worldID, req.PlayerID, managerID, relType, delta, &eventID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	req.Status = status
	return req, nil
}

// ApprovePlayerRequest resolves the latest pending request of a player
// (HTTP-facing; no requestID needed).
func (s *Service) ApprovePlayerRequest(ctx context.Context, worldID, managerID, playerID uuid.UUID) (requestView, error) {
	return s.resolveByPlayer(ctx, worldID, managerID, playerID, TransferRequestApproved,
		EventTransferRequestApproved, RelationshipTransferApproved, SentimentTransferApproved,
		"transfer_request_approved", true)
}

// DenyPlayerRequest resolves the latest pending request of a player
// (HTTP-facing; no requestID needed).
func (s *Service) DenyPlayerRequest(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*TransferRequest, error) {
	view, err := s.resolveByPlayer(ctx, worldID, managerID, playerID, TransferRequestDenied,
		EventTransferRequestDenied, RelationshipTransferDenied, SentimentTransferDenied,
		"transfer_request_denied", false)
	if err != nil {
		return nil, err
	}
	return view.Request, nil
}

func (s *Service) resolveByPlayer(ctx context.Context, worldID, managerID, playerID uuid.UUID,
	status, eventType, relType string, delta int, subject string, isApprove bool,
) (requestView, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return requestView{}, err
	}
	defer tx.Rollback(ctx)

	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return requestView{}, err
	}
	playerClub, err := playerClubID(ctx, tx, playerID)
	if err != nil || playerClub != clubID {
		return requestView{}, ErrPlayerNotInClub
	}
	req, err := openTransferRequest(ctx, tx, playerID)
	if err != nil {
		return requestView{}, err
	}
	if req == nil {
		return requestView{}, ErrRequestNotFound
	}
	var adjust func(ctx context.Context, tx pgx.Tx, r *TransferRequest) error
	if !isApprove {
		adjust = func(ctx context.Context, tx pgx.Tx, r *TransferRequest) error {
			if err := setCooldown(ctx, tx, r.PlayerID, DenyCooldownDays, false); err != nil {
				return err
			}
			vars, err := loadMoraleVars(ctx, tx, r.PlayerID)
			if err != nil {
				return err
			}
			next := vars.Current - DenyMoraleDrop
			if next < 0 {
				next = 0
			}
			return upsertMorale(ctx, tx, r.PlayerID, round4(next))
		}
	}
	if adjust != nil {
		if err := adjust(ctx, tx, req); err != nil {
			return requestView{}, err
		}
	}
	if err := resolveRequest(ctx, tx, req.ID, status); err != nil {
		return requestView{}, err
	}
	tick, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return requestView{}, err
	}
	rawExp, err := explanationJSON(subject, delta)
	if err != nil {
		return requestView{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"request_id": req.ID, "player_id": playerID, "club_id": clubID, "reason": req.Reason, "status": status,
	})
	if err != nil {
		return requestView{}, err
	}
	eventID, err := s.recordEvent(ctx, tx, worldID, tick, "manager", managerID, eventType, payload, rawExp)
	if err != nil {
		return requestView{}, err
	}
	if err := recordRelationshipEvent(ctx, tx, worldID, playerID, managerID, relType, delta, &eventID); err != nil {
		return requestView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return requestView{}, err
	}
	req.Status = status
	view := requestView{Request: req}
	if isApprove && s.transfers != nil {
		val, err := marketValue(ctx, s.pool, playerID)
		if err != nil {
			return view, err
		}
		listing, lerr := s.transfers.CreateListing(ctx, transfer.Actor{ManagerID: managerID}, worldID,
			transfer.CreateListingInput{PlayerID: playerID, AskingPrice: &val, ListingType: transfer.ListingOpenToOffers})
		if lerr != nil {
			return view, fmt.Errorf("list approved player: %w", lerr)
		}
		view.Listing = listing
	}
	return view, nil
}

func playerClubID(ctx context.Context, q dbtx, playerID uuid.UUID) (uuid.UUID, error) {
	var clubID uuid.UUID
	err := q.QueryRow(ctx, `SELECT club_id FROM player.players WHERE id = $1`, playerID).Scan(&clubID)
	if err != nil {
		return uuid.Nil, err
	}
	return clubID, nil
}

func explanationJSON(subject string, delta int) ([]byte, error) {
	return json.Marshal(explanation.New(subject, delta).Add("transfer request", delta))
}

// PromisePlayingTime records the manager's promise to the player (explicit,
// human-facing action). The promise is then evaluated weekly: share that meets
// the role entitlement is fulfilled (+10 memory), overdue promise broken (−30).
func (s *Service) PromisePlayingTime(ctx context.Context, worldID, managerID, playerID uuid.UUID) error {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return err
	}
	pc, err := playerClubID(ctx, s.pool, playerID)
	if err != nil || pc != clubID {
		return ErrPlayerNotInClub
	}
	return insertPromise(ctx, s.pool, worldID, playerID, managerID, PromiseEvaluationWeeks*7)
}

// OnPlayerTransferred is the transfer-completion hook (wired through
// transfer.Service's lifecycle interface): a fresh start for a new club —
// morale resets high, the season share resets, and no open transfer request
// follows the player. Runs inside the transfer's completion transaction.
func (s *Service) OnPlayerTransferred(ctx context.Context, tx pgx.Tx, playerID, newClubID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, morale, playing_time_pct, updated_at)
		VALUES ($1, $2, 0, now())
		ON CONFLICT (player_id) DO UPDATE
		SET morale = EXCLUDED.morale,
		    playing_time_pct = 0,
		    transfer_request_cooldown_until = NULL,
		    updated_at = now()`,
		playerID, FreshStartMorale)
	if err != nil {
		return err
	}
	return cancelOpenRequest(ctx, tx, playerID)
}
