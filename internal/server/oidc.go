package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// randomToken returns a URL-safe random string for OIDC state/nonce values.
func randomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// setFlowCookie sets or clears a short-lived cookie used during the OIDC
// redirect round-trip. SameSite=Lax lets the browser send it back on the
// top-level redirect from the identity provider.
func (s *server) setFlowCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/admin/oidc",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.PublicURL, "https://"),
	})
}

// handleOIDCLogin starts the authorization-code flow: it mints state and
// nonce values, stores them in short-lived cookies, and redirects the user to
// the identity provider.
func (s *server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Redirect(w, r, "/?login_error=oidc_not_configured", http.StatusFound)
		return
	}
	state, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	nonce, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	url, err := s.oidc.AuthCodeURL(r.Context(), state, nonce)
	if err != nil {
		slog.Warn("oidc login failed", "err", err)
		http.Redirect(w, r, "/?login_error=oidc_unavailable", http.StatusFound)
		return
	}
	s.setFlowCookie(w, oidcStateCookie, state, int(oidcFlowTTL.Seconds()))
	s.setFlowCookie(w, oidcNonceCookie, nonce, int(oidcFlowTTL.Seconds()))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOIDCCallback completes the flow: it validates state, verifies the ID
// token against the stored nonce, applies the email allowlist, and — on
// success — issues an admin session and redirects to the app.
func (s *server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Redirect(w, r, "/?login_error=oidc_not_configured", http.StatusFound)
		return
	}
	// Clear the flow cookies regardless of outcome.
	defer func() {
		s.setFlowCookie(w, oidcStateCookie, "", -1)
		s.setFlowCookie(w, oidcNonceCookie, "", -1)
	}()

	if e := r.URL.Query().Get("error"); e != "" {
		slog.Warn("oidc callback: provider returned error", "error", e)
		http.Redirect(w, r, "/?login_error=oidc_denied", http.StatusFound)
		return
	}

	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Redirect(w, r, "/?login_error=oidc_state", http.StatusFound)
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		http.Redirect(w, r, "/?login_error=oidc_state", http.StatusFound)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	identity, err := s.oidc.Verify(ctx, r.URL.Query().Get("code"), nonceCookie.Value)
	if err != nil {
		slog.Warn("oidc callback verification failed", "err", err)
		http.Redirect(w, r, "/?login_error=oidc_verify", http.StatusFound)
		return
	}
	if !s.oidc.Allowed(identity) {
		slog.Warn("oidc login rejected: not on allowlist", "email", identity.Email, "subject", identity.Subject)
		http.Redirect(w, r, "/?login_error=oidc_forbidden", http.StatusFound)
		return
	}

	if err := s.issueSession(w); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("oidc login", "user", identity.DisplayName())
	http.Redirect(w, r, "/", http.StatusFound)
}
