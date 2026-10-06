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
		respondError(c, http.StatusBadRequest, "invalid_club_id", "invalid club id")
		return
	}
	var req addClubToLeagueRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.LeagueID == uuid.Nil {
		respondError(c, http.StatusBadRequest, "league_id_is_required", "league_id is required")
		return
	}

	admission, err := s.compSvc.AddClubToLeague(c.Request.Context(), worldID, clubID, req.LeagueID)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound),
		errors.Is(err, internalcompetition.ErrClubNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrClubWorldMismatch),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrClubAlreadyInLeague),
		errors.Is(err, internalcompetition.ErrLeagueFull):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	if admission.League.Country.ID != countryID {
		respondError(c, http.StatusUnprocessableEntity, "the_league_does_not_belong_to_this_country", "the league does not belong to this country")
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
		respondError(c, http.StatusBadRequest, "invalid_league_id", "invalid league id")
		return
	}
	var req setLeagueCapacityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_capacity_payload", "malformed capacity payload")
		return
	}
	if req.TeamCount == nil || req.Promotions == nil || req.Relegations == nil {
		respondError(c, http.StatusBadRequest, "team_count_promotions_and_relegations_are_required", "team_count, promotions and relegations are required")
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
		respondErr(c, http.StatusBadRequest, err)
		return
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondError(c, http.StatusNotFound, "league_not_found", "league not found")
		return
	case errors.Is(err, internalcompetition.ErrLeagueShrink),
		errors.Is(err, internalcompetition.ErrAdjacencyMismatch),
		errors.Is(err, internalcompetition.ErrBadAdjacency):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, league)
}
