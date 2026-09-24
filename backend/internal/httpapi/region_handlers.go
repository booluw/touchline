package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Admin: world regions, country-to-region assignment, league reputation (IM06)
// ---------------------------------------------------------------------------

// regionRequest is the admin region create body. On rename only Name is used.
type regionRequest struct {
	WorldID uuid.UUID `json:"world_id"`
	Name    string    `json:"name"`
}

// countryRegionRequest assigns a country to a region; a null or omitted
// region_id clears the assignment.
type countryRegionRequest struct {
	RegionID *uuid.UUID `json:"region_id"`
}

// leagueReputationRequest sets a league's reputation (0..100).
type leagueReputationRequest struct {
	Reputation *int `json:"reputation"`
}

// handleCreateRegion declares a world-scoped region.
func (s *server) handleCreateRegion(c *gin.Context) {
	var req regionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.WorldID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id is required"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "region name is required"})
		return
	}
	region, err := s.compSvc.CreateRegion(c.Request.Context(), req.WorldID, req.Name)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrRegionNameCollision):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, region)
}

// handleListRegions lists a world's regions (world_id query parameter).
func (s *server) handleListRegions(c *gin.Context) {
	worldID, err := uuid.Parse(c.Query("world_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id query parameter is required"})
		return
	}
	regions, err := s.compSvc.ListRegions(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"regions": regions})
}

// handleRenameRegion renames a region (world inherited from the region row).
func (s *server) handleRenameRegion(c *gin.Context) {
	regionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid region id"})
		return
	}
	var req regionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "region name is required"})
		return
	}
	region, err := s.compSvc.RenameRegion(c.Request.Context(), regionID, req.Name)
	switch {
	case errors.Is(err, internalcompetition.ErrRegionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrRegionNameCollision):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, region)
}

// handleDeleteRegion removes a region; its countries fall back to unassigned.
func (s *server) handleDeleteRegion(c *gin.Context) {
	regionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid region id"})
		return
	}
	if err := s.compSvc.DeleteRegion(c.Request.Context(), regionID); err != nil {
		if errors.Is(err, internalcompetition.ErrRegionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// handleSetCountryRegion assigns (or clears) a country's region.
func (s *server) handleSetCountryRegion(c *gin.Context) {
	countryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid country id"})
		return
	}
	var req countryRegionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	country, err := s.compSvc.SetCountryRegion(c.Request.Context(), countryID, req.RegionID)
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "country not found"})
		return
	case errors.Is(err, internalcompetition.ErrRegionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrRegionWorldMismatch):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, country)
}

// handleSetLeagueReputation persists a league's admin-managed reputation.
func (s *server) handleSetLeagueReputation(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid league id"})
		return
	}
	var req leagueReputationRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Reputation == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reputation is required"})
		return
	}
	league, err := s.compSvc.SetLeagueReputation(c.Request.Context(), leagueID, *req.Reputation)
	switch {
	case errors.Is(err, internalcompetition.ErrReputationOutOfRange):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "league not found"})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, league)
}
