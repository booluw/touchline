package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/touchline/backend/internal/academy"
)

// academyStatus maps academy sentinel errors to HTTP statuses. Nonexistent
// clubs collapse to 404 so existence is never leaked.
func academyStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, academy.ErrClubNotFound), errors.Is(err, academy.ErrWorldNotActive):
		c.JSON(http.StatusNotFound, gin.H{"error": "club not found"})
	case errors.Is(err, academy.ErrNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, academy.ErrInvalidTier):
		c.JSON(http.StatusBadRequest, gin.H{"error": "investment_tier must be between 1 and 5"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// handleGetAcademy returns the owning manager's academy configuration.
func (s *server) handleGetAcademy(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := s.academySvc.RequireOwnership(ctx, managerID, clubID); err != nil {
		academyStatus(c, err)
		return
	}
	a, err := s.academySvc.GetAcademy(ctx, clubID)
	if err != nil {
		academyStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, a)
}

type academyUpdateRequest struct {
	InvestmentTier *int  `json:"investment_tier"`
	IsActive       *bool `json:"is_active"`
}

// handleUpdateAcademy applies an investment-tier change and/or a shutdown/
// reopen to the owning manager's academy, returning the resulting config.
func (s *server) handleUpdateAcademy(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	var req academyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.InvestmentTier == nil && req.IsActive == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}
	ctx := c.Request.Context()

	var a academy.Academy
	var err error
	if req.InvestmentTier != nil {
		a, err = s.academySvc.SetInvestment(ctx, managerID, clubID, *req.InvestmentTier)
		if err != nil {
			academyStatus(c, err)
			return
		}
	}
	if req.IsActive != nil {
		a, err = s.academySvc.SetActive(ctx, managerID, clubID, *req.IsActive)
		if err != nil {
			academyStatus(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, a)
}
