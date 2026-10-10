package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalplayer "github.com/touchline/backend/internal/player"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

func playerStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalplayer.ErrPlayerNotFound):
		respondError(c, http.StatusNotFound, "player_not_found", "player not found")
	case errors.Is(err, internalplayer.ErrManagerHasNoClub):
		respondError(c, http.StatusConflict, "no_active_club", "no active club")
	case errors.Is(err, internalplayer.ErrRequestNotFound):
		respondError(c, http.StatusNotFound, "no_open_transfer_request", "no open transfer request")
	case errors.Is(err, internalplayer.ErrNoOpenInjury):
		respondError(c, http.StatusNotFound, "no_open_injury", "no open injury")
	case errors.Is(err, internalplayer.ErrPlayerNotInClub):
		respondError(c, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, internalplayer.ErrRequestResolved):
		respondError(c, http.StatusConflict, "request_already_resolved", "request already resolved")
	default:
		internalError(c, err)
	}
}

// handleGetPlayerProfile returns one player's card: identity, current club,
// ability (six attribute means + the position-weighted overall), the career
// record, and the weekly wage when the player is at the caller's own club
// (GET /api/players/:playerID).
//
// World-scoped, not club-scoped: a manager may open any player in their own
// world — a cup opponent, a rival's star, a signing target — so this is the
// entry point for the player detail page. A player in another world is a plain
// 404, identical to a player that does not exist, so the endpoint never
// confirms another world's players.
func (s *server) handleGetPlayerProfile(c *gin.Context) {
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	profile, err := s.playerSvc.GetPlayerDetail(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

// handleListClubPlayers returns the calling manager's roster with morale,
// playing time and any open transfer request (GET /api/clubs/:id/players).
func (s *server) handleListClubPlayers(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	rows, err := s.playerSvc.ListSquadMorale(c.Request.Context(), worldID, ident.ManagerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"club_id": clubID, "players": rows})
}

// handleGetPlayerMorale returns the full morale picture of one player plus the
// "why" (GET /api/clubs/:id/players/:playerID).
func (s *server) handleGetPlayerMorale(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID // ownership already enforced inside the player service via manager
	detail, err := s.playerSvc.GetPlayerMoraleDetail(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// handleGetPlayerDevelopment returns one owned player's observable S08-02
// development trajectory, drivers and recent attribute movement — never the
// hidden potential ceiling (GET /api/clubs/:id/players/:playerID/development).
func (s *server) handleGetPlayerDevelopment(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID // ownership already enforced inside the player service via manager
	detail, err := s.playerSvc.GetPlayerDevelopmentDetail(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// handleGetPlayerInjury returns the owning player's open injury with the
// read-time derived recovery progress, or null when fit
// (GET /api/clubs/:id/players/:playerID/injury).
func (s *server) handleGetPlayerInjury(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID // ownership already enforced inside the player service via manager
	v, err := s.playerSvc.GetPlayerInjury(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// handleRushReturn closes the owning player's open injury early, raising the
// recurrence risk (POST /api/clubs/:id/players/:playerID/rush-return).
func (s *server) handleRushReturn(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID // ownership already enforced inside the player service via manager
	v, err := s.playerSvc.RushReturn(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// handlePromisePlayingTime records an explicit playing-time promise to the
// player (POST /api/clubs/:id/players/:playerID/promise-playing-time).
func (s *server) handlePromisePlayingTime(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID
	if err := s.playerSvc.PromisePlayingTime(c.Request.Context(), worldID, ident.ManagerID, playerID); err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleApproveTransferRequest honours the player's open request: the player
// gets listed on the market automatically (POST .../transfer-request/approve).
func (s *server) handleApproveTransferRequest(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	// IM65: optional {"price_preset": quick_sale|valuation|hold_out}; default valuation.
	var body struct {
		PricePreset string `json:"price_preset"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid_body", "invalid request body")
			return
		}
	}
	if body.PricePreset == "" {
		body.PricePreset = "valuation"
	}
	multiplier, ok := internalplayer.AskingPricePresets[body.PricePreset]
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid_price_preset", "price_preset must be quick_sale, valuation or hold_out")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID
	view, err := s.playerSvc.ApprovePlayerRequest(c.Request.Context(), worldID, ident.ManagerID, playerID, multiplier)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// handleDenyTransferRequest rejects the player's open request: the player takes
// a morale hit and cools down (POST .../transfer-request/deny).
func (s *server) handleDenyTransferRequest(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID
	req, err := s.playerSvc.DenyPlayerRequest(c.Request.Context(), worldID, ident.ManagerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}

// playerAction parses the club/player/world/identity every per-player action
// shares; false means a response was already written.
func (s *server) playerAction(c *gin.Context) (worldID, managerID, playerID uuid.UUID, ok bool) {
	if _, ok = clubParam(c); !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_player_id", "invalid player id")
		return worldID, managerID, playerID, false
	}
	worldID, err = s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return worldID, managerID, playerID, false
	}
	return worldID, c.MustGet(identityKey).(*pkgjwt.ManagerIdentity).ManagerID, playerID, true
}

// handleTransferRequestPreview returns the consequences of each answer to the
// player's open request (IM65; GET .../transfer-request/preview).
func (s *server) handleTransferRequestPreview(c *gin.Context) {
	worldID, managerID, playerID, ok := s.playerAction(c)
	if !ok {
		return
	}
	p, err := s.playerSvc.TransferRequestPreview(c.Request.Context(), worldID, managerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// handleReassureTransferRequest pauses the player's open request with a
// playing-time promise (IM65; POST .../transfer-request/reassure).
func (s *server) handleReassureTransferRequest(c *gin.Context) {
	worldID, managerID, playerID, ok := s.playerAction(c)
	if !ok {
		return
	}
	req, err := s.playerSvc.ReassurePlayerRequest(c.Request.Context(), worldID, managerID, playerID)
	if err != nil {
		playerStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}
