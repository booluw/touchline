package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// handleDashboard returns the manager's home feed (S07-01): the urgent /
// important / interesting aggregation over their active club(s). World is
// resolved from the session identity at request time (OPD-15); a manager with
// no active club gets empty sections rather than an error.
func (s *server) handleDashboard(c *gin.Context) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	if s.dashSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dashboard unavailable"})
		return
	}
	snap, err := s.dashSvc.GetDashboard(c.Request.Context(), worldID, ident.ManagerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, snap)
}
