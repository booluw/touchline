package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalmanager "github.com/touchline/backend/internal/manager"
	internalworld "github.com/touchline/backend/internal/world"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

type createWorldRequest struct {
	Name string `json:"name"`
}

// handleCreateWorld provisions a new world in 'provisioning'. An admin must
// launch it before gameplay operations are allowed.
func (s *server) handleCreateWorld(c *gin.Context) {
	var req createWorldRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	w, err := s.worldSvc.CreateWorld(c.Request.Context(), req.Name)
	switch {
	case errors.Is(err, internalworld.ErrNameCollision):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, w)
}

type worldStatusRequest struct {
	Status string `json:"status"`
}

// handleWorldStatus applies a lifecycle transition (active/paused/archived/
// open_beta). Playability follows from the resulting status.
func (s *server) handleWorldStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	var req worldStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}

	w, err := s.worldSvc.SetStatus(c.Request.Context(), id, req.Status)
	switch {
	case errors.Is(err, internalworld.ErrWorldNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalworld.ErrInvalidTransition):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, w)
}

type worldConfigRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// handleWorldConfig upserts a single world_config key at runtime. Cadence
// changes take effect once the scheduler re-reads config on its next poll.
func (s *server) handleWorldConfig(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	var req worldConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	err = s.worldSvc.SetConfig(c.Request.Context(), id, req.Key, req.Value)
	switch {
	case errors.Is(err, internalworld.ErrWorldNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type createOfferRequest struct {
	ClubID    uuid.UUID `json:"club_id"`
	ManagerID uuid.UUID `json:"manager_id"`
}

// handleCreateOffer lets an admin (on behalf of an AI club at game start) offer
// a job to an unemployed human manager in the club's world.
func (s *server) handleCreateOffer(c *gin.Context) {
	var req createOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ClubID == uuid.Nil || req.ManagerID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "club_id and manager_id are required"})
		return
	}

	o, err := s.mgrSvc.CreateJobOffer(c.Request.Context(), req.ClubID, req.ManagerID)
	switch {
	case errors.Is(err, internalmanager.ErrClubNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalmanager.ErrManagerUnavailable),
		errors.Is(err, internalmanager.ErrNotAIClub),
		errors.Is(err, internalmanager.ErrClubOccupied),
		errors.Is(err, internalmanager.ErrClubWorldMismatch),
		errors.Is(err, internalmanager.ErrOfferResolved),
		errors.Is(err, internalmanager.ErrClubNotPlayable):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, o)
}

// handleListOffers lists the signed-in manager's pending job offers. The world
// is resolved from the manager row — the JWT carries no world_id (OPD-15) — so
// a manager only ever sees offers aimed at their own session context.
func (s *server) handleListOffers(c *gin.Context) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	worldID, err := s.managerWorld(c.Request.Context(), ident.ManagerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	offers, err := s.mgrSvc.ListOffers(c.Request.Context(), ident.ManagerID, worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"offers": offers})
}

// managerWorld resolves the world bound to a manager row (the JWT's
// world-free session context, OPD-15).
func (s *server) managerWorld(ctx context.Context, managerID uuid.UUID) (uuid.UUID, error) {
	var worldID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT world_id FROM manager.managers WHERE id = $1`, managerID).Scan(&worldID)
	if err != nil {
		return uuid.Nil, err
	}
	return worldID, nil
}

// handleAcceptOffer hires the signed-in manager at the offering club.
func (s *server) handleAcceptOffer(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offer id"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	o, err := s.mgrSvc.AcceptJobOffer(c.Request.Context(), id, ident.ManagerID)
	switch {
	case errors.Is(err, internalmanager.ErrOfferNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalmanager.ErrOfferResolved),
		errors.Is(err, internalmanager.ErrNotOfferCandidate),
		errors.Is(err, internalmanager.ErrManagerEmployed),
		errors.Is(err, internalmanager.ErrClubNotPlayable):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, o)
}

// handleDeclineOffer declines a pending offer.
func (s *server) handleDeclineOffer(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offer id"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	o, err := s.mgrSvc.DeclineJobOffer(c.Request.Context(), id, ident.ManagerID)
	switch {
	case errors.Is(err, internalmanager.ErrOfferNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalmanager.ErrOfferResolved),
		errors.Is(err, internalmanager.ErrNotOfferCandidate):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, o)
}

// handleResign ends the signed-in manager's current assignment (self-service).
func (s *server) handleResign(c *gin.Context) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	err := s.mgrSvc.Resign(c.Request.Context(), ident.ManagerID)
	switch {
	case errors.Is(err, internalmanager.ErrNotEmployed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
