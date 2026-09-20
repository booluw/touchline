package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/touchline/backend/internal/finance"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// financeStatus maps finance sentinel errors to HTTP statuses. Nonexistent
// clubs and inactive worlds collapse to 404 so existence is never leaked;
// anything else is a server error.
func financeStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, finance.ErrClubNotFound), errors.Is(err, finance.ErrWorldNotActive):
		c.JSON(http.StatusNotFound, gin.H{"error": "club not found"})
	case errors.Is(err, finance.ErrNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	default:
		internalError(c, err)
	}
}

// ownedClubParams resolves the :id path param and the caller's manager id,
// emitting the appropriate HTTP error when either is absent.
func (s *server) ownedClubParams(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	id, ok := clubParam(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	ident, ok := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return uuid.Nil, uuid.Nil, false
	}
	return id, ident.ManagerID, true
}

func (s *server) handleGetFinances(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.financeSvc.RequireOwnership(ctx, managerID, clubID); err != nil {
		financeStatus(c, err)
		return
	}
	sum, err := s.financeSvc.GetSummary(ctx, clubID)
	if err != nil {
		financeStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, sum)
}

func (s *server) handleGetLedger(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.financeSvc.RequireOwnership(ctx, managerID, clubID); err != nil {
		financeStatus(c, err)
		return
	}
	entries, err := s.financeSvc.GetLedger(ctx, clubID, 200)
	if err != nil {
		financeStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, entries)
}

func (s *server) handleGetContracts(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := s.financeSvc.RequireOwnership(ctx, managerID, clubID); err != nil {
		financeStatus(c, err)
		return
	}
	contracts, err := s.financeSvc.GetContracts(ctx, clubID)
	if err != nil {
		financeStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, contracts)
}
