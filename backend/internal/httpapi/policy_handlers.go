package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pkgjwt "github.com/touchline/backend/pkg/auth"

	"github.com/touchline/backend/internal/policybot"
)

// policyActor extracts the authenticated manager as a policybot.Actor.
func (s *server) policyActor(c *gin.Context) (policybot.Actor, uuid.UUID, error) {
	ident, ok := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	if !ok {
		return policybot.Actor{}, uuid.Nil, errors.New("unauthenticated")
	}
	return policybot.Actor{ManagerID: ident.ManagerID}, ident.WorldID, nil
}

// GET /api/managers/me/policies/:type
func (s *server) handleGetPolicy(c *gin.Context) {
	actor, _, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	policyType := c.Param("type")
	if !policybot.ValidPolicyTypes[policyType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy type"})
		return
	}
	p, err := s.policySvc.GetPolicy(c.Request.Context(), actor.ManagerID, policyType)
	if errors.Is(err, policybot.ErrNoPolicy) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no policy saved"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// PUT /api/managers/me/policies/:type
func (s *server) handleUpsertPolicy(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	policyType := c.Param("type")
	if !policybot.ValidPolicyTypes[policyType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy type"})
		return
	}
	var req struct {
		Params json.RawMessage `json:"params"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Params) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy params"})
		return
	}
	p, err := s.policySvc.UpsertPolicy(c.Request.Context(), actor, worldID, actor.ManagerID, policyType, req.Params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// DELETE /api/managers/me/policies/:type
func (s *server) handleDeletePolicy(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	policyType := c.Param("type")
	if !policybot.ValidPolicyTypes[policyType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy type"})
		return
	}
	found, err := s.policySvc.DeletePolicy(c.Request.Context(), actor, worldID, actor.ManagerID, policyType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no policy saved"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /api/managers/me/absence
func (s *server) handleGetAbsence(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	view, err := s.policySvc.GetAbsence(c.Request.Context(), worldID, actor.ManagerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, view)
}

// PUT /api/managers/me/absence  { "away": true }
func (s *server) handleSetAway(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	var req struct {
		Away bool `json:"away"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid away payload"})
		return
	}
	if err := s.policySvc.SetAway(c.Request.Context(), worldID, actor.ManagerID, req.Away); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// DELETE /api/managers/me/absence
func (s *server) handleClearAway(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	if err := s.policySvc.SetAway(c.Request.Context(), worldID, actor.ManagerID, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GET /api/managers/me/absence-summary
func (s *server) handleGetAbsenceSummary(c *gin.Context) {
	actor, worldID, err := s.policyActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	summary, err := s.policySvc.GetAbsenceSummary(c.Request.Context(), worldID, actor.ManagerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, summary)
}
