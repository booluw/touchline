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
		respondError(c, http.StatusBadRequest, "world_id_is_required", "world_id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusUnprocessableEntity, "region_name_is_required", "region name is required")
		return
	}
	region, err := s.compSvc.CreateRegion(c.Request.Context(), req.WorldID, req.Name)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrRegionNameCollision):
		respondErr(c, http.StatusConflict, err)
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
		respondError(c, http.StatusBadRequest, "world_id_query_parameter_is_required", "world_id query parameter is required")
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
		respondError(c, http.StatusBadRequest, "invalid_region_id", "invalid region id")
		return
	}
	var req regionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		respondError(c, http.StatusUnprocessableEntity, "region_name_is_required", "region name is required")
		return
	}
	region, err := s.compSvc.RenameRegion(c.Request.Context(), regionID, req.Name)
	switch {
	case errors.Is(err, internalcompetition.ErrRegionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrRegionNameCollision):
		respondErr(c, http.StatusConflict, err)
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
		respondError(c, http.StatusBadRequest, "invalid_region_id", "invalid region id")
		return
	}
	if err := s.compSvc.DeleteRegion(c.Request.Context(), regionID); err != nil {
		if errors.Is(err, internalcompetition.ErrRegionNotFound) {
			respondErr(c, http.StatusNotFound, err)
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
		respondError(c, http.StatusBadRequest, "invalid_country_id", "invalid country id")
		return
	}
	var req countryRegionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	country, err := s.compSvc.SetCountryRegion(c.Request.Context(), countryID, req.RegionID)
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound):
		respondError(c, http.StatusNotFound, "country_not_found", "country not found")
		return
	case errors.Is(err, internalcompetition.ErrRegionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrRegionWorldMismatch):
		respondErr(c, http.StatusUnprocessableEntity, err)
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
		respondError(c, http.StatusBadRequest, "invalid_league_id", "invalid league id")
		return
	}
	var req leagueReputationRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Reputation == nil {
		respondError(c, http.StatusBadRequest, "reputation_is_required", "reputation is required")
		return
	}
	league, err := s.compSvc.SetLeagueReputation(c.Request.Context(), leagueID, *req.Reputation)
	switch {
	case errors.Is(err, internalcompetition.ErrReputationOutOfRange):
		respondErr(c, http.StatusBadRequest, err)
		return
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondError(c, http.StatusNotFound, "league_not_found", "league not found")
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, league)
}
