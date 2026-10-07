package httpapi

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
		respondError(c, http.StatusBadRequest, "invalid_fixture_id", "invalid fixture id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}

	view, err := s.matchSvc.GetFixtureMatch(c.Request.Context(), fixtureID)
	if errors.Is(err, pgx.ErrNoRows) {
		respondError(c, http.StatusNotFound, "fixture_not_found", "fixture not found")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if view.Fixture.WorldID != worldID {
		respondError(c, http.StatusNotFound, "fixture_not_found", "fixture not found")
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
		respondError(c, http.StatusBadRequest, "invalid_match_id", "invalid match id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}

	ctx := c.Request.Context()
	m, err := s.matchSvc.GetMatch(ctx, matchID)
	if errors.Is(err, pgx.ErrNoRows) {
		respondError(c, http.StatusNotFound, "match_not_found", "match not found")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if m.WorldID != worldID {
		respondError(c, http.StatusNotFound, "match_not_found", "match not found")
		return
	}
	if m.Status == internalmatch.MatchStatusPending {
		c.JSON(http.StatusOK, gin.H{"events": []any{}, "stats": internalmatch.MatchStats{}})
		return
	}

	events, err := s.matchSvc.GetMatchEvents(ctx, matchID)
	if err != nil {
		internalError(c, err)
		return
	}
	stats, err := s.matchSvc.Stats(ctx, matchID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "stats": stats})
}
