package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalcompetition "github.com/touchline/backend/internal/competition"
)

// ---------------------------------------------------------------------------
// Cups (IM04 domestic, IM08 regional)
// ---------------------------------------------------------------------------

// handleCreateCup declares a knockout cup under a world's country (IM04,
// regression) or scoped to a region (IM08): region scope carries a soft tier
// and the per-league qualification bands and becomes a 'continental' cup.
func (s *server) handleCreateCup(c *gin.Context) {
	var req internalcompetition.CupParams
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_cup_payload", "malformed cup payload")
		return
	}
	if req.WorldID == uuid.Nil || req.Name == "" {
		respondError(c, http.StatusBadRequest, "world_id_and_name_are_required", "world_id and name are required")
		return
	}

	var (
		cup *internalcompetition.Cup
		err error
	)
	if req.RegionID != nil {
		if *req.RegionID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "region_id_is_required_for_a_regional_cup", "region_id is required for a regional cup")
			return
		}
		cup, err = s.compSvc.CreateRegionalCup(c.Request.Context(), internalcompetition.RegionalCupParams{
			WorldID:         req.WorldID,
			RegionID:        *req.RegionID,
			Tier:            req.Tier,
			Name:            req.Name,
			PrizePool:       req.PrizePool,
			SchedulingRules: req.SchedulingRules,
			Qualification:   req.Qualification,
		})
	} else {
		if req.CountryID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "country_id_or_region_id_is_required", "country_id or region_id is required")
			return
		}
		cup, err = s.compSvc.CreateCup(c.Request.Context(), req)
	}
	switch {
	case errors.Is(err, internalcompetition.ErrCountryNotFound),
		errors.Is(err, internalcompetition.ErrRegionNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrNameCollision),
		errors.Is(err, internalcompetition.ErrStagingInvalid),
		errors.Is(err, internalcompetition.ErrRegionMismatch),
		errors.Is(err, internalcompetition.ErrQualificationOverlap),
		errors.Is(err, internalcompetition.ErrInvalidTier):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, cup)
}

// handlePreviewCup projects a regional cup draft's field through the IM07
// engine (writes nothing): origins, reigning champion, and warnings.
func (s *server) handlePreviewCup(c *gin.Context) {
	var req internalcompetition.CupPreviewParams
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_preview_payload", "malformed preview payload")
		return
	}
	if req.WorldID == uuid.Nil || req.RegionID == uuid.Nil {
		respondError(c, http.StatusBadRequest, "world_id_and_region_id_are_required", "world_id and region_id are required")
		return
	}
	preview, err := s.compSvc.PreviewCupField(c.Request.Context(), req)
	switch {
	case errors.Is(err, internalcompetition.ErrRegionNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrRegionMismatch),
		errors.Is(err, internalcompetition.ErrQualificationOverlap),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusUnprocessableEntity, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, preview)
}

// handleSetCupQualification replaces a regional cup's qualification bands.
func (s *server) handleSetCupQualification(c *gin.Context) {
	cupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}
	var req struct {
		Qualification []internalcompetition.QualBandInput `json:"qualification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_qualification_payload", "malformed qualification payload")
		return
	}
	cup, err := s.compSvc.SetQualification(c.Request.Context(), cupID, req.Qualification)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrCompetitionTypeMismatch):
		respondErr(c, http.StatusConflict, err)
		return
	case errors.Is(err, internalcompetition.ErrRegionMismatch),
		errors.Is(err, internalcompetition.ErrQualificationOverlap):
		respondErr(c, http.StatusUnprocessableEntity, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, cup)
}

// handleSetCupFinalDate edits a cup's final-date policy (IM10): the
// declaration's fixed date/offset for future seasons and, while a campaign is
// live, re-stamps the rounds that have not materialized yet. Body:
// {"final_date": "YYYY-MM-DD" | null, "final_offset_days": int}. On a
// calculated cup final_date stamps a per-campaign override and null clears it;
// on a fixed cup final_date is required and offsets are rejected.
func (s *server) handleSetCupFinalDate(c *gin.Context) {
	cupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondError(c, http.StatusBadRequest, "malformed_final_date_payload", "malformed final-date payload")
		return
	}
	var req struct {
		FinalDate       *string `json:"final_date"` // null = clear a calculated override
		FinalOffsetDays *int    `json:"final_offset_days"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(c, http.StatusBadRequest, "malformed_final_date_payload", "malformed final-date payload")
		return
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields) //nolint:errcheck // presence probe only
	_, finalDateSet := fields["final_date"]
	_, offsetSet := fields["final_offset_days"]
	if !finalDateSet && !offsetSet {
		respondError(c, http.StatusBadRequest, "final_date_or_final_offset_days_is_required", "final_date or final_offset_days is required")
		return
	}
	cup, err := s.compSvc.SetCupFinalDate(c.Request.Context(), cupID, req.FinalDate, finalDateSet, req.FinalOffsetDays)
	switch {
	case errors.Is(err, internalcompetition.ErrCompetitionNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrCompetitionTypeMismatch):
		respondErr(c, http.StatusConflict, err)
		return
	case errors.Is(err, internalcompetition.ErrCupFinalDateInvalid),
		errors.Is(err, internalcompetition.ErrCupFinalDateLocked):
		respondErr(c, http.StatusUnprocessableEntity, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, cup)
}

