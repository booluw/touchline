package httpapi

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Admin: countries, leagues, seeding (S04-01)
// ---------------------------------------------------------------------------

type countryRequest struct {
	WorldID uuid.UUID `json:"world_id" binding:"required"`
	Code    string    `json:"code" binding:"required"`
	Name    string    `json:"name" binding:"required"`
}

func (s *server) handleCreateCountry(c *gin.Context) {
	var req countryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "world_id_code_and_name_are_required", "world_id, code and name are required")
		return
	}
	country, err := s.compSvc.CreateCountry(c.Request.Context(), req.WorldID, req.Code, req.Name)
	if err != nil {
		respondErr(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusCreated, country)
}

func (s *server) handleListCountries(c *gin.Context) {
	worldID, err := uuid.Parse(c.Query("world_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "world_id_query_parameter_is_required", "world_id query parameter is required")
		return
	}
	countries, err := s.compSvc.ListCountries(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"countries": countries})
}

func (s *server) handleCreateLeague(c *gin.Context) {
	var req internalcompetition.LeagueParams
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("%v", err)
		respondError(c, http.StatusBadRequest, "malformed_league_payload", "malformed league payload")
		return
	}
	if req.CountryID == uuid.Nil || req.Name == "" || req.TeamCount < 4 {
		respondError(c, http.StatusBadRequest, "country_id_name_and_team_count_are_required", "country_id, name and team_count are required")
		return
	}
	league, err := s.compSvc.CreateLeague(c.Request.Context(), req)
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrNameCollision),
		errors.Is(err, internalcompetition.ErrInvalidTeamCount),
		errors.Is(err, internalcompetition.ErrInvalidTier),
		errors.Is(err, internalcompetition.ErrInvalidCounts),
		errors.Is(err, internalcompetition.ErrBadAdjacency):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, league)
}

func (s *server) handleListLeagues(c *gin.Context) {
	worldID, err := uuid.Parse(c.Query("world_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "world_id_query_parameter_is_required", "world_id query parameter is required")
		return
	}
	leagues, err := s.compSvc.ListLeagues(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"leagues": leagues})
}

type adjacencyRequest struct {
	PromotesTo  *uuid.UUID `json:"promotes_to"`
	RelegatesTo *uuid.UUID `json:"relegates_to"`
}

func (s *server) handleUpdateAdjacency(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_league_id", "invalid league id")
		return
	}
	var req adjacencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_adjacency_payload", "malformed adjacency payload")
		return
	}
	err = s.compSvc.UpdateLeagueAdjacency(c.Request.Context(), leagueID, req.PromotesTo, req.RelegatesTo)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrBadAdjacency):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *server) handleSeedWorld(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_world_id", "invalid world id")
		return
	}
	// Validate synchronously so a bad world 4xxs immediately instead of through
	// a queued job that would just fail after the heavy work.
	if err := s.compSvc.ValidateSeedWorld(c.Request.Context(), worldID); err != nil {
		switch {
		case errors.Is(err, internalcompetition.ErrWorldNotFound):
			respondErr(c, http.StatusNotFound, err)
		case errors.Is(err, internalcompetition.ErrWorldArchived),
			errors.Is(err, internalcompetition.ErrWorldHasNoLeagues):
			respondErr(c, http.StatusUnprocessableEntity, err)
		default:
			internalError(c, err)
		}
		return
	}
	if s.seedJobs == nil {
		respondError(c, http.StatusInternalServerError, "seed_job_enqueuer_not_wired", "seed job enqueuer not wired")
		return
	}
	jobID, err := s.seedJobs(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	log.Printf("admin: seed queued for world %s (job=%d)", worldID, jobID)
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "world_id": worldID, "job_id": jobID})
}

