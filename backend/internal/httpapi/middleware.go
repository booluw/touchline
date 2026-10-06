package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

const identityKey = "identity"

// requireAuth rejects requests without a valid access-token cookie. The JWT
// carries only manager_id + user_id; world_id is never in the token (OPD-15) —
// handlers resolve it from manager.managers when they need it. After
// validation the manager's activity heartbeat is updated (throttled to once per
// hour by the store) so the absence engine can track fixture attendance.
func (s *server) requireAuth(c *gin.Context) {
	raw, err := c.Cookie(accessCookie)
	if err != nil || raw == "" {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}

	identity, err := pkgjwt.ValidateAccessToken(s.jwtCfg, raw)
	if err != nil {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}

	c.Set(identityKey, identity)

	if s.policySvc != nil {
		_ = s.policySvc.TouchActivity(c.Request.Context(), identity.ManagerID)
	}

	c.Next()
}

// requireAdmin gates admin-only routes (world lifecycle, aid offers). Admin
// status lives on auth.users.is_admin and is queried per request — the token
// carries only identity claims, so admin grants/revocations apply immediately.
func (s *server) requireAdmin(c *gin.Context) {
	identity, _ := c.Get(identityKey)
	ident, ok := identity.(*pkgjwt.ManagerIdentity)
	if !ok {
		abortError(c, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}

	var isAdmin bool
	err := s.pool.QueryRow(c.Request.Context(),
		`SELECT is_admin FROM auth.users WHERE id = $1`, ident.UserID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		abortError(c, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	c.Next()
}