// handleStartCupCampaign starts a cup's first campaign from the country's
// league membership (admin).
func (s *server) handleStartCupCampaign(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_world_id", "invalid world id")
		return
	}
	countryID, err := uuid.Parse(c.Param("countryID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_country_id", "invalid country id")
		return
	}
	cupID, err := uuid.Parse(c.Param("cupID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}
	season, err := s.compSvc.StartCupCampaign(c.Request.Context(), worldID, countryID, cupID)
	switch {
	case errors.Is(err, internalcompetition.ErrWorldNotFound),
		errors.Is(err, internalcompetition.ErrCompetitionNotFound),
		errors.Is(err, internalcompetition.ErrCountryNotFound):
		respondErr(c, http.StatusNotFound, err)
		return
	case errors.Is(err, internalcompetition.ErrWorldArchived),
		errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
		errors.Is(err, internalcompetition.ErrCupCampaignExists),
		errors.Is(err, internalcompetition.ErrStagingInvalid),
		errors.Is(err, internalcompetition.ErrCupLimit):
		respondErr(c, http.StatusConflict, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, season)
}

// handleStartRegionalCupCampaign starts a cup campaign from the world-scoped
// route, dispatching to the regional engine for continental cups and to the
// country engine (its country read from the cup) otherwise. The response
// carries the season plus any calendar warnings.
func (s *server) handleStartRegionalCupCampaign(c *gin.Context) {
	worldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_world_id", "invalid world id")
		return
	}
	cupID, err := uuid.Parse(c.Param("cupID"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}

	ctype, countryID, err := s.compSvc.CupScope(c.Request.Context(), cupID)
	if err != nil {
		respondErr(c, http.StatusNotFound, err)
		return
	}

	var season *internalcompetition.Season
	var warnings []string
	switch ctype {
	case "continental":
		res, err := s.compSvc.StartRegionalCupCampaign(c.Request.Context(), worldID, cupID)
		switch {
		case errors.Is(err, internalcompetition.ErrWorldNotFound),
			errors.Is(err, internalcompetition.ErrCompetitionNotFound):
			respondErr(c, http.StatusNotFound, err)
			return
		case errors.Is(err, internalcompetition.ErrWorldArchived),
			errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
			errors.Is(err, internalcompetition.ErrCompetitionTypeMismatch),
			errors.Is(err, internalcompetition.ErrCupCampaignExists),
			errors.Is(err, internalcompetition.ErrCupLimit):
			respondErr(c, http.StatusConflict, err)
			return
		case errors.Is(err, internalcompetition.ErrQualificationUnavailable),
			errors.Is(err, internalcompetition.ErrQualificationField),
			errors.Is(err, internalcompetition.ErrRegionMismatch),
			errors.Is(err, internalcompetition.ErrQualificationOverlap):
			respondErr(c, http.StatusUnprocessableEntity, err)
			return
		case err != nil:
			internalError(c, err)
			return
		}
		season = res.Season
		warnings = res.Warnings
	case "domestic_cup":
		if countryID == nil {
			respondError(c, http.StatusConflict, "non_regional_cup_has_no_country", "non-regional cup has no country")
			return
		}
		season, err = s.compSvc.StartCupCampaign(c.Request.Context(), worldID, *countryID, cupID)
		switch {
		case errors.Is(err, internalcompetition.ErrWorldNotFound),
			errors.Is(err, internalcompetition.ErrCompetitionNotFound),
			errors.Is(err, internalcompetition.ErrCountryNotFound):
			respondErr(c, http.StatusNotFound, err)
			return
		case errors.Is(err, internalcompetition.ErrWorldArchived),
			errors.Is(err, internalcompetition.ErrCompetitionWorldMismatch),
			errors.Is(err, internalcompetition.ErrCupCampaignExists),
			errors.Is(err, internalcompetition.ErrStagingInvalid),
			errors.Is(err, internalcompetition.ErrCupLimit):
			respondErr(c, http.StatusConflict, err)
			return
		case err != nil:
			internalError(c, err)
			return
		}
	default:
		respondError(c, http.StatusConflict, "cannot_start_a_campaign_for_this_competition_type", "cannot start a campaign for this competition type")
		return
	}

	body := gin.H{"season": season}
	if len(warnings) > 0 {
		body["warnings"] = warnings
	}
	c.JSON(http.StatusCreated, body)
}

// handleListCups returns every domestic cup of the caller's world.
func (s *server) handleListCups(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	cups, err := s.compSvc.ListCups(c.Request.Context(), worldID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cups": cups})
}

// handleGetCup returns the manager cup view (bracket, entries, champion).
func (s *server) handleGetCup(c *gin.Context) {
	cupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_cup_id", "invalid cup id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	cup, err := s.compSvc.GetCup(c.Request.Context(), worldID, cupID)
	if err != nil {
		respondErr(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, cup)
}
