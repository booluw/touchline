package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalsocial "github.com/touchline/backend/internal/social"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// handleGetManagerProfile returns the manager profile page for any manager in
// the caller's world (GET /api/managers/:id/profile). World scoping (OPD-15)
// is enforced in the service: cross-world and unknown managers read as 404.
func (s *server) handleGetManagerProfile(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manager id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	profile, err := s.socialSvc.GetManagerProfile(c.Request.Context(), worldID, ident.ManagerID, id)
	switch {
	case errors.Is(err, internalsocial.ErrManagerNotFound), errors.Is(err, internalsocial.ErrManagerNotInWorld):
		c.JSON(http.StatusNotFound, gin.H{"error": "manager not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, profile)
}
