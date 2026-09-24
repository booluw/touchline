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
		api.GET("/news", s.requireAuth, s.handleNews)

		// Club reads (S03-01): the caller's own world only.
		api.GET("/clubs", s.requireAuth, s.handleListClubs)
		api.GET("/clubs/:id", s.requireAuth, s.handleGetClub)
		api.GET("/clubs/:id/fixtures", s.requireAuth, s.handleListClubFixtures)
		api.GET("/clubs/:id/next-fixture", s.requireAuth, s.handleNextClubFixture)
		api.GET("/clubs/:id/lineup", s.requireAuth, s.handleGetLineup)
		api.PUT("/clubs/:id/lineup", s.requireAuth, s.handleSetLineup)
		api.GET("/clubs/:id/tactics", s.requireAuth, s.handleGetTactics)
		api.POST("/clubs/:id/tactics", s.requireAuth, s.handleSetTactics)
		api.GET("/clubs/:id/training-plan", s.requireAuth, s.handleGetTrainingPlan)
		api.POST("/clubs/:id/training-plan", s.requireAuth, s.handleSetTrainingPlan)

		// Academy investment (S08-01): the owning manager's own club only.
		api.GET("/clubs/:id/academy", s.requireAuth, s.handleGetAcademy)
		api.PUT("/clubs/:id/academy", s.requireAuth, s.handleUpdateAcademy)

		// Medical facility (S08-03): the owning manager's own club only.
		api.GET("/clubs/:id/medical-facility", s.requireAuth, s.handleGetMedicalFacility)
		api.PUT("/clubs/:id/medical-facility", s.requireAuth, s.handleUpgradeMedical)

		// Dressing-room dynamics (S09-02): the owning manager's own club only.
		api.GET("/clubs/:id/dynamics", s.requireAuth, s.handleGetSquadDynamics)

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
			manager.GET("/:id/profile", s.handleGetManagerProfile)
			manager.GET("/me/board", s.handleBoardView)
			manager.POST("/me/board/mandates/:id/negotiate", s.handleNegotiateMandate)
			manager.GET("/me/policies/:type", s.handleGetPolicy)
			manager.PUT("/me/policies/:type", s.handleUpsertPolicy)
			manager.DELETE("/me/policies/:type", s.handleDeletePolicy)
			manager.GET("/me/absence", s.handleGetAbsence)
			manager.PUT("/me/absence", s.handleSetAway)
			manager.DELETE("/me/absence", s.handleClearAway)
			manager.GET("/me/absence-summary", s.handleGetAbsenceSummary)
			manager.GET("/me/competitions", s.handleMyClubCompetitions)
		}

		// Social messaging (S06-04b): the manager's inbox and sending.
		// World-scoped to the caller's manager; recipients must be human
		// managers in the same world.
		api.GET("/messages", s.requireAuth, s.handleListMessages)
		api.POST("/messages", s.requireAuth, s.handleSendMessage)
		api.POST("/messages/:id/read", s.requireAuth, s.handleReadMessage)

		// Relationship graph (S06-04c): the caller's rivalry edges + those of
		// their active club, and the realtime relationship_change feed.
		api.GET("/relationships", s.requireAuth, s.handleListRelationships)

		api.POST("/offers/:id/accept", s.requireAuth, s.handleAcceptOffer)
		api.POST("/offers/:id/decline", s.requireAuth, s.handleDeclineOffer)

		// Transfer market (S06-01): listings, bids, negotiation. World-scoped
		// to the caller's manager; handlers resolve the manager's club and
		// world from the session identity (OPD-15).
		api.POST("/transfers/listings", s.requireAuth, s.handleCreateListing)
		api.GET("/transfers/listings", s.requireAuth, s.handleListListings)
		api.GET("/transfers/listings/:id", s.requireAuth, s.handleGetListing)
		api.POST("/transfers/listings/:id/withdraw", s.requireAuth, s.handleWithdrawListing)
		api.POST("/transfers/bids", s.requireAuth, s.handlePlaceBid)
		api.GET("/transfers/bids", s.requireAuth, s.handleListBids)
		api.POST("/transfers/bids/:id/respond", s.requireAuth, s.handleRespondBid)

		// Player morale + playing time (S06-03): per-club squad overview and
		// individual detail with the 'why'; manager-facing promise and
		// transfer-request actions. World-scoped to the caller's manager.
		api.GET("/clubs/:id/players", s.requireAuth, s.handleListClubPlayers)
		api.GET("/clubs/:id/players/:playerID", s.requireAuth, s.handleGetPlayerMorale)
		api.GET("/clubs/:id/players/:playerID/development", s.requireAuth, s.handleGetPlayerDevelopment)
		api.GET("/clubs/:id/players/:playerID/injury", s.requireAuth, s.handleGetPlayerInjury)
		api.POST("/clubs/:id/players/:playerID/rush-return", s.requireAuth, s.handleRushReturn)
		api.POST("/clubs/:id/players/:playerID/promise-playing-time", s.requireAuth, s.handlePromisePlayingTime)
		api.POST("/clubs/:id/players/:playerID/transfer-request/approve", s.requireAuth, s.handleApproveTransferRequest)
		api.POST("/clubs/:id/players/:playerID/transfer-request/deny", s.requireAuth, s.handleDenyTransferRequest)

		// Admin: world lifecycle (S02-02), whole-world seeding (clubs + players
		// + memberships, launch model), media aid offers, country-scoped league
		// administration (S04-01).
		admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
		{
			admin.GET("/worlds", s.handleListWorlds)
			admin.POST("/worlds", s.handleCreateWorld)
			admin.POST("/worlds/:id/status", s.handleWorldStatus)
			admin.POST("/worlds/:id/config", s.handleWorldConfig)
			admin.POST("/worlds/:id/seed", s.handleSeedWorld)
			admin.GET("/worlds/:id/seed-status", s.handleSeedWorldStatus)
			admin.POST("/worlds/:id/leagues/:leagueID/season", s.handleStartSeason)
			admin.POST("/offers", s.handleCreateOffer)
			admin.POST("/countries", s.handleCreateCountry)
			admin.GET("/countries", s.handleListCountries)
			admin.POST("/leagues", s.handleCreateLeague)
			admin.GET("/leagues", s.handleListLeagues)
			admin.POST("/cups", s.handleCreateCup)
			admin.POST("/worlds/:id/countries/:countryID/cups/:cupID/campaign", s.handleStartCupCampaign)
			admin.PATCH("/leagues/:id/adjacency", s.handleUpdateAdjacency)
			admin.POST("/regions", s.handleCreateRegion)
			admin.GET("/regions", s.handleListRegions)
			admin.PATCH("/regions/:id", s.handleRenameRegion)
			admin.DELETE("/regions/:id", s.handleDeleteRegion)
			admin.PATCH("/countries/:id/region", s.handleSetCountryRegion)
			admin.PATCH("/leagues/:id/reputation", s.handleSetLeagueReputation)
			admin.GET("/club-name-parts", s.handleListClubNameParts)
			admin.POST("/club-name-parts", s.handleAddClubNamePart)
			admin.DELETE("/club-name-parts/:kind/:value", s.handleRemoveClubNamePart)
			admin.POST("/worlds/:id/countries/:countryID/players/bulk", s.handleAdminBulkCreatePlayers)
			admin.GET("/worlds/:id/countries/:countryID/overview", s.handleAdminCountryOverview)
			admin.GET("/worlds/:id/countries/:countryID/pyramid", s.handleAdminCountryPyramid)
			admin.GET("/worlds/:id/countries/:countryID/clubs", s.handleAdminCountryClubs)
			admin.GET("/worlds/:id/countries/:countryID/players", s.handleAdminCountryPlayers)
			admin.GET("/worlds/:id/countries/:countryID/free-agents", s.handleAdminCountryFreeAgents)
			admin.GET("/worlds/:id/countries/:countryID/market", s.handleAdminCountryMarket)
			admin.GET("/worlds/:id/countries/:countryID/finance", s.handleAdminCountryFinance)
			admin.GET("/worlds/:id/countries/:countryID/timeline", s.handleAdminCountryTimeline)
			admin.PATCH("/worlds/:id/countries/:countryID/clubs/:clubID", s.handleAdminRenameClub)
			admin.PATCH("/worlds/:id/countries/:countryID/scheduling", s.handleUpdateCountryScheduling)
			admin.PATCH("/leagues/:id/scheduling", s.handleUpdateLeagueScheduling)
			admin.PATCH("/cups/:id/scheduling", s.handleUpdateCupScheduling)
			admin.GET("/worlds/:id/news", s.handleAdminWorldNews)
		}

		// Competition reads (S04-01): always scoped to the caller's world.
		comp := api.Group("/countries", s.requireAuth)
		{
			comp.GET("", s.handleMyCountries)
		}
		api.GET("/competitions", s.requireAuth, s.handleMyCompetitions)
		api.GET("/competitions/:id", s.requireAuth, s.handleGetCompetition)
		api.GET("/competitions/:id/fixtures", s.requireAuth, s.handleGetFixtures)
		api.GET("/competitions/:id/calendar", s.requireAuth, s.handleGetSeasonCalendar)
		api.GET("/competitions/:id/standings", s.requireAuth, s.handleGetStandings)

		// Cups (IM04): roster grouped by cup; the manager view groups by world.
		api.GET("/cups", s.requireAuth, s.handleListCups)
		api.GET("/cups/:id", s.requireAuth, s.handleGetCup)

		// Match feed (S04-03): the fixture header for the match screen and
		// the persisted event feed. Both are world-scoped to the caller.
		api.GET("/fixtures/:id", s.requireAuth, s.handleGetFixture)
		api.GET("/matches/:id/events", s.requireAuth, s.handleGetMatchEvents)
		api.POST("/matches/:id/tactical", s.requireAuth, s.handleLiveTacticChange)

		// Free agents (A08): pool browsing is world-scoped to the caller's own
		// world/country; signing requires owning the club; release is for the
		// owning club's manager or an admin.
		api.GET("/worlds/:worldID/countries/:countryID/free-agents", s.requireAuth, s.handleListFreeAgents)
		api.POST("/clubs/:id/free-agent-signings", s.requireAuth, s.handleSignFreeAgent)
		api.POST("/players/:playerID/release", s.requireAuth, s.handleReleasePlayer)
	}

	return r
}
