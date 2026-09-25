package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/touchline/backend/internal/competition"
)

// cupResolutionStatus maps the competition resolution sentinels to HTTP
// statuses, mirroring financeStatus: nonexistent clubs collapse to 404,
// ownership failures to 403, ineligible choices to 422.
func cupResolutionStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, competition.ErrClubNotFound), errors.Is(err, competition.ErrCompetitionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, competition.ErrClubNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, competition.ErrChoiceNotEligible), errors.Is(err, competition.ErrCompetitionWorldMismatch):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	default:
		internalError(c, err)
	}
}

// handleClubCupQualifications serves the club's continental outlook: every
// continental cup, projected entry, conflicts, recorded choice, and next-best
// replacements. Read-only, world-scoped to the caller's own club.
func (s *server) handleClubCupQualifications(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	worldID, err := s.compSvc.RequireOwnership(ctx, managerID, clubID)
	if err != nil {
		cupResolutionStatus(c, err)
		return
	}
	views, err := s.compSvc.ClubCupQualifications(ctx, worldID, clubID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cups": views})
}

type cupChoiceRequest struct {
	CupID uuid.UUID `json:"cup_id"`
}

// handleRecordCupChoice records the caller's opt-in for a cup (upsert). A cup
// the club is not projected for is rejected 422; the choice is consumed at the
// cup's next campaign start.
func (s *server) handleRecordCupChoice(c *gin.Context) {
	clubID, managerID, ok := s.ownedClubParams(c)
	if !ok {
		return
	}
	var req cupChoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.CupID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cup_id is required"})
		return
	}
	ctx := c.Request.Context()
	if err := s.compSvc.RecordCupChoice(ctx, managerID, clubID, req.CupID); err != nil {
		cupResolutionStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "recorded", "cup_id": req.CupID})
}
