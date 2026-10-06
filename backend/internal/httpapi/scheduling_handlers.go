package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// schedulingUpdate is the IM05 admin surface for weekday-aware fixture
// calendars. `allowed_weekdays` is the ISO weekday set (1=Mon..7=Sun) the
// competition may play on; an empty array clears a per-competition override
// (restoring the fallback chain, no re-pacing) or clears a country default.
type schedulingUpdate struct {
	AllowedWeekdays []int `json:"allowed_weekdays"`
}

// handleUpdateCountryScheduling persists a country's weekday-default and
// re-paces every of its leagues that follows that default.
func (s *server) handleUpdateCountryScheduling(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	var in schedulingUpdate
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	res, err := s.compSvc.UpdateCountryScheduling(c.Request.Context(), worldID, countryID, in.AllowedWeekdays)
	if err != nil {
		switch {
		case errors.Is(err, internalcompetition.ErrCountryNotFound):
			respondError(c, http.StatusNotFound, "country_not_found_in_world", "country not found in world")
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, res)
}

// handleUpdateLeagueScheduling persists a league's weekday override and
// re-paces its unstarted matchdays.
func (s *server) handleUpdateLeagueScheduling(c *gin.Context) {
	var in schedulingUpdate
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_league_id", "invalid league id")
		return
	}
	res, updErr := s.compSvc.UpdateLeagueScheduling(c.Request.Context(), leagueID, in.AllowedWeekdays)
	if updErr != nil {
		switch {
		case errors.Is(updErr, internalcompetition.ErrCompetitionNotFound):
			respondError(c, http.StatusNotFound, "league_not_found", "league not found")
		case errors.Is(updErr, internalcompetition.ErrCompetitionTypeMismatch):
			respondError(c, http.StatusBadRequest, "competition_is_not_a_league", "competition is not a league")
		default:
			internalError(c, updErr)
		}
		return
	}
	c.JSON(http.StatusOK, res)
}

// handleUpdateCupScheduling persists a cup's weekday override; existing cup
// dates stand until the next round is materialized.
func (s *server) handleUpdateCupScheduling(c *gin.Context) {
	var in schedulingUpdate
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	cupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}
	res, updErr := s.compSvc.UpdateCupScheduling(c.Request.Context(), cupID, in.AllowedWeekdays)
	if updErr != nil {
		switch {
		case errors.Is(updErr, internalcompetition.ErrCompetitionNotFound):
			respondError(c, http.StatusNotFound, "cup_not_found", "cup not found")
		case errors.Is(updErr, internalcompetition.ErrCompetitionTypeMismatch):
			respondError(c, http.StatusBadRequest, "competition_is_not_a_domestic_cup", "competition is not a domestic cup")
		default:
			internalError(c, updErr)
		}
		return
	}
	c.JSON(http.StatusOK, res)
}
