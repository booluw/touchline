package player

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/explanation"
)

// Domain event types (S06-03), consumed by the outbox news/dispatch pipeline.
const (
	EventPlayerTransferRequested   = "PLAYER_TRANSFER_REQUESTED"
	EventTransferRequestApproved   = "TRANSFER_REQUEST_APPROVED"
	EventTransferRequestDenied     = "TRANSFER_REQUEST_DENIED"
	EventTransferRequestReassured  = "TRANSFER_REQUEST_REASSURED"
	EventTransferRequestAutoListed = "TRANSFER_REQUEST_AUTO_LISTED"
)

// RecordMatchAppearances persists the appearances of one completed match and
// refreshes each appearing player's whole-season share + morale. Callers own
// the surrounding transaction; in the live path it is the finalize side-effect
// pass (which commits its MATCH_PLAYED marker in the same transaction, so a
// re-finalize is a no-op and the pitch outcome and event log land together).
func (s *Service) RecordMatchAppearances(ctx context.Context, tx pgx.Tx, matchID uuid.UUID, appearances []Appearance) error {
	for _, a := range appearances {
		if err := insertAppearance(ctx, tx, matchID, a); err != nil {
			return err
		}
	}
	// Share first, then pull morale toward the role expectation with the
	// refreshed season picture.
	for _, a := range appearances {
		if _, err := refreshShare(ctx, tx, a.PlayerID); err != nil {
			return err
		}
		vars, err := loadMoraleVars(ctx, tx, a.PlayerID)
		if err != nil {
			return err
		}
		next := UpdateMorale(vars.Current, vars.Share, ExpectedShareForRole(vars.Role), vars.personality())
		if err := upsertMorale(ctx, tx, a.PlayerID, next); err != nil {
			return err
		}
	}
	return nil
}