// handleStartSeason starts a league's first season (IM01). It requires the
// league to already be seeded (clubs present). Later seasons are created
// automatically at rollover, after the configured off-season gap. An optional
// JSON body may pin matchday 1 to a calendar date:
//
//	{"kickoff_date": "2031-08-02"}
//
// Without a body the season kicks off the day after the world's current date;
// a past date or a date off the league's allowed weekdays is rejected with 422.
func (s *server) handleStartSeason(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_world_id", "invalid world id")
		return
	}
	leagueID, err := uuid.Parse(c.Param("leagueID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_league_id", "invalid league id")
		return
	}

	var body struct {
		KickoffDate *string `json:"kickoff_date"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		respondError(c, http.StatusBadRequest, "invalid_kickoff_date_body", "invalid kickoff date body")
		return
	}
	var kickoff *time.Time
	if body.KickoffDate != nil {
		day, err := time.Parse("2006-01-02", *body.KickoffDate)
		if err != nil {
			respondError(c, http.StatusBadRequest, "kickoff_date_must_be_a_calendar_date_yyyy_mm_dd", "kickoff_date must be a calendar date (YYYY-MM-DD)")
			return
		}
		kickoff = &day
	}

	season, err := s.compSvc.StartSeasonKickoff(c.Request.Context(), worldID, leagueID, kickoff)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrLeagueAlreadySeeded),
		errors.Is(err, internalcompetition.ErrCompetitionNotSeeded):
		respondErr(c, http.StatusConflict, err)
		return
	case errors.Is(err, internalcompetition.ErrKickoffDateInPast),
		errors.Is(err, internalcompetition.ErrKickoffNotAllowedWeekday):
		respondErr(c, http.StatusUnprocessableEntity, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, season)
}

// seedJobStatus is the normalized view of the world's most recent seed job.
type seedJobStatus struct {
	ID          int64      `json:"id"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	AttemptedAt *time.Time `json:"attempted_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	LastError   *string    `json:"last_error"`
}

func (s *server) handleSeedWorldStatus(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_world_id", "invalid world id")
		return
	}
	ctx := c.Request.Context()

	var (
		worldExists bool
		worldSeed   bool
	)
	err = s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM world.worlds WHERE id = $1), 
		        COALESCE((SELECT world_seed IS NOT NULL FROM world.worlds WHERE id = $1), FALSE)`,
		worldID,
	).Scan(&worldExists, &worldSeed)
	if err != nil {
		log.Printf("seed-status world %s: check world: %v", worldID, err)
		internalError(c, err)
		return
	}
	if !worldExists {
		respondErr(c, http.StatusNotFound, internalcompetition.ErrWorldNotFound)
		return
	}

	var leaguesTotal, leaguesSeeded, clubs, poolSize int
	err = s.pool.QueryRow(ctx, `
		SELECT 
		  (SELECT COUNT(*) FROM competition.competitions c
		     JOIN world.countries wc ON wc.id = c.country_id WHERE wc.world_id = $1),
		  (SELECT COUNT(*) FROM competition.competitions c
		     JOIN world.countries wc ON wc.id = c.country_id
		     WHERE wc.world_id = $1
		       AND c.team_count = (SELECT COUNT(*) FROM competition.club_competitions cc
		                           WHERE cc.competition_id = c.id AND cc.role = 'league')),
		  (SELECT COUNT(*) FROM club.clubs WHERE world_id = $1),
		  (SELECT COUNT(*) FROM player.players WHERE world_id = $1 AND club_id IS NULL AND status = 'free_agent')`,
		worldID,
	).Scan(&leaguesTotal, &leaguesSeeded, &clubs, &poolSize)
	if err != nil {
		log.Printf("seed-status world %s: read progress: %v", worldID, err)
		internalError(c, err)
		return
	}

	var job *seedJobStatus
	var (
		jobID       int64
		state       string
		attempt     int
		maxAttempts int
		attemptedAt *time.Time
		finishedAt  *time.Time
		lastError   *string
	)
	err = s.pool.QueryRow(ctx, `
		SELECT id, state::text, attempt, max_attempts, attempted_at, finalized_at,
		       errors[CARDINALITY(errors)]->>'error'
		FROM river.river_job
		WHERE kind = 'seed_world' AND args->>'world_id' = $1
		ORDER BY id DESC LIMIT 1`,
		worldID.String(),
	).Scan(&jobID, &state, &attempt, &maxAttempts, &attemptedAt, &finishedAt, &lastError)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("seed-status world %s: read seed job: %v", worldID, err)
		internalError(c, err)
		return
	}
	if err == nil {
		job = &seedJobStatus{
			ID:          jobID,
			State:       state,
			Attempt:     attempt,
			MaxAttempts: maxAttempts,
			AttemptedAt: attemptedAt,
			FinishedAt:  finishedAt,
			LastError:   lastError,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"world_id":     worldID,
		"world_seeded": worldSeed,
		"leagues": gin.H{
			"total":  leaguesTotal,
			"seeded": leaguesSeeded,
		},
		"clubs":     clubs,
		"pool_size": poolSize,
		"job":       job,
	})
}

// ---------------------------------------------------------------------------
// Manager reads: the caller's own world only (S04-01)
// ---------------------------------------------------------------------------

func (s *server) handleMyCountries(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	countries, err := s.compSvc.ListCountries(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"countries": countries})
}

