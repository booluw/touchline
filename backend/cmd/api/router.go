package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// router wires the REST API. Health endpoints (/health, /health/db) are OPD-14
// contract — compose healthchecks and CI depend on them; they stay untouched.
func (s *server) router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{s.appOrigin},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/health/db", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db-down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "db-ok"})
	})

	api := r.Group("/api")
	{
		api.POST("/auth/login", s.handleLogin)
		api.POST("/auth/refresh", s.handleRefresh)
		api.GET("/dashboard", s.requireAuth, s.handleDashboard)

		// Player: the manager's own offer inbox and career actions.
		manager := api.Group("/managers", s.requireAuth)
		{
			manager.GET("/me/offers", s.handleListOffers)
			manager.POST("/me/resign", s.handleResign)
		}
		api.POST("/offers/:id/accept", s.requireAuth, s.handleAcceptOffer)
		api.POST("/offers/:id/decline", s.requireAuth, s.handleDeclineOffer)

		// Admin: world lifecycle (S02-02) and game-start club->manager job offers.
		admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
		{
			admin.POST("/worlds", s.handleCreateWorld)
			admin.POST("/worlds/:id/status", s.handleWorldStatus)
			admin.POST("/offers", s.handleCreateOffer)
		}
	}

	return r
}
