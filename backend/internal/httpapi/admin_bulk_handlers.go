package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/playergen"
)

// bulkCreatePlayersRequest is the admin body for bulk player creation.
type bulkCreatePlayersRequest struct {
	Count       int      `json:"count"`
	AgeMin      int      `json:"age_min"`
	AgeMax      int      `json:"age_max"`
	Quality     string   `json:"quality"`
	Positions   []string `json:"positions"`
	Nationality string   `json:"nationality"`
	Origin      string   `json:"origin"`
}

// handleAdminBulkCreatePlayers injects `count` generated free agents into a
// country pool with admin-chosen metrics (A10). Admin-only via requireAdmin.
func (s *server) handleAdminBulkCreatePlayers(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid world id"})
		return
	}
	countryID, err := uuid.Parse(c.Param("countryID"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid country id"})
		return
	}

	var req bulkCreatePlayersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed bulk payload"})
		return
	}
	opts := playerpool.BulkOpts{
		Count:       req.Count,
		MinAge:      req.AgeMin,
		MaxAge:      req.AgeMax,
		Quality:     req.Quality,
		Positions:   req.Positions,
		Nationality: req.Nationality,
		Origin:      req.Origin,
	}
	if err := opts.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := s.countryInWorld(ctx, worldID, countryID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "country not found in world"})
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	defer tx.Rollback(ctx)

	generator, natPool, err := internalbootstrap.LoadPools(ctx, tx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// A per-request seed keeps the batch deterministic only in the sense of
	// format; content varies per call, which is the point of an admin tool.
	factory := playergen.NewPlayerFactory(generator, natPool,
		rand.New(rand.NewSource(time.Now().UTC().UnixNano()))).WithRegistry(playergen.NewNameRegistry())

	ids, err := playerpool.BulkCreate(ctx, tx, s.bus, worldID, countryID, opts, factory, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"created": len(ids), "player_ids": ids})
}

// countryInWorld verifies the country belongs to the world.
func (s *server) countryInWorld(ctx context.Context, worldID, countryID uuid.UUID) error {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM world.countries WHERE id = $1 AND world_id = $2)`,
		countryID, worldID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("country not in world")
	}
	return nil
}
