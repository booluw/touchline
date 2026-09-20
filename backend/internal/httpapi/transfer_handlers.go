package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internaltransfer "github.com/touchline/backend/internal/transfer"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

// transferStatus maps transfer sentinel errors to HTTP statuses. Nonexistent
// listings/bids/clubs and cross-world access collapse to 404 so existence is
// never leaked; protocol misuse is 400/403/409.
func transferStatus(c *gin.Context, err error) {
	switch {
	case errors.Is(err, internaltransfer.ErrListingNotFound),
		errors.Is(err, internaltransfer.ErrBidNotFound),
		errors.Is(err, internaltransfer.ErrPlayerNotFound),
		errors.Is(err, internaltransfer.ErrClubNotFound),
		errors.Is(err, internaltransfer.ErrWorldNotPlayable),
		errors.Is(err, internaltransfer.ErrNoActiveClub),
		errors.Is(err, internaltransfer.ErrWorldMismatch),
		errors.Is(err, internaltransfer.ErrPlayerWorldMismatch):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, internaltransfer.ErrNotClubMember),
		errors.Is(err, internaltransfer.ErrNotListingClub),
		errors.Is(err, internaltransfer.ErrNotBidParticipant),
		errors.Is(err, internaltransfer.ErrSelfBid):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, internaltransfer.ErrInvalidTerms),
		errors.Is(err, internaltransfer.ErrInvalidActionForRole),
		errors.Is(err, internaltransfer.ErrCounterLimitReached),
		errors.Is(err, internaltransfer.ErrDuplicateListing),
		errors.Is(err, internaltransfer.ErrDuplicateOpenBid),
		errors.Is(err, internaltransfer.ErrPlayerNotTransferable):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, internaltransfer.ErrInsufficientFunds),
		errors.Is(err, internaltransfer.ErrBidResolved),
		errors.Is(err, internaltransfer.ErrBidExpired):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		internalError(c, err)
	}
}

// transferActor resolves the caller's manager row into a transfer Actor
// (policy bots act through their own manager ids, mirroring commandActor).
func (s *server) transferActor(c *gin.Context) (internaltransfer.Actor, error) {
	ident, ok := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	if !ok {
		return internaltransfer.Actor{}, errors.New("unauthenticated")
	}
	var bot bool
	if err := s.pool.QueryRow(c.Request.Context(), `SELECT is_policy_bot FROM manager.managers WHERE id = $1`, ident.ManagerID).Scan(&bot); err != nil {
		return internaltransfer.Actor{}, err
	}
	return internaltransfer.Actor{ManagerID: ident.ManagerID, IsPolicyBot: bot}, nil
}

func listingParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid listing id"})
		return uuid.Nil, false
	}
	return id, true
}

func (s *server) handleListListings(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	var sellingClub uuid.UUID
	if raw := c.Query("club_id"); raw != "" {
		if sellingClub, err = uuid.Parse(raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid club_id"})
			return
		}
	}
	listings, err := s.transfersSvc.ListListings(c.Request.Context(), worldID,
		c.Query("status"), c.Query("position"), c.Query("listing_type"), sellingClub)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"listings": listings})
}

func (s *server) handleGetListing(c *gin.Context) {
	id, ok := listingParam(c)
	if !ok {
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	l, err := s.transfersSvc.GetListing(c.Request.Context(), worldID, id)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

func (s *server) handleCreateListing(c *gin.Context) {
	var req internaltransfer.CreateListingInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid listing payload"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	actor, err := s.transferActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	l, err := s.transfersSvc.CreateListing(c.Request.Context(), actor, worldID, req)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusCreated, l)
}

func (s *server) handleWithdrawListing(c *gin.Context) {
	id, ok := listingParam(c)
	if !ok {
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	actor, err := s.transferActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	l, err := s.transfersSvc.WithdrawListing(c.Request.Context(), actor, worldID, id)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, l)
}

func (s *server) handlePlaceBid(c *gin.Context) {
	var req internaltransfer.BidInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bid payload"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	actor, err := s.transferActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	bid, transfer, explanation, err := s.transfersSvc.PlaceBid(c.Request.Context(), actor, worldID, req)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"bid": bid, "transfer": transfer, "explanation": explanation})
}

func (s *server) handleListBids(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)
	incoming, outgoing, err := s.transfersSvc.ListBids(c.Request.Context(), worldID, ident.ManagerID)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"incoming": incoming, "outgoing": outgoing})
}

func (s *server) handleRespondBid(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bid id"})
		return
	}
	var req struct {
		Action string                  `json:"action"`
		Terms  *internaltransfer.Terms `json:"terms,omitempty"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid respond payload"})
		return
	}
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	actor, err := s.transferActor(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}
	bid, transfer, explanation, err := s.transfersSvc.RespondToBid(c.Request.Context(), actor, worldID, id, req.Action, req.Terms)
	if err != nil {
		transferStatus(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"bid": bid, "transfer": transfer, "explanation": explanation})
}
