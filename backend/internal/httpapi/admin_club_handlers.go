package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internaladmin "github.com/touchline/backend/internal/admin"
)

// handleAdminRenameClub renames a country's club and publishes the admin's
// news story announcing it (the rename is never a silent change).
func (s *server) handleAdminRenameClub(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	clubID, err := uuid.Parse(c.Param("clubID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid club id"})
		return
	}
	var in internaladmin.RenameInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	res, err := s.adminSvc.RenameClub(c.Request.Context(), worldID, countryID, clubID, in)
	if err != nil {
		switch {
		case errors.Is(err, internaladmin.ErrClubNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "club not found in world"})
		case errors.Is(err, internaladmin.ErrCountryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "country not found in world"})
		case errors.Is(err, internaladmin.ErrClubNameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, internaladmin.ErrClubNameRequired),
			errors.Is(err, internaladmin.ErrRenameNewsRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, res)
}

// handleAdminWorldNews returns a world's news stories, newest first.
func (s *server) handleAdminWorldNews(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	limit := intQuery(c, "limit", 30, 100)
	stories, err := s.adminSvc.News(c.Request.Context(), worldID, nil, limit)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"world_id": worldID, "stories": stories})
}

// handleNews returns the calling manager's world news feed (the world is
// resolved from the session identity; a manager with no club gets an empty
// feed rather than an error). IM05 country scoping: the feed shows world-wide
// stories plus the stories of the manager's own club country.
func (s *server) handleNews(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	if s.adminSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "news unavailable"})
		return
	}
	countryID, err := s.callerCountry(c)
	if err != nil {
		internalError(c, err)
		return
	}
	limit := intQuery(c, "limit", 30, 100)
	stories, err := s.adminSvc.News(c.Request.Context(), worldID, countryID, limit)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"world_id": worldID, "stories": stories})
}
