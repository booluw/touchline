package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	pkgjwt "github.com/touchline/backend/pkg/auth"
)

const identityKey = "identity"

// requireAuth rejects requests without a valid access-token cookie. The JWT
// carries only manager_id + user_id; world_id is never in the token (OPD-15) —
// handlers resolve it from manager.managers when they need it.
func (s *server) requireAuth(c *gin.Context) {
	raw, err := c.Cookie(accessCookie)
	if err != nil || raw == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	identity, err := pkgjwt.ValidateAccessToken(s.jwtCfg, raw)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	c.Set(identityKey, identity)
	c.Next()
}

// requireAdmin gates admin-only routes (world lifecycle, aid offers). Admin
// status lives on auth.users.is_admin and is queried per request — the token
// carries only identity claims, so admin grants/revocations apply immediately.
func (s *server) requireAdmin(c *gin.Context) {
	identity, _ := c.Get(identityKey)
	ident, ok := identity.(*pkgjwt.ManagerIdentity)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var isAdmin bool
	err := s.pool.QueryRow(c.Request.Context(),
		`SELECT is_admin FROM auth.users WHERE id = $1`, ident.UserID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	c.Next()
}
