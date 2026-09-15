package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	internalsocial "github.com/touchline/backend/internal/social"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// handleListRelationships returns every relationship-graph edge attached to the
// caller and their active club (GET /api/relationships, S06-04c). World scoping
// (OPD-15) is enforced in the service against the caller's manager row: a
// cross-world or unknown manager reads as 404.
func (s *server) handleListRelationships(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	edges, err := s.socialSvc.ListRelationships(c.Request.Context(), worldID, ident.ManagerID)
	switch {
	case errors.Is(err, internalsocial.ErrManagerNotFound), errors.Is(err, internalsocial.ErrManagerNotInWorld):
		c.JSON(http.StatusNotFound, gin.H{"error": "manager not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"world_id": worldID, "edges": edges})
}
