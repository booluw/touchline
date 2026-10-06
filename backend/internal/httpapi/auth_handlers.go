package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	internalauth "github.com/touchline/backend/internal/auth"
	internalmanager "github.com/touchline/backend/internal/manager"
	pkgjwt "github.com/touchline/backend/pkg/jwt"
)

const (
	accessCookie  = "access_token"
	refreshCookie = "refresh_token"
)

// internalError logs an unexpected handler error with its call site and
// responds 500 with the actual error message, so failures are never opaque.
func internalError(c *gin.Context, err error) {
	if _, file, line, ok := runtime.Caller(1); ok {
		log.Printf("httpapi %s:%d: %v", filepath.Base(file), line, err)
	} else {
		log.Printf("httpapi: %v", err)
	}
	// The cause stays in the server log: raw errors carry SQL and schema
	// detail that must not reach clients (IM28).
	respondError(c, http.StatusInternalServerError, "internal_server_error", "internal server error")
}

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
		respondError(c, http.StatusBadRequest, "request_body_must_be_json_with_email_and_password", "request body must be JSON with email and password")
		return
	}
	if !strings.Contains(req.Email, "@") || req.Password == "" {
		respondError(c, http.StatusBadRequest, "email_and_password_are_required", "email and password are required")
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
		respondError(c, http.StatusUnauthorized, "invalid_email_or_password", "invalid email or password")
		return
	case errors.Is(err, internalauth.ErrNoManager):
		respondErr(c, http.StatusForbidden, err)
		return
	case errors.Is(err, internalauth.ErrNotMember):
		respondErr(c, http.StatusForbidden, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}

	// World picker outcome: no session yet, the client must choose.
	if res.Worlds != nil {
		c.JSON(http.StatusOK, gin.H{"status": "worlds", "worlds": res.Worlds})
		return
	}

	// An unemployed manager with no pending offer gets one on every login
	// (best-effort, like registration: a failure never blocks the login).
	var offer *internalmanager.JobOffer
	if res.Identity != nil && res.Identity.ManagerID != uuid.Nil {
		o, oErr := s.mgrSvc.EnsureOffer(c.Request.Context(), res.Identity.ManagerID)
		if oErr != nil {
			log.Printf("login %s: no offer for manager %s: %v", res.ID, res.Identity.ManagerID, oErr)
		}
		offer = o
	}

	s.setAuthCookies(c, res.TokenPair)
	c.JSON(http.StatusOK, gin.H{
		"display_name": res.DisplayName,
		"created_at":   res.CreatedAt,
		"is_admin":     res.IsAdmin,
		"id":           res.ID,
		"club":         res.Club,
		"offer":        offer,
	})
}

// handleRefresh rotates the refresh-token session and issues a fresh cookie
// pair for the same manager (the world stays bound to the manager row).
func (s *server) handleRefresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookie)
	if err != nil || raw == "" {
		respondError(c, http.StatusUnauthorized, "missing_refresh_token", "missing refresh token")
		return
	}

	res, err := s.svc.Refresh(c.Request.Context(), raw, clientIP(c), deviceFingerprint(c))
	if err != nil {
		respondError(c, http.StatusUnauthorized, "invalid_or_expired_session", "invalid or expired session")
		return
	}

	s.setAuthCookies(c, res.TokenPair)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "club": res.Club})
}

// handleLogout ends the session server-side (IM13): the refresh-token session
// row is deleted and both cookies are cleared, regardless of whether a session
// actually matched. Deliberately unauthenticated — a logged-out user's access
// token may already be expired, so logout must never depend on requireAuth.
// The already-issued access JWT stays valid until its own TTL (a documented
// grace window; it can no longer be refreshed).
func (s *server) handleLogout(c *gin.Context) {
	raw, err := c.Cookie(refreshCookie)
	if err == nil && raw != "" {
		if err := s.svc.Logout(c.Request.Context(), raw); err != nil {
			internalError(c, err)
			return
		}
	}
	s.clearAuthCookies(c)
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
		respondError(c, http.StatusBadRequest, "request_body_must_be_json_with_email_password_and_optional_display_name", "request body must be JSON with email, password and optional display_name")
		return
	}
	if !strings.Contains(req.Email, "@") || req.Password == "" {
		respondError(c, http.StatusBadRequest, "email_and_password_are_required", "email and password are required")
		return
	}

	res, err := s.svc.Register(c.Request.Context(), internalauth.RegisterParams{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
	})
	switch {
	case errors.Is(err, internalauth.ErrEmailTaken):
		respondErr(c, http.StatusConflict, err)
		return
	case errors.Is(err, internalauth.ErrPasswordTooLong):
		respondErr(c, http.StatusBadRequest, err)
		return
	case err != nil:
		internalError(c, err)
		return
	}

	// Auto-offer (best-effort, A13): a random league club not already proposing
	// to someone else. A failure never fails the registration, but it is logged
	// so an admin can see why a new manager has no offer (IM28).
	var offer *internalmanager.JobOffer
	if res.JoinedWorld != nil {
		o, oErr := s.mgrSvc.OfferOnboardingJob(c.Request.Context(), res.JoinedWorld.ID, *res.ManagerID, uuid.Nil)
		if oErr != nil {
			log.Printf("register %s: no onboarding offer in world %s: %v", res.UserID, res.JoinedWorld.ID, oErr)
		} else {
			offer = o
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

// clearAuthCookies deletes both auth cookies. The Path values must mirror
// setAuthCookies exactly (access "/", refresh "/api/auth"), or the browser
// keeps a stale cookie that silently shadows the deletion.
func (s *server) clearAuthCookies(c *gin.Context) {
	clear := &http.Cookie{
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookiesSecure,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	}
	access := *clear
	access.Name, access.Path = accessCookie, "/"
	http.SetCookie(c.Writer, &access)

	refresh := *clear
	refresh.Name, refresh.Path = refreshCookie, "/api/auth"
	http.SetCookie(c.Writer, &refresh)
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
