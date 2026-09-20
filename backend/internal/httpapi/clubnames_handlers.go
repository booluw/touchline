package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
)

// handleListClubNameParts returns one country-scoped club-name pool
// (ref.club_name_parts); country_code defaults to the global pool (”).
func (s *server) handleListClubNameParts(c *gin.Context) {
	countryCode := c.Query("country_code")
	stems, suffixes, err := s.bootSvc.ListClubNameParts(c.Request.Context(), countryCode)
	if err != nil {
		switch {
		case errors.Is(err, internalbootstrap.ErrInvalidClubNameCountry):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"country_code": countryCode, "stems": stems, "suffixes": suffixes})
}

// handleAddClubNamePart upserts a single stem or suffix into a club-name pool.
func (s *server) handleAddClubNamePart(c *gin.Context) {
	var req struct {
		Kind        string `json:"kind"`
		Value       string `json:"value"`
		CountryCode string `json:"country_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := s.bootSvc.AddClubNamePart(c.Request.Context(), req.Kind, req.Value, req.CountryCode); err != nil {
		switch {
		case errors.Is(err, internalbootstrap.ErrInvalidClubNamePart),
			errors.Is(err, internalbootstrap.ErrClubNamePartRequired),
			errors.Is(err, internalbootstrap.ErrInvalidClubNameCountry):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"kind": req.Kind, "value": req.Value, "country_code": req.CountryCode})
}

// handleRemoveClubNamePart deletes a single stem or suffix from a club-name
// pool. Deleting an absent entry is a no-op (204).
func (s *server) handleRemoveClubNamePart(c *gin.Context) {
	countryCode := c.Query("country_code")
	if err := s.bootSvc.RemoveClubNamePart(c.Request.Context(), c.Param("kind"), c.Param("value"), countryCode); err != nil {
		switch {
		case errors.Is(err, internalbootstrap.ErrInvalidClubNamePart),
			errors.Is(err, internalbootstrap.ErrClubNamePartRequired),
			errors.Is(err, internalbootstrap.ErrInvalidClubNameCountry):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			internalError(c, err)
		}
		return
	}
	c.JSON(http.StatusNoContent, gin.H{})
}
