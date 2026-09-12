package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

type bootstrapRequest struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

// handleBootstrap generates a provisioning world's material start state: an AI
// starter club, its policy-bot manager, and a generated squad (S03-01). The
// world must be provisioning and empty of clubs; the human manager takes the
// club later through the OPD-16 offer->accept path.
func (s *server) handleBootstrap(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	var req bootstrapRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	res, err := s.bootSvc.BootstrapWorld(c.Request.Context(), id, req.Name, req.ShortName)
	switch {
	case errors.Is(err, internalbootstrap.ErrWorldNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalbootstrap.ErrWorldNotProvisioning),
		errors.Is(err, internalbootstrap.ErrAlreadyBootstrapped):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalbootstrap.ErrRefDataMissing):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, res)
}

// handleListClubs lists the clubs in the caller's world. The JWT carries no
// world_id (OPD-15); the world is derived from the manager row.
func (s *server) handleListClubs(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	clubs, err := s.clubSvc.ListClubs(c.Request.Context(), worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"clubs": clubs})
}

// handleGetClub returns a club with its manager and squad, but only when the
// club is in the caller's own world — clubs never leak across worlds.
func (s *server) handleGetClub(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid club id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}

	detail, err := s.clubSvc.GetClubDetail(c.Request.Context(), id)
	switch {
	case errors.Is(err, internalclub.ErrClubNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if detail.WorldID != worldID {
		c.JSON(http.StatusNotFound, gin.H{"error": internalclub.ErrClubNotFound.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// callerWorld resolves the caller's world from their manager row. A JWT never
// carries a world_id (OPD-15); authenticated managers always have a row.
func (s *server) callerWorld(c *gin.Context) (uuid.UUID, error) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	var worldID uuid.UUID
	err := s.pool.QueryRow(c.Request.Context(),
		`SELECT world_id FROM manager.managers WHERE id = $1`, ident.ManagerID,
	).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errors.New("no world context")
	}
	return worldID, err
}
