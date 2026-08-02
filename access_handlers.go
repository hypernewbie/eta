package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hypernewbie/eta/internal/access"
)

// accessAuthMiddleware gates the desktop's own API behind the access
// password, when one is configured. Off by default: an instance with no
// password behaves exactly as it did before this existed.
//
// A peer's proxied request reaches this same gate as any browser request
// would — it is authenticated the same way, by presenting a session
// cookie this server issued after a successful login (see peer_auth.go).
// There is no separate bypass for "requests from a peer": a peer that
// does not know this instance's password is refused exactly as a browser
// without one would be.
func (s *server) accessAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.accessPublicPath(r.URL.Path) || !s.access.Enabled() || s.access.Authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "access authentication required", http.StatusUnauthorized)
	})
}

func (s *server) accessPublicPath(path string) bool {
	switch path {
	case "/api/auth/status", "/api/auth/login", "/api/healthz", "/api/version", "/api/changelog":
		return true
	// Deliberately public regardless of password: it carries no paths,
	// addresses, or credentials (see hostid.Identity's doc comment), and
	// a peer must be able to read it — to learn this host's accent and
	// name when it is enrolled, and to discover whether this host also
	// needs a password login (handlePeerAdd's own use of it).
	case "/api/identity":
		return true
	}
	// Everything else under /api/ is gated; the SPA shell (index.html,
	// generated/app.js, vendor/*, style.css) stays public so the browser
	// can load the code that draws the login screen before it has
	// logged in.
	return !strings.HasPrefix(path, "/api/")
}

func (s *server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	enabled, salt, challenge, err := s.access.StatusAndChallenge()
	if err != nil {
		http.Error(w, "unable to start login: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	payload := map[string]any{
		"enabled":       enabled,
		"authenticated": enabled && s.access.Authenticated(r),
	}
	if enabled {
		payload["version"] = access.PasswordHashVersion
		payload["algorithm"] = access.PasswordHashAlgorithm
		payload["iterations"] = access.PasswordHashIterations
		payload["salt"] = salt
		payload["challenge"] = challenge
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Challenge string `json:"challenge"`
		Proof     string `json:"proof"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid login request", http.StatusBadRequest)
		return
	}
	token, err := s.access.Login(req.Challenge, req.Proof, r.RemoteAddr)
	if err != nil {
		switch {
		case errors.Is(err, access.ErrRateLimited):
			http.Error(w, err.Error(), http.StatusTooManyRequests)
		case errors.Is(err, access.ErrUnauthorized):
			http.Error(w, "invalid password", http.StatusUnauthorized)
		default:
			http.Error(w, "unable to login", http.StatusInternalServerError)
		}
		return
	}
	if token != "" {
		setAccessSessionCookie(w, r, token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		PasswordHash string `json:"password_hash"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid password configuration", http.StatusBadRequest)
		return
	}
	if len(req.PasswordHash) > 512 {
		http.Error(w, "invalid password configuration", http.StatusBadRequest)
		return
	}
	authorized := s.access.Authenticated(r)
	token, _, err := s.access.UpdatePassword(req.PasswordHash, authorized)
	if err != nil {
		if errors.Is(err, access.ErrUnauthorized) {
			http.Error(w, "access authentication required", http.StatusUnauthorized)
			return
		}
		http.Error(w, "invalid password configuration", http.StatusBadRequest)
		return
	}

	if s.accessPath != "" {
		if err := access.Save(s.accessPath, access.Config{PasswordHash: req.PasswordHash}); err != nil {
			// The in-memory manager is already updated and the cookie
			// below still gets set, so the current browser keeps
			// working; only a restart would lose this change. Reported
			// rather than silently swallowed.
			http.Error(w, "password updated but could not be saved: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if token == "" {
		clearAccessSessionCookie(w, r)
	} else {
		setAccessSessionCookie(w, r, token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": token != ""})
}

func setAccessSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     access.SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(access.SessionTTL),
		MaxAge:   int(access.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAccessSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     access.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}
