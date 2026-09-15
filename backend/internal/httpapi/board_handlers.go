package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalboard "github.com/touchline/backend/internal/board"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// boardStatus maps board sentinel errors to HTTP statuses. Unknown mandates and
// cross-world access collapse to 404; protocol misuse is 400/403/409.
func boardStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internalboard.ErrNotEmployed):
		c.JSON(http.StatusConflict, gin.H{"error": "no active club"})
	case errors.Is(err, internalboard.ErrMandateNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, internalboard.ErrNotMandateManager):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, internalboard.ErrMandateResolved),
		errors.Is(err, internalboard.ErrNegotiationRejected),
		errors.Is(err, internalboard.ErrWorldMismatch):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, internalboard.ErrMandateTypeNotNegotiable),
		errors.Is(err, internalboard.ErrMandateValueInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// handleBoardView returns the caller's club board status: confidence, factor
// snapshot, and the current season's mandates.
func (s *server) handleBoardView(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mandate id"})
		return
	}
	var req internalboard.NegotiateInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid negotiation payload"})
		return
	}
	req.MandateID = id

	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
