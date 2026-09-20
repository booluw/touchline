package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Admin: countries, leagues, seeding (S04-01)
// ---------------------------------------------------------------------------

type countryRequest struct {
	WorldID uuid.UUID `json:"world_id" binding:"required"`
	Code    string    `json:"code" binding:"required"`
	Name    string    `json:"name" binding:"required"`
}

func (s *server) handleCreateCountry(c *gin.Context) {
	var req countryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id, code and name are required"})
		return
	}
	country, err := s.compSvc.CreateCountry(c.Request.Context(), req.WorldID, req.Code, req.Name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, country)
}

func (s *server) handleListCountries(c *gin.Context) {
	worldID, err := uuid.Parse(c.Query("world_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id query parameter is required"})
		return
	}
	countries, err := s.compSvc.ListCountries(c.Request.Context(), worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"countries": countries})
}

func (s *server) handleCreateLeague(c *gin.Context) {
	var req internalcompetition.LeagueParams
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("%v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed league payload"})
		return
	}
	if req.CountryID == uuid.Nil || req.Name == "" || req.TeamCount < 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "country_id, name and team_count are required"})
		return
	}
	league, err := s.compSvc.CreateLeague(c.Request.Context(), req)
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrNameCollision),
		errors.Is(err, internalcompetition.ErrInvalidTeamCount),
		errors.Is(err, internalcompetition.ErrInvalidTier),
		errors.Is(err, internalcompetition.ErrInvalidCounts),
		errors.Is(err, internalcompetition.ErrBadAdjacency):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, league)
}

func (s *server) handleListLeagues(c *gin.Context) {
	worldID, err := uuid.Parse(c.Query("world_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "world_id query parameter is required"})
		return
	}
	leagues, err := s.compSvc.ListLeagues(c.Request.Context(), worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"leagues": leagues})
}

type adjacencyRequest struct {
	PromotesTo  *uuid.UUID `json:"promotes_to"`
	RelegatesTo *uuid.UUID `json:"relegates_to"`
}

func (s *server) handleUpdateAdjacency(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid league id"})
		return
	}
	var req adjacencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed adjacency payload"})
		return
	}
	err = s.compSvc.UpdateLeagueAdjacency(c.Request.Context(), leagueID, req.PromotesTo, req.RelegatesTo)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrBadAdjacency):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *server) handleSeedWorld(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	res, err := s.compSvc.SeedWorld(c.Request.Context(), worldID)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrWorldHasNoLeagues):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, res)
}

// ---------------------------------------------------------------------------
// Manager reads: the caller's own world only (S04-01)
// ---------------------------------------------------------------------------

func (s *server) handleMyCountries(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	countries, err := s.compSvc.ListCountries(c.Request.Context(), worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"countries": countries})
}

func (s *server) handleMyCompetitions(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	leagues, err := s.compSvc.ListLeagues(c.Request.Context(), worldID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"competitions": leagues})
}

func (s *server) handleGetCompetition(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid competition id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	league, err := s.compSvc.GetLeague(c.Request.Context(), worldID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, league)
}

func (s *server) handleGetFixtures(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid competition id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}

	var matchday *int
	if raw := c.Query("matchday"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			matchday = &v
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid matchday"})
			return
		}
	}

	fixtures, err := s.compSvc.GetFixtures(c.Request.Context(), leagueID, worldID, matchday)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"fixtures": fixtures})
}

func (s *server) handleGetStandings(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid competition id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	standings, err := s.compSvc.GetStandings(c.Request.Context(), leagueID, worldID)
	if errors.Is(err, internalcompetition.ErrNoSeason) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, standings)
}
