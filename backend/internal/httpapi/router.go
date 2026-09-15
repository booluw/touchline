package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/touchline/backend/internal/apidocs"
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

	// API reference (OpenAPI spec + interactive Scalar UI). Public like the
	// health endpoints — anyone can read the contract.
	apidocs.Mount(r)

	// The single authenticated WebSocket (S02-04). requireAuth validates the
	// access_token cookie on the handshake; events are world-scoped.
	r.GET("/ws", s.requireAuth, s.handleWS)

	api := r.Group("/api")
	{
		api.POST("/auth/login", s.handleLogin)
		api.POST("/auth/register", s.handleRegister)
		api.POST("/auth/refresh", s.handleRefresh)
		api.GET("/dashboard", s.requireAuth, s.handleDashboard)

		// Club reads (S03-01): the caller's own world only.
		api.GET("/clubs", s.requireAuth, s.handleListClubs)
		api.GET("/clubs/:id", s.requireAuth, s.handleGetClub)
		api.GET("/clubs/:id/lineup", s.requireAuth, s.handleGetLineup)
		api.PUT("/clubs/:id/lineup", s.requireAuth, s.handleSetLineup)
		api.GET("/clubs/:id/tactics", s.requireAuth, s.handleGetTactics)
		api.POST("/clubs/:id/tactics", s.requireAuth, s.handleSetTactics)
		api.GET("/clubs/:id/training-plan", s.requireAuth, s.handleGetTrainingPlan)
		api.POST("/clubs/:id/training-plan", s.requireAuth, s.handleSetTrainingPlan)

		// Finance reads (S05-02): the owning manager's own club only. The
		// handler resolves the club's world through ownership, so these
		// endpoints are inherently world-scoped to the caller's manager.
		api.GET("/clubs/:id/finances", s.requireAuth, s.handleGetFinances)
		api.GET("/clubs/:id/ledger", s.requireAuth, s.handleGetLedger)
		api.GET("/clubs/:id/contracts", s.requireAuth, s.handleGetContracts)

		// Player: the manager's own offer inbox and career actions.
		manager := api.Group("/managers", s.requireAuth)
		{
			manager.GET("/me/offers", s.handleListOffers)
			manager.POST("/me/resign", s.handleResign)
		}
		api.POST("/offers/:id/accept", s.requireAuth, s.handleAcceptOffer)
		api.POST("/offers/:id/decline", s.requireAuth, s.handleDeclineOffer)

		// Admin: world lifecycle (S02-02), whole-world seeding (clubs + players
		// + memberships, launch model), media aid offers, country-scoped league
		// administration (S04-01).
		admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
		{
			admin.POST("/worlds", s.handleCreateWorld)
			admin.POST("/worlds/:id/status", s.handleWorldStatus)
			admin.POST("/worlds/:id/config", s.handleWorldConfig)
			admin.POST("/worlds/:id/seed", s.handleSeedWorld)
			admin.POST("/offers", s.handleCreateOffer)
			admin.POST("/countries", s.handleCreateCountry)
			admin.GET("/countries", s.handleListCountries)
			admin.POST("/leagues", s.handleCreateLeague)
			admin.GET("/leagues", s.handleListLeagues)
			admin.PATCH("/leagues/:id/adjacency", s.handleUpdateAdjacency)
			admin.GET("/club-name-parts", s.handleListClubNameParts)
			admin.POST("/club-name-parts", s.handleAddClubNamePart)
			admin.DELETE("/club-name-parts/:kind/:value", s.handleRemoveClubNamePart)
		}

		// Competition reads (S04-01): always scoped to the caller's world.
		comp := api.Group("/countries", s.requireAuth)
		{
			comp.GET("", s.handleMyCountries)
		}
		api.GET("/competitions", s.requireAuth, s.handleMyCompetitions)
		api.GET("/competitions/:id", s.requireAuth, s.handleGetCompetition)
		api.GET("/competitions/:id/fixtures", s.requireAuth, s.handleGetFixtures)
		api.GET("/competitions/:id/standings", s.requireAuth, s.handleGetStandings)

		// Match feed (S04-03): the fixture header for the match screen and
		// the persisted event feed. Both are world-scoped to the caller.
		api.GET("/fixtures/:id", s.requireAuth, s.handleGetFixture)
		api.GET("/matches/:id/events", s.requireAuth, s.handleGetMatchEvents)
		api.POST("/matches/:id/tactical", s.requireAuth, s.handleLiveTacticChange)
	}

	return r
}
