package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalplayer "github.com/touchline/backend/internal/player"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

func playerStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalplayer.ErrPlayerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
	case errors.Is(err, internalplayer.ErrManagerHasNoClub):
		c.JSON(http.StatusConflict, gin.H{"error": "no active club"})
	case errors.Is(err, internalplayer.ErrRequestNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "no open transfer request"})
	case errors.Is(err, internalplayer.ErrPlayerNotInClub):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, internalplayer.ErrRequestResolved):
		c.JSON(http.StatusConflict, gin.H{"error": "request already resolved"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
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
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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

// handlePromisePlayingTime records an explicit playing-time promise to the
// player (POST /api/clubs/:id/players/:playerID/promise-playing-time).
func (s *server) handlePromisePlayingTime(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	_ = clubID
	view, err := s.playerSvc.ApprovePlayerRequest(c.Request.Context(), worldID, ident.ManagerID, playerID)
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
