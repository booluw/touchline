package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalmatch "github.com/touchline/backend/internal/match"
)

// handleGetFixture serves the match-screen header for one fixture (S04-03):
// fixture + club names plus the match view (status, live clock, server-computed
// scoreline). Any signed-in user may read any fixture (see mayViewMatch).
func (s *server) handleGetFixture(c *gin.Context) {
	fixtureID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid fixture id"})
		return
	}

	view, err := s.matchSvc.GetFixtureMatch(c.Request.Context(), fixtureID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "fixture not found"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if !s.mayViewMatch(c, view.Fixture.WorldID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "fixture not found"})
		return
	}
	c.JSON(http.StatusOK, view)
}

// handleGetMatchEvents serves the full ordered match event feed (S04-03), the
// same persisted match_events list the live socket streams, in feed order
// (minute, position in the minute, sequence). With the positional engine on
// (IM34) it also holds that engine's extra events (source "pitchsim").
func (s *server) handleGetMatchEvents(c *gin.Context) {
	matchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid match id"})
		return
	}

	ctx := c.Request.Context()
	m, err := s.matchSvc.GetMatch(ctx, matchID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if !s.mayViewMatch(c, m.WorldID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if m.Status == internalmatch.MatchStatusPending {
		c.JSON(http.StatusOK, gin.H{"events": []any{}})
		return
	}

	events, err := s.matchSvc.GetMatchEvents(ctx, matchID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// mayViewMatch is the one place that decides who may watch a match (fixture
// header, event feed, simulation track). IM34 decision: any signed-in user may
// watch any match, in any world. The planned shareable link becomes a second
// way to pass this check (a per-match token on a public route).
func (s *server) mayViewMatch(_ *gin.Context, _ uuid.UUID) bool { return true }

// handleGetMatchTrack serves the 2D simulation track for a match (IM34): the
// ball and player keyframes for minutes ?from..?to (both optional), regenerated
// from the match seed, kickoff snapshot and event feed. 404 when the match was
// played without the positional engine.
func (s *server) handleGetMatchTrack(c *gin.Context) {
	matchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid match id"})
		return
	}
	from, to := 1, 0
	if v := c.Query("from"); v != "" {
		if from, err = strconv.Atoi(v); err != nil || from < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from must be a minute of 1 or more"})
			return
		}
	}
	if v := c.Query("to"); v != "" {
		if to, err = strconv.Atoi(v); err != nil || to < from {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be a minute not before from"})
			return
		}
	}

	ctx := c.Request.Context()
	m, err := s.matchSvc.GetMatch(ctx, matchID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !s.mayViewMatch(c, m.WorldID)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	view, err := s.matchSvc.GetTrack(ctx, matchID, from, to)
	switch {
	case errors.Is(err, internalmatch.ErrNoTrack):
		c.JSON(http.StatusNotFound, gin.H{"error": "this match has no simulation"})
	case err != nil:
		internalError(c, err)
	default:
		c.JSON(http.StatusOK, view)
	}
}
