package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalclub "github.com/touchline/backend/internal/club"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// handleListClubs lists the clubs in the caller's world. The JWT carries no
// world_id (OPD-15); the world is derived from the manager row.
func (s *server) handleListClubs(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	clubs, err := s.clubSvc.ListClubs(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"clubs": clubs})
}

// handleGetClub returns a club with its manager and squad, but only when the
// club is in the caller's own world — clubs never leak across worlds.
func (s *server) handleGetClub(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid club id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}

	detail, err := s.clubSvc.GetClubDetail(c.Request.Context(), id)
	switch {
	case errors.Is(err, internalclub.ErrClubNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	if detail.WorldID != worldID {
		c.JSON(http.StatusNotFound, gin.H{"error": internalclub.ErrClubNotFound.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// callerWorld resolves the caller's world from their manager row. A JWT never
// carries a world_id (OPD-15); authenticated managers always have a row.
func (s *server) callerWorld(c *gin.Context) (uuid.UUID, error) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	var worldID uuid.UUID
	err := s.pool.QueryRow(c.Request.Context(),
		`SELECT world_id FROM manager.managers WHERE id = $1`, ident.ManagerID,
	).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errors.New("no world context")
	}
	return worldID, err
}