func (s *server) handleMyCompetitions(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	leagues, err := s.compSvc.ListLeagues(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"competitions": leagues})
}

// handleMyClubCompetitions returns the competitions the caller's club belongs
// to, each with a rich manager dossier — the league table for the (single)
// league and the current round + next fixture for each cup (OPD-01/OPD-20).
// An unemployed manager sees an empty list.
func (s *server) handleMyClubCompetitions(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	clubID, err := s.callerClubID(c)
	if err != nil {
		internalError(c, err)
		return
	}
	if clubID == nil {
		c.JSON(http.StatusOK, gin.H{"competitions": []internalcompetition.ClubCompetitionItem{}})
		return
	}
	competitions, err := s.compSvc.MyClubCompetitions(c.Request.Context(), worldID, *clubID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"competitions": competitions})
}

// handleMyStandingsWindow returns the active-season table cut to the three
// positions above and below the caller's club (IM60), same shape as
// handleGetStandings. 404 when the caller's club is not in that table.
func (s *server) handleMyStandingsWindow(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	clubID, err := s.callerClubID(c)
	if err != nil {
		internalError(c, err)
		return
	}
	if clubID == nil {
		respondErr(c, http.StatusNotFound, internalcompetition.ErrClubNotFound)
		return
	}
	standings, err := s.compSvc.GetStandings(c.Request.Context(), leagueID, worldID)
	if errors.Is(err, internalcompetition.ErrNoSeason) {
		respondErr(c, http.StatusNotFound, err)
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	rows, ok := internalcompetition.StandingsWindow(standings.Rows, *clubID, 3)
	if !ok {
		respondErr(c, http.StatusNotFound, internalcompetition.ErrClubNotFound)
		return
	}
	standings.Rows = rows
	c.JSON(http.StatusOK, standings)
}

// handleMyLeagueOutlook returns what is at stake for the caller's club in a
// league and its projected finish (IM56/IM57). 404 when the caller has no
// club, the league has no active season, or the club is not in its table.
func (s *server) handleMyLeagueOutlook(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	clubID, err := s.callerClubID(c)
	if err != nil {
		internalError(c, err)
		return
	}
	if clubID == nil {
		respondErr(c, http.StatusNotFound, internalcompetition.ErrClubNotFound)
		return
	}
	outlook, err := s.compSvc.ClubOutlook(c.Request.Context(), worldID, leagueID, *clubID)
	switch {
	case errors.Is(err, internalcompetition.ErrNoSeason), errors.Is(err, internalcompetition.ErrClubNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, outlook)
}

func (s *server) handleGetCompetition(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	league, err := s.compSvc.GetLeague(c.Request.Context(), worldID, id)
	if err != nil {
		respondErr(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, league)
}

// handleAdminGetCompetitionDetail returns the full admin dossier for one
// competition — league or cup, discriminated by competition_type (IM15). This
// is a global admin read; unlike the manager-facing gets it is not world-scoped.
func (s *server) handleAdminGetCompetitionDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	detail, err := s.compSvc.CompetitionDetail(c.Request.Context(), id)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondError(c, http.StatusNotFound, "competition_not_found", "competition not found")
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (s *server) handleGetFixtures(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}

	var matchday *int
	if raw := c.Query("matchday"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			matchday = &v
		} else {
			respondError(c, http.StatusBadRequest, "invalid_matchday", "invalid matchday")
			return
		}
	}

	fixtures, err := s.compSvc.GetFixtures(c.Request.Context(), leagueID, worldID, matchday)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"fixtures": fixtures})
}

// handleGetSeasonCalendar returns the competition's fixture calendar grouped
// by game-week (IM03), world-scoped to the caller. With an optional ?season=N
// query it serves that numbered season; otherwise the active one.
func (s *server) handleGetSeasonCalendar(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	var season *int
	if raw := c.Query("season"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			respondError(c, http.StatusBadRequest, "invalid_season", "invalid season")
			return
		}
		season = &n
	}
	calendar, err := s.compSvc.GetSeasonCalendar(c.Request.Context(), worldID, leagueID, season)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound),
		errors.Is(err, internalcompetition.ErrNoSeason),
		errors.Is(err, internalcompetition.ErrSeasonNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, calendar)
}

func (s *server) handleGetStandings(c *gin.Context) {
	leagueID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_competition_id", "invalid competition id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	standings, err := s.compSvc.GetStandings(c.Request.Context(), leagueID, worldID)
	if errors.Is(err, internalcompetition.ErrNoSeason) {
		respondErr(c, http.StatusNotFound, err)
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, standings)
}
