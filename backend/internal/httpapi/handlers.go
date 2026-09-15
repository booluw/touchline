package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalauth "github.com/touchline/backend/internal/auth"
	internalmanager "github.com/touchline/backend/internal/manager"
	pkgjwt "github.com/touchline/backend/pkg/auth"
)

const (
	accessCookie  = "access_token"
	refreshCookie = "refresh_token"
)

type loginRequest struct {
	Email    string     `json:"email"`
	Password string     `json:"password"`
	WorldID  *uuid.UUID `json:"world_id"`
}

// handleLogin verifies credentials and opens a session.
//
// An administrator always gets a world-less console session. A non-admin
// account is resolved per OPD-15(4): when the account is a member of several
// worlds the response is the world picker {"status":"worlds","worlds":[...]}
// with NO cookies, and the client re-posts login with the chosen world_id.
// A non-admin with no joined world gets 403 (ErrNoManager).
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
		WorldID:           req.WorldID,
	})
	switch {
	case errors.Is(err, internalauth.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	case errors.Is(err, internalauth.ErrNoManager):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	case errors.Is(err, internalauth.ErrNotMember):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// World picker outcome: no session yet, the client must choose.
	if res.Worlds != nil {
		c.JSON(http.StatusOK, gin.H{"status": "worlds", "worlds": res.Worlds})
		return
	}

	s.setAuthCookies(c, res.TokenPair)
	c.JSON(http.StatusOK, gin.H{
		"display_name": res.DisplayName,
		"created_at":   res.CreatedAt,
		"is_admin":     res.IsAdmin,
		"id":           res.ID,
	})
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

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// handleRegister is the product signup flow (A13): it creates a plain
// (non-admin) account, auto-joins the single playable world as an unemployed
// manager, and auto-issues a first job offer from the first available AI club.
// No session is minted by registration; the new manager logs in normally.
// Auto-offer failures are non-fatal (offer: null — an admin can offer later).
func (s *server) handleRegister(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be JSON with email, password and optional display_name"})
		return
	}
	if !strings.Contains(req.Email, "@") || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	res, err := s.svc.Register(c.Request.Context(), internalauth.RegisterParams{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	switch {
	case errors.Is(err, internalauth.ErrEmailTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Auto-offer (best-effort): the deterministic first available AI club.
	var offer *internalmanager.JobOffer
	if res.JoinedWorld != nil {
		clubID, oErr := s.mgrSvc.OnboardingAIClubID(c.Request.Context(), res.JoinedWorld.ID)
		if oErr == nil {
			offer, _ = s.mgrSvc.CreateJobOffer(c.Request.Context(), clubID, *res.ManagerID)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":           res.UserID,
		"email":        req.Email,
		"display_name": res.DisplayName,
		"is_admin":     false,
		"world":        res.JoinedWorld,
		"offer":        offer,
	})
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
