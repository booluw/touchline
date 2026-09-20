package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalsocial "github.com/touchline/backend/internal/social"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// handleListMessages returns the caller's direct-message inbox, newest first,
// plus the unread count (GET /api/messages). World-scoped via the caller's
// manager row (OPD-15).
func (s *server) handleListMessages(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "recipient_id and body are required"})
		return
	}
	recipientID, err := uuid.Parse(req.RecipientID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient_id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	msg, err := s.socialSvc.SendMessage(c.Request.Context(), worldID, ident.ManagerID, recipientID, req.Body)
	switch {
	case errors.Is(err, internalsocial.ErrManagerNotFound), errors.Is(err, internalsocial.ErrManagerNotInWorld):
		c.JSON(http.StatusNotFound, gin.H{"error": "recipient not found"})
		return
	case errors.Is(err, internalsocial.ErrMessageTooLong):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "message exceeds the maximum length"})
		return
	case errors.Is(err, internalsocial.ErrRateLimited):
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "message rate limit exceeded"})
		return
	case errors.Is(err, internalsocial.ErrEmptyMessage),
		errors.Is(err, internalsocial.ErrSelfMessage),
		errors.Is(err, internalsocial.ErrBotRecipient):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid message id"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	msg, err := s.socialSvc.MarkRead(c.Request.Context(), worldID, ident.ManagerID, id)
	switch {
	case errors.Is(err, internalsocial.ErrMessageNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, msg)
}
