package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalboard "github.com/touchline/backend/internal/board"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

// boardStatus maps board sentinel errors to HTTP statuses. Unknown mandates and
// cross-world access collapse to 404; protocol misuse is 400/403/409.
func boardStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalboard.ErrNotEmployed):
		respondError(c, http.StatusConflict, "no_active_club", "no active club")
	case errors.Is(err, internalboard.ErrMandateNotFound):
		respondError(c, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, internalboard.ErrNotMandateManager):
		respondError(c, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, internalboard.ErrMandateResolved),
		errors.Is(err, internalboard.ErrNegotiationRejected),
		errors.Is(err, internalboard.ErrWorldMismatch):
		respondErr(c, http.StatusConflict, err)
	case errors.Is(err, internalboard.ErrMandateTypeNotNegotiable),
		errors.Is(err, internalboard.ErrMandateValueInvalid):
		respondErr(c, http.StatusBadRequest, err)
	default:
		internalError(c, err)
	}
}

// handleBoardView returns the caller's club board status: confidence, factor
// snapshot, and the current season's mandates.
func (s *server) handleBoardView(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	v, err := s.boardSvc.BoardView(c.Request.Context(), worldID, ident.ManagerID)
	if err != nil {
		boardStatus(c, err)
		return
	}
	if v == nil {
		c.JSON(http.StatusOK, gin.H{"confidence": 0, "snapshot": nil, "explanation": nil, "mandates": []any{}})
		return
	}
	c.JSON(http.StatusOK, v)
}

// handleNegotiateMandate proposes a bounded new target for one sporting
// mandate.
func (s *server) handleNegotiateMandate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_mandate_id", "invalid mandate id")
		return
	}
	var req internalboard.NegotiateInput
	if c.ShouldBindJSON(&req) != nil {
		respondError(c, http.StatusBadRequest, "invalid_negotiation_payload", "invalid negotiation payload")
		return
	}
	req.MandateID = id

	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	mandate, exp, err := s.boardSvc.NegotiateMandate(c.Request.Context(), worldID, ident.ManagerID, req)
	if err != nil {
		boardStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"mandate": mandate, "explanation": exp})
}
