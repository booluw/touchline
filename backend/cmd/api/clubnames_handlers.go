package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
)

// handleListClubNameParts returns the global club-name pools (ref.club_name_parts).
func (s *server) handleListClubNameParts(c *gin.Context) {
	stems, suffixes, err := s.bootSvc.ListClubNameParts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stems": stems, "suffixes": suffixes})
}

// handleAddClubNamePart upserts a single stem or suffix into the global pool.
func (s *server) handleAddClubNamePart(c *gin.Context) {
	var req struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := s.bootSvc.AddClubNamePart(c.Request.Context(), req.Kind, req.Value); err != nil {
		switch {
		case errors.Is(err, internalbootstrap.ErrInvalidClubNamePart),
			errors.Is(err, internalbootstrap.ErrClubNamePartRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"kind": req.Kind, "value": req.Value})
}

// handleRemoveClubNamePart deletes a single stem or suffix from the global
// pool. Deleting an absent entry is a no-op (204).
func (s *server) handleRemoveClubNamePart(c *gin.Context) {
	if err := s.bootSvc.RemoveClubNamePart(c.Request.Context(), c.Param("kind"), c.Param("value")); err != nil {
		switch {
		case errors.Is(err, internalbootstrap.ErrInvalidClubNamePart),
			errors.Is(err, internalbootstrap.ErrClubNamePartRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}
	c.JSON(http.StatusNoContent, gin.H{})
}
