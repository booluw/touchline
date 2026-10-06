package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalsocial "github.com/touchline/backend/internal/social"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

// handleListMessages returns the caller's direct-message inbox, newest first,
// plus the unread count (GET /api/messages). World-scoped via the caller's
// manager row (OPD-15).
func (s *server) handleListMessages(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	messages, unread, err := s.socialSvc.ListInbox(c.Request.Context(), worldID, ident.ManagerID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages, "unread": unread})
}

type sendMessageRequest struct {
	RecipientID string `json:"recipient_id" binding:"required"`
	Body        string `json:"body" binding:"required"`
}

// handleSendMessage delivers one manager→manager message (POST /api/messages).
// The recipient must be a human manager in the caller's world; the body is
// sanitized server-side. Response codes: 201 sent, 400 recipient/body invalid,
// 404 recipient unknown or in another world, 413 body too long, 429 rate
// limited.
func (s *server) handleSendMessage(c *gin.Context) {
	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "recipient_id_and_body_are_required", "recipient_id and body are required")
		return
	}
	recipientID, err := uuid.Parse(req.RecipientID)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_recipient_id", "invalid recipient_id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	msg, err := s.socialSvc.SendMessage(c.Request.Context(), worldID, ident.ManagerID, recipientID, req.Body)
	switch {
	case errors.Is(err, internalsocial.ErrManagerNotFound), errors.Is(err, internalsocial.ErrManagerNotInWorld):
		respondError(c, http.StatusNotFound, "recipient_not_found", "recipient not found")
		return
	case errors.Is(err, internalsocial.ErrMessageTooLong):
		respondError(c, http.StatusRequestEntityTooLarge, "message_exceeds_the_maximum_length", "message exceeds the maximum length")
		return
	case errors.Is(err, internalsocial.ErrRateLimited):
		c.Header("Retry-After", "60")
		respondError(c, http.StatusTooManyRequests, "message_rate_limit_exceeded", "message rate limit exceeded")
		return
	case errors.Is(err, internalsocial.ErrEmptyMessage),
		errors.Is(err, internalsocial.ErrSelfMessage),
		errors.Is(err, internalsocial.ErrBotRecipient):
		respondErr(c, http.StatusBadRequest, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, msg)
}

// handleReadMessage idempotently marks one of the caller's inbound messages as
// read (POST /api/messages/:id/read). Messages that are not the caller's read
// as 404.
func (s *server) handleReadMessage(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid_message_id", "invalid message id")
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		respondError(c, http.StatusForbidden, "no_world_context", "no world context")
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	msg, err := s.socialSvc.MarkRead(c.Request.Context(), worldID, ident.ManagerID, id)
	switch {
	case errors.Is(err, internalsocial.ErrMessageNotFound):
		respondError(c, http.StatusNotFound, "message_not_found", "message not found")
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, msg)
}
