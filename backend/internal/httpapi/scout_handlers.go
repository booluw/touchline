package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// handleNextClubFixture returns the club's earliest upcoming fixture (across
// all its competitions, by real kickoff time) plus a scout report on the
// opponent. Scoped exactly like the other club reads: any club in the caller's
// world is readable, and a manager with no world context is rejected. An
// off-season club (no upcoming fixture) yields a 200 with a null payload rather
// than an error, mirroring the empty managers/me/competitions list.
func (s *server) handleNextClubFixture(c *gin.Context) {
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
	view, err := s.scoutSvc.NextFixture(c.Request.Context(), worldID, id)
	switch {
	case errors.Is(err, internalcompetition.ErrClubNotFound),
		errors.Is(err, internalcompetition.ErrClubWorldMismatch):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"next_fixture": view})
}
