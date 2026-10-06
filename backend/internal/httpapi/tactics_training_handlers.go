package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalmatch "github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/training"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

func (s *server) commandActor(c *gin.Context) (tactics.Actor, error) {
	ident, ok := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	if !ok {
		return tactics.Actor{}, errors.New("unauthenticated")
	}
	var bot bool
	if err := s.pool.QueryRow(c.Request.Context(), `SELECT is_policy_bot FROM manager.managers WHERE id = $1`, ident.ManagerID).Scan(&bot); err != nil {
		return tactics.Actor{}, err
	}
	return tactics.Actor{ManagerID: ident.ManagerID, IsPolicyBot: bot}, nil
}

func clubParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_club_id", "invalid club id")
		return uuid.Nil, false
	}
	return id, true
}

func tacticStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, tactics.ErrClubNotFound):
		respondError(c, http.StatusNotFound, "club_not_found", "club not found")
	case errors.Is(err, tactics.ErrNotOwned):
		respondError(c, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, tactics.ErrFixtureLive):
		respondErr(c, http.StatusConflict, err)
	case errors.Is(err, tactics.ErrInvalidStyle) || errors.Is(err, tactics.ErrInvalidFormation) || errors.Is(err, tactics.ErrInvalidLineup):
		respondErr(c, http.StatusBadRequest, err)
	case errors.Is(err, tactics.ErrPlayerUnavailable):
		respondErr(c, http.StatusUnprocessableEntity, err)
	default:
		internalError(c, err)
	}
}

func (s *server) handleGetLineup(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	v, err := s.tacticsSvc.GetLineup(c.Request.Context(), id)
	if err != nil {
		tacticStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (s *server) handleGetTactics(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	v, err := s.tacticsSvc.GetTactics(c.Request.Context(), id)
	if err != nil {
		tacticStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (s *server) handleSetLineup(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	var req struct {
		Slots []tactics.LineupInput `json:"slots"`
	}
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, 400, "invalid_lineup", "invalid lineup")
		return
	}
	a, err := s.commandActor(c)
	if err != nil {
		respondError(c, 401, "unauthenticated", "unauthenticated")
		return
	}
	if err = s.tacticsSvc.SetLineup(c.Request.Context(), a, id, req.Slots); err != nil {
		tacticStatus(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (s *server) handleSetTactics(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	var req struct {
		Style     string `json:"style"`
		Formation string `json:"formation"`
	}
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, 400, "invalid_tactics", "invalid tactics")
		return
	}
	a, err := s.commandActor(c)
	if err != nil {
		respondError(c, 401, "unauthenticated", "unauthenticated")
		return
	}
	if err = s.tacticsSvc.SetTactics(c.Request.Context(), a, id, req.Style, req.Formation); err != nil {
		tacticStatus(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (s *server) handleGetTrainingPlan(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	v, err := s.trainingSvc.GetPlan(c.Request.Context(), id)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, v)
}
func (s *server) handleSetTrainingPlan(c *gin.Context) {
	id, ok := clubParam(c)
	if !ok {
		return
	}
	var req struct {
		Archetype string `json:"archetype"`
	}
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, 400, "invalid_training_plan", "invalid training plan")
		return
	}
	a, err := s.commandActor(c)
	if err != nil {
		respondError(c, 401, "unauthenticated", "unauthenticated")
		return
	}
	ta := training.Actor{ManagerID: a.ManagerID, IsPolicyBot: a.IsPolicyBot}
	if err = s.trainingSvc.SubmitPlan(c.Request.Context(), ta, id, req.Archetype); err != nil {
		if errors.Is(err, training.ErrNotOwned) {
			respondError(c, 403, "forbidden", "forbidden")
		} else if errors.Is(err, training.ErrInvalidArchetype) {
			respondErr(c, 400, err)
		} else {
			internalError(c, err)
		}
		return
	}
	c.Status(204)
}
func (s *server) handleLiveTacticChange(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, 400, "invalid_match_id", "invalid match id")
		return
	}
	var req struct {
		Minute int    `json:"minute"`
		Style  string `json:"style"`
	}
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, 400, "invalid_tactical_change", "invalid tactical change")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	err = s.matchSvc.TacticChange(c.Request.Context(), id, ident.ManagerID, req.Minute, map[string]any{"style": req.Style})
	if err != nil {
		if errors.Is(err, internalmatch.ErrMinuteClosed) || errors.Is(err, internalmatch.ErrMatchNotLive) {
			respondErr(c, 409, err)
		} else {
			respondErr(c, 400, err)
		}
		return
	}
	c.Status(204)
}
