package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	internalauth "github.com/touchline/backend/internal/auth"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

const (
	accessCookie  = "access_token"
	refreshCookie = "refresh_token"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin verifies credentials and establishes a session. Phase-1 launch
// gates login to administrators; an admin session is a world-less console
// session (the admin row has no manager yet on an empty DB).
func (s *server) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be JSON with email and password"})
		return
	}
	if !strings.Contains(req.Email, "@") || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	res, err := s.svc.Login(c.Request.Context(), internalauth.LoginParams{
		Email:             req.Email,
		Password:          req.Password,
		IP:                clientIP(c),
		DeviceFingerprint: deviceFingerprint(c),
	})
	switch {
	case errors.Is(err, internalauth.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	case errors.Is(err, internalauth.ErrNotAuthorized):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	s.setAuthCookies(c, res.TokenPair)
	c.JSON(http.StatusOK, gin.H{"display_name": res.DisplayName})
}

// handleRefresh rotates the refresh-token session and issues a fresh cookie
// pair for the same manager (the world stays bound to the manager row).
func (s *server) handleRefresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookie)
	if err != nil || raw == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing refresh token"})
		return
	}

	res, err := s.svc.Refresh(c.Request.Context(), raw, clientIP(c), deviceFingerprint(c))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
		return
	}

	s.setAuthCookies(c, res.TokenPair)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleDashboard is a protected endpoint proving the session round-trip. The
// aggregation itself is S07-01; it returns the exact empty shape the frontend
// useDashboard composable already types against.
func (s *server) handleDashboard(c *gin.Context) {
	identity, _ := c.Get(identityKey)
	_ = identity // identity is available to the S07-01 aggregator

	c.JSON(http.StatusOK, gin.H{
		"urgent":      []any{},
		"important":   []any{},
		"interesting": []any{},
	})
}

// setAuthCookies writes the httpOnly cookie pair. SameSite=Lax: dev runs the
// Nuxt client on :3000 against the API on :8080 — different origins but the
// same site (host localhost), so Lax cookies are sent on fetch. Secure is only
// set outside development.
func (s *server) setAuthCookies(c *gin.Context, pair *pkgjwt.TokenPair) {
	accessMaxAge := int(time.Until(pair.ExpiresAt).Seconds())
	refreshMaxAge := int(s.jwtCfg.RefreshTTL.Seconds())

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     accessCookie,
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookiesSecure,
		Expires:  pair.ExpiresAt,
		MaxAge:   accessMaxAge,
	})
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookie,
		Value:    pair.RefreshToken,
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookiesSecure,
		MaxAge:   refreshMaxAge,
	})
}

// clientIP parses the request's client IP for auth.sessions.ip_address.
func clientIP(c *gin.Context) *netip.Addr {
	addr, err := netip.ParseAddr(c.ClientIP())
	if err != nil {
		return nil
	}
	return &addr
}

// deviceFingerprint derives a stable per-browser signal (UA hash) for the
// anti-multi-accounting hooks (plan §13). Best-effort; empty when unavailable.
func deviceFingerprint(c *gin.Context) string {
	ua := c.Request.UserAgent()
	if ua == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ua))
	return hex.EncodeToString(sum[:])
}
