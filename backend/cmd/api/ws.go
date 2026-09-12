package main

import (
	"net/http"
	"net/url"

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

// originHostPattern converts APP_ORIGIN (e.g. "http://localhost:3000") into a
// WebSocket origin pattern (e.g. "localhost:3000"). coder/websocket matches
// these against the browser's Origin header to stop cross-site socket
// hijacking; with no configured origin every WS path falls back to same-host
// checks (fine for bare `go run`).
func originHostPattern(appOrigin string) []string {
	if appOrigin == "" {
		return nil
	}
	u, err := url.Parse(appOrigin)
	if err != nil {
		return []string{appOrigin}
	}
	return []string{u.Host}
}
