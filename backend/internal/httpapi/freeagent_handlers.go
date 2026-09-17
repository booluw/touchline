package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalfinance "github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/playerpool"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// freeAgentFilterRequest is the query-string form of playerpool.FreeAgentFilter.
type freeAgentFilterRequest struct {
	Position    string
	AgeMin      *int
	AgeMax      *int
	Nationality string
	Page        int
	Limit       int
}

// handleListFreeAgents returns the free-agent pool for a world/country with
// optional position, age, and nationality filters.
func (s *server) handleListFreeAgents(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("worldID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	countryID, err := uuid.Parse(c.Param("countryID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid country id"})
		return
	}

	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	ownWorld, err := s.managerWorld(c.Request.Context(), ident.ManagerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if ownWorld != worldID {
		c.JSON(http.StatusForbidden, gin.H{"error": "outside your world"})
		return
	}

	f, ok := parseFreeAgentFilter(c)
	if !ok {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": agents, "total": total})
}

type signFreeAgentRequest struct {
	PlayerID   uuid.UUID `json:"player_id"`
	WeeklyWage int64     `json:"weekly_wage"`
	Years      int       `json:"years"`
}

// handleSignFreeAgent signs a free agent for the owning club. Club ownership
// is validated through finance; the signing itself runs inside a single
// transaction that assigns the player, inserts the contract + wage commitment,
// and logs PLAYER_SIGNED.
func (s *server) handleSignFreeAgent(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	var req signFreeAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlayerID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "player_id is required"})
		return
	}
	if req.WeeklyWage <= 0 || req.Years <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "weekly_wage and years must be positive"})
		return
	}

	ctx := c.Request.Context()
	worldID, err := s.financeSvc.RequireOwnership(ctx, managerID, clubID)
	if err != nil {
		financeOwnershipStatus(c, err)
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	defer tx.Rollback(ctx)

	if err := playerpool.SignFreeAgent(ctx, tx, s.bus, worldID, req.PlayerID, clubID,
		req.WeeklyWage, req.Years, time.Now().UTC()); err != nil {
		poolStatus(c, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

type releasePlayerRequest struct {
	Reason string `json:"reason"`
}

// handleReleasePlayer terminates a player's contract and returns them to the
// free-agent pool. An admin may release any player; a manager may only release
// a player at their own current club.
func (s *server) handleReleasePlayer(c *gin.Context) {
	playerID, err := uuid.Parse(c.Param("playerID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player id"})
		return
	}
	var req releasePlayerRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required"})
		return
	}

	ctx := c.Request.Context()
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	if !s.isAdmin(ctx, ident.UserID) {
		clubID, err := s.playerClub(ctx, playerID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if clubID == uuid.Nil {
			c.JSON(http.StatusConflict, gin.H{"error": "player is a free agent"})
			return
		}
		if _, err := s.financeSvc.RequireOwnership(ctx, ident.ManagerID, clubID); err != nil {
			financeOwnershipStatus(c, err)
			return
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	defer tx.Rollback(ctx)

	if err := playerpool.ReleasePlayer(ctx, tx, s.bus, playerID, req.Reason); err != nil {
		poolStatus(c, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// poolStatus maps free-agent sign/release sentinel errors to HTTP statuses.
func poolStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, playerpool.ErrNotFreeAgent), errors.Is(err, playerpool.ErrClubCannotAfford):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, playerpool.ErrNoActiveContract):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, playerpool.ErrStreetUnder18):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// financeOwnershipStatus maps the finance ownership gate to HTTP statuses.
func financeOwnershipStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalfinance.ErrClubNotFound), errors.Is(err, internalfinance.ErrWorldNotActive):
		c.JSON(http.StatusNotFound, gin.H{"error": "club not found"})
	case errors.Is(err, internalfinance.ErrNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// isAdmin reports whether the user is an admin (auth.users.is_admin).
func (s *server) isAdmin(ctx context.Context, userID uuid.UUID) bool {
	var isAdmin bool
	_ = s.pool.QueryRow(ctx,
		`SELECT is_admin FROM auth.users WHERE id = $1`, userID).Scan(&isAdmin)
	return isAdmin
}

// playerClub resolves a player's current club. uuid.Nil means free agent.
func (s *server) playerClub(ctx context.Context, playerID uuid.UUID) (uuid.UUID, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(club_id, '00000000-0000-0000-0000-000000000000')
		FROM player.players WHERE id = $1`, playerID).Scan(&clubID)
	return clubID, err
}

func parseFreeAgentFilter(c *gin.Context) (freeAgentFilterRequest, bool) {
	f := freeAgentFilterRequest{
		Position:    c.Query("position"),
		Nationality: c.Query("nationality"),
	}
	if raw := c.Query("age_min"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid age_min"})
			return f, false
		}
		f.AgeMin = &v
	}
	if raw := c.Query("age_max"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid age_max"})
			return f, false
		}
		f.AgeMax = &v
	}
	f.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	f.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "25"))
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 {
		f.Limit = 25
	}
	return f, true
}

func toPtrOrNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
