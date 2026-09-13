package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalmatch "github.com/touchline/backend/internal/match"
)

// handleGetFixture serves the match-screen header for one fixture (S04-03):
// fixture + club names plus the match view (status, live clock, server-computed
// scoreline). The caller's world scopes the lookup; a fixture outside that
// world is indistinguishable from a missing one.
func (s *server) handleGetFixture(c *gin.Context) {
	fixtureID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fixture id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}

	view, err := s.matchSvc.GetFixtureMatch(c.Request.Context(), fixtureID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if view.Fixture.WorldID != worldID {
		c.JSON(http.StatusNotFound, gin.H{"error": "fixture not found"})
		return
	}
	c.JSON(http.StatusOK, view)
}

// handleGetMatchEvents serves the full ordered match event feed (S04-03), the
// same persisted match_events list the live socket streams. The caller's world
// must own the match.
func (s *server) handleGetMatchEvents(c *gin.Context) {
	matchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid match id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}

	ctx := c.Request.Context()
	m, err := s.matchSvc.GetMatch(ctx, matchID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if m.WorldID != worldID {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if m.Status == internalmatch.MatchStatusPending {
		c.JSON(http.StatusOK, gin.H{"events": []any{}})
		return
	}

	events, err := s.matchSvc.GetMatchEvents(ctx, matchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}
