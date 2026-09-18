package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	internalfaction "github.com/touchline/backend/internal/faction"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

func factionStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalfaction.ErrNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// handleGetSquadDynamics returns the owning manager's computed dressing-room
// hierarchy, factions, cohesion, manager support, mood and current unrest
// (GET /api/clubs/:id/dynamics).
func (s *server) handleGetSquadDynamics(c *gin.Context) {
	clubID, ok := clubParam(c)
	if !ok {
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	dyn, err := s.factionSvc.GetDynamics(c.Request.Context(), worldID, ident.ManagerID, clubID)
	if err != nil {
		factionStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, dyn)
}