// ListSquadMorale returns the club's active roster with each player's role,
// morale, whole-season share, six attribute-category means, position-weighted
// overall and any open transfer request.
func (s *Service) ListSquadMorale(ctx context.Context, worldID, managerID uuid.UUID) ([]PlayerMoraleRow, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	rows, err := squadRows(ctx, s.pool, clubID)
	if err != nil {
		return nil, err
	}
	open, err := openRequestsByClub(ctx, s.pool, clubID)
	if err != nil {
		return nil, err
	}
	attrs, err := squadAttributeMeans(ctx, s.pool, clubID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		ids[i] = rows[i].Player.ID
	}
	hidden, err := squadHiddenAttributes(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	dossiers, err := loadDossiers(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	ratings, err := squadRecentRatings(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	// Same eligibility gate (and date) as tactics.SetLineup.
	loaded, err := squad.NewStore(s.pool).LoadSquad(ctx, clubID, time.Now())
	if err != nil {
		return nil, err
	}
	eligibility := make(map[uuid.UUID]squad.LoadedPlayer, len(loaded))
	for _, p := range loaded {
		eligibility[p.PlayerID] = p
	}
	var byPlayer map[uuid.UUID]string
	if len(open) > 0 {
		byPlayer = make(map[uuid.UUID]string, len(open))
		for _, r := range open {
			byPlayer[r.PlayerID] = r.Status
		}
	}
	for i := range rows {
		if st, ok := byPlayer[rows[i].Player.ID]; ok {
			rows[i].TransferRequest = st
		}
		rows[i].Hidden = hidden[rows[i].Player.ID]
		rows[i].Dossier = dossiers[rows[i].Player.ID]
		rows[i].RecentRatings = ratings[rows[i].Player.ID]
		if rows[i].RecentRatings == nil {
			rows[i].RecentRatings = []int{}
		}
		rows[i].Available, rows[i].UnavailableReason = availability(eligibility[rows[i].Player.ID])
		att := attrs[rows[i].Player.ID]
		rows[i].Attributes = att
		rows[i].Overall = squad.PositionalOverall(rows[i].Position, squad.AttributeSnapshot{
			Technical:   att.Technical,
			Physical:    att.Physical,
			Mental:      att.Mental,
			Tactical:    att.Tactical,
			Goalkeeping: att.Goalkeeping,
			Positional:  att.Positional,
		})
	}
	return rows, nil
}

// GetPlayerMoraleDetail returns one player's morale picture plus the "why"
// (role expectations vs share), any open request and the durable
// player↔manager memory behind it.
func (s *Service) GetPlayerMoraleDetail(ctx context.Context, worldID, managerID, playerID uuid.UUID) (*PlayerMoraleDetail, error) {
	clubID, err := s.clubByManager(ctx, worldID, managerID)
	if err != nil {
		return nil, err
	}
	prof, err := playerProfile(ctx, s.pool, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.ClubID == nil || *prof.ClubID != clubID || prof.WorldID != worldID {
		return nil, ErrPlayerNotInClub
	}

	vars, err := loadMoraleVars(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	expected := ExpectedShareForRole(vars.Role)
	status := statusFor(vars.Share, expected)

	d := &PlayerMoraleDetail{
		Player:         &apiref.PlayerRef{ID: prof.ID, Name: prof.DisplayName},
		FirstName:      prof.FirstName,
		LastName:       prof.LastName,
		Position:       prof.PrimaryPosition,
		SquadRole:      vars.Role,
		Morale:         vars.Current,
		PlayingTimePct: vars.Share,
		Expectations: []MoraleExpectation{{
			Label:    roleLabel(vars.Role),
			Expected: expectedLabel(expected),
			Current:  vars.Share,
			Status:   status,
		}},
		Explanation: map[string]any{
			"role":                roleLabel(vars.Role),
			"expected_share":      expected,
			"current_share":       vars.Share,
			"satisfaction_status": status,
			"morale_target":       moraleTarget(vars.Share, expected, vars.personality()),
			"swing_alpha":         swingAlpha(vars.personality()),
			// IM64: signed terms of morale_target, 0–100 relative to neutral 50.
			"why": moraleTargetExplanation(vars.Share, expected, vars.personality()),
		},
	}
	req, err := latestTransferRequest(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	if req != nil && req.Status != TransferRequestWithdrawn {
		if err := decorateRequestRefs(ctx, s.pool, req); err != nil {
			return nil, err
		}
		d.TransferRequest = req
	}
	hist, err := relationshipEvents(ctx, s.pool, playerID)
	if err != nil {
		return nil, err
	}
	d.RelationshipEvents = hist
	if d.Hidden, err = playerHiddenAttributes(ctx, s.pool, playerID); err != nil {
		return nil, err
	}
	if d.Dossier, err = playerDossier(ctx, s.pool, playerID, true); err != nil {
		return nil, err
	}
	return d, nil
}

func statusFor(share, expected float64) string {
	if expected <= 0 {
		return "free"
	}
	if Satisfied(share, expected) {
		return "satisfied"
	}
	if share >= TransferRequestDeepShare*expected {
		return "neutral"
	}
	return "unhappy"
}

func roleLabel(role string) string {
	switch role {
	case SquadRoleKeyPlayer:
		return "Key player"
	case SquadRoleRotation:
		return "Rotation"
	case SquadRoleDevelopment:
		return "Development"
	default:
		return "Squad player"
	}
}

func expectedLabel(expected float64) string {
	switch expected {
	case RoleExpectedShareKeyPlayer:
		return "Plays ~3 of 4 matches"
	case RoleExpectedShareRotation:
		return "Plays every other match"
	case RoleExpectedShareDevelopment:
		return "Development minutes"
	default:
		return "Occasional minutes"
	}
}

// requestExplanation builds the player-facing "why" for a transfer request, in
// the shared §54 explanation shape.
func requestExplanation(playerID uuid.UUID, morale, share, expected float64) *explanation.Explanation {
	return explanation.New("player_transfer_request", -int(morale*100)).
		Add("morale", -int((1-morale)*100)).
		Add("playing time satisfaction", int(share*100)-int(expected*100))
}

// availability renders the lineup gate for a roster row.
func availability(p squad.LoadedPlayer) (bool, string) {
	switch {
	case p.Available:
		return true, ""
	case p.Injured:
		return false, "injured"
	default:
		return false, "ineligible"
	}
}
