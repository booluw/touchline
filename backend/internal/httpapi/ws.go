package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	pkgjwt "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/realtime"
)

// handleWS upgrades the authenticated HTTP request to a WebSocket and registers
// the socket under the caller's world. Authentication happens in requireAuth
// (the access_token cookie travels with the WebSocket handshake); the world is
// derived from the manager row exactly like REST handlers. Events are
// world-scoped on delivery, so a socket can never observe another world's
// stream.
func (s *server) handleWS(c *gin.Context) {
	worldID, err := s.callerWorld(c)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "no world context"})
		return
	}
	ident := c.MustGet(identityKey).(*pkgjwt.ManagerIdentity)

	s.hub.HandleWS(c.Writer, c.Request, realtime.ClientInfo{
		WorldID:   worldID,
		ManagerID: ident.ManagerID,
	})
}
