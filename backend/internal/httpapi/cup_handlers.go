package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Domestic cups (IM04)
// ---------------------------------------------------------------------------

// handleCreateCup declares a domestic cup under a world's country.
func (s *server) handleCreateCup(c *gin.Context) {
	var req internalcompetition.CupParams
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed cup payload"})
		return
	}
	if req.WorldID == uuid.Nil || req.CountryID == uuid.Nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id, country_id and name are required"})
		return
	}
	cup, err := s.compSvc.CreateCup(c.Request.Context(), req)
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrNameCollision),
		errors.Is(err, internalcompetition.ErrStagingInvalid):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, cup)
}

// handleStartCupCampaign starts a cup's first campaign from the country's
// league membership (admin).
func (s *server) handleStartCupCampaign(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	countryID, err := uuid.Parse(c.Param("countryID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid country id"})
		return
	}
	cupID, err := uuid.Parse(c.Param("cupID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cup id"})
		return
	}
	season, err := s.compSvc.StartCupCampaign(c.Request.Context(), worldID, countryID, cupID)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound),
		errors.Is(err, internalcompetition.ErrCountryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrCupCampaignExists),
		errors.Is(err, internalcompetition.ErrStagingInvalid),
		errors.Is(err, internalcompetition.ErrCupLimit):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, season)
}

// handleListCups returns every domestic cup of the caller's world.
func (s *server) handleListCups(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	cups, err := s.compSvc.ListCups(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cups": cups})
}

// handleGetCup returns the manager cup view (bracket, entries, champion).
func (s *server) handleGetCup(c *gin.Context) {
	cupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cup id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	cup, err := s.compSvc.GetCup(c.Request.Context(), worldID, cupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cup)
}
