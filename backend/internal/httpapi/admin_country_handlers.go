package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internaladmin "github.com/touchline/backend/internal/admin"
	"github.com/touchline/backend/internal/playerpool"
)

// parseAdminCountryParams parses the admin dashboard's world/country path ids
// and 400s on malformed UUIDs. These routes hang off the admin group.
func (s *server) parseAdminCountryParams(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return uuid.Nil, uuid.Nil, false
	}
	countryID, err := uuid.Parse(c.Param("countryID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid country id"})
		return uuid.Nil, uuid.Nil, false
	}
	return worldID, countryID, true
}

// respondAdminCountry writes a dashboard read, mapping the not-found sentinel
// to 404 and everything else to a flat 500.
func (s *server) respondAdminCountry(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, data)
		return
	}
	if errors.Is(err, internaladmin.ErrCountryNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "country not found in world"})
		return
	}
	internalError(c, err)
}

// intQuery parses an optional int query param with a default and an upper cap.
func intQuery(c *gin.Context, key string, def, max int) int {
	v := def
	if raw := c.Query(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			v = n
		}
	}
	if v < 1 {
		v = def
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}

// handleAdminCountryOverview returns the country's home counters.
func (s *server) handleAdminCountryOverview(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ov, err := s.adminSvc.Overview(ctx, worldID, countryID)
	s.respondAdminCountry(c, ov, err)
}

// handleAdminCountryPyramid returns the country's leagues by tier.
func (s *server) handleAdminCountryPyramid(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	py, err := s.adminSvc.Pyramid(ctx, worldID, countryID)
	s.respondAdminCountry(c, py, err)
}

// handleAdminCountryClubs returns the country's clubs with finance sanity.
func (s *server) handleAdminCountryClubs(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	panels, err := s.adminSvc.Clubs(ctx, worldID, countryID)
	s.respondAdminCountry(c, panels, err)
}

// handleAdminCountryPlayers returns the country's population aggregates.
func (s *server) handleAdminCountryPlayers(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ps, err := s.adminSvc.PlayerSummary(ctx, worldID, countryID)
	s.respondAdminCountry(c, ps, err)
}

// handleAdminCountryFreeAgents lists the country's free-agent pool via the
// existing playerpool read, honoring the position/age/nationality filters.
func (s *server) handleAdminCountryFreeAgents(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	f, ok2 := parseFreeAgentFilter(c)
	if !ok2 {
		return
	}
	filter := playerpool.FreeAgentFilter{
		CountryID:   &countryID,
		Position:    toPtrOrNil(f.Position),
		AgeMin:      f.AgeMin,
		AgeMax:      f.AgeMax,
		Nationality: toPtrOrNil(f.Nationality),
	}
	agents, total, err := playerpool.ListFreeAgents(c.Request.Context(), s.pool, worldID, filter, f.Page, f.Limit)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"country_id": countryID, "items": agents, "total": total})
}

// handleAdminCountryMarket returns the transfer market drill-down. `days`
// (default 90) widens/narrows the completed-transfer ledger window.
func (s *server) handleAdminCountryMarket(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	days := intQuery(c, "days", 90, 365)
	ctx := c.Request.Context()
	m, err := s.adminSvc.Market(ctx, worldID, countryID, days)
	s.respondAdminCountry(c, m, err)
}

// handleAdminCountryFinance returns the country's economy panel.
func (s *server) handleAdminCountryFinance(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	fp, err := s.adminSvc.Finance(ctx, worldID, countryID)
	s.respondAdminCountry(c, fp, err)
}

// handleAdminCountryTimeline returns the country-scoped recent event stream.
// `days` (default 14) and `limit` (default 200, cap 500) bound the window.
func (s *server) handleAdminCountryTimeline(c *gin.Context) {
	worldID, countryID, ok := s.parseAdminCountryParams(c)
	if !ok {
		return
	}
	days := intQuery(c, "days", 14, 365)
	limit := intQuery(c, "limit", 200, 500)
	ctx := c.Request.Context()
	tl, err := s.adminSvc.Timeline(ctx, worldID, countryID, days, limit)
	s.respondAdminCountry(c, tl, err)
}
