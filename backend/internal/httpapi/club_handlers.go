package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
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

// handleListClubFixtures returns one club's fixtures ordered by kickoff time,
// world-scoped exactly like handleGetClub: any club in the caller's world is
// readable; clubs never leak across worlds (IM03 season calendar).
func (s *server) handleListClubFixtures(c *gin.Context) {
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
	fixtures, err := s.compSvc.ListClubFixtures(c.Request.Context(), worldID, id, 30)
	switch {
	case errors.Is(err, internalcompetition.ErrClubNotFound),
		errors.Is(err, internalcompetition.ErrClubWorldMismatch):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"fixtures": fixtures})
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

// callerCountry resolves the world.countries id of the manager's club (via
// the club's country *name*, which the club row carries). A manager with no
// club, or a club whose country name doesn't match a world country, yields a
// nil country — the caller then sees world-wide stories only.
func (s *server) callerCountry(c *gin.Context) (*uuid.UUID, error) {
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	var countryID *uuid.UUID
	err := s.pool.QueryRow(c.Request.Context(), `
		SELECT wc.id
		FROM manager.managers m
		JOIN club.clubs cl ON cl.id = m.current_club_id
		JOIN world.countries wc
		  ON wc.world_id = cl.world_id AND lower(wc.name) = lower(cl.country)
		WHERE m.id = $1`, ident.ManagerID).Scan(&countryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return countryID, nil
}
