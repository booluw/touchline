package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Admin: league membership controls (IM14)
//
// Both endpoints are pure declarations: they never touch the live season. A
// club added to a league joins in at the NEXT season composition; a capacity
// change sets team_count / promotions / relegations for the next rollover.
// ---------------------------------------------------------------------------

// addClubToLeagueRequest picks the target league for an existing league-less
// club; the country and world scope come from the URL.
type addClubToLeagueRequest struct {
	LeagueID uuid.UUID `json:"league_id" binding:"required"`
}

// handleAdminAddClubToLeague admits an existing league-less club into a league
// (IM14). The club's country text is normalised to the league's country and
// its membership binds at the next season composition, audited as
// CLUB_JOINED_LEAGUE.
func (s *server) handleAdminAddClubToLeague(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	clubID, err := uuid.Parse(c.Param("clubID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid club id"})
		return
	}
	var req addClubToLeagueRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.LeagueID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "league_id is required"})
		return
	}

	admission, err := s.compSvc.AddClubToLeague(c.Request.Context(), worldID, clubID, req.LeagueID)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound),
		errors.Is(err, internalcompetition.ErrClubNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrClubWorldMismatch),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrClubAlreadyInLeague),
		errors.Is(err, internalcompetition.ErrLeagueFull):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	if admission.League.Country.ID != countryID {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "the league does not belong to this country"})
		return
	}
	c.JSON(http.StatusOK, admission)
}

// setLeagueCapacityRequest declares a league's next-season team_count and its
// promotion/relegation counts; neighbours' reciprocal counts auto-adjust.
type setLeagueCapacityRequest struct {
	TeamCount   *int `json:"team_count"`
	Promotions  *int `json:"promotions"`
	Relegations *int `json:"relegations"`
}

// handleSetLeagueCapacity raises a league's team_count and sets the
// promotions/relegations applied at the next rollover (IM14, upward-only), and
// auto-adjusts the neighbouring leagues' reciprocal counts in the same
// transaction. Audited as LEAGUE_CAPACITY_CHANGED.
func (s *server) handleSetLeagueCapacity(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid league id"})
		return
	}
	var req setLeagueCapacityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed capacity payload"})
		return
	}
	if req.TeamCount == nil || req.Promotions == nil || req.Relegations == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "team_count, promotions and relegations are required"})
		return
	}

	league, err := s.compSvc.SetLeagueCapacity(c.Request.Context(), leagueID, internalcompetition.CapacityParams{
		TeamCount:   *req.TeamCount,
		Promotions:  *req.Promotions,
		Relegations: *req.Relegations,
	})
	switch {
	case errors.Is(err, internalcompetition.ErrInvalidTeamCount),
		errors.Is(err, internalcompetition.ErrInvalidCounts):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "league not found"})
		return
	case errors.Is(err, internalcompetition.ErrLeagueShrink),
		errors.Is(err, internalcompetition.ErrAdjacencyMismatch),
		errors.Is(err, internalcompetition.ErrBadAdjacency):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, league)
}
