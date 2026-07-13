package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/EmpireForge-ef/aux-app/internal/settings"
)

const (
	sessionCookie   = "aux_admin"
	oidcStateCookie = "aux_oidc_state"
	oidcNonceCookie = "aux_oidc_nonce"
	sessionTTL      = 7 * 24 * time.Hour
	oidcFlowTTL     = 10 * time.Minute
)

// sessionStore keeps admin session tokens in memory. Restarting the server
// logs everyone out, which is fine for a single-admin app.
type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{tokens: make(map[string]time.Time)}
}

func (s *sessionStore) create() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	s.mu.Lock()
	s.tokens[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return token, nil
}

func (s *sessionStore) valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, token)
		return false
	}
	return true
}

func (s *sessionStore) drop(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

// passwordEnabled reports whether password login is configured.
func (s *server) passwordEnabled() bool {
	return s.cfg.Admin.Password != ""
}

// authDisabled reports whether no login method is configured at all, in which
// case the app runs unprotected (local development).
func (s *server) authDisabled() bool {
	return !s.passwordEnabled() && !s.cfg.OIDC.Enabled()
}

func (s *server) isAuthenticated(r *http.Request) bool {
	if s.authDisabled() {
		return true
	}
	cookie, err := r.Cookie(sessionCookie)
	return err == nil && s.adminSessions.valid(cookie.Value)
}

// requireAuth guards a handler behind the admin session.
func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next(w, r)
	}
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.cfg.PublicURL, "https://"),
	})
}

// issueSession creates an admin session and sets its cookie.
func (s *server) issueSession(w http.ResponseWriter) error {
	token, err := s.adminSessions.create()
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, int(sessionTTL.Seconds()))
	return nil
}

// handleAdminLogin exchanges the admin password for a session cookie.
func (s *server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if s.authDisabled() {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "auth_disabled": true})
		return
	}
	if !s.passwordEnabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password login is not enabled; use single sign-on"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.cfg.Admin.Password)) != 1 {
		time.Sleep(500 * time.Millisecond) // damp brute-force attempts
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong password"})
		return
	}
	if err := s.issueSession(w); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (s *server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.adminSessions.drop(cookie.Value)
	}
	s.setSessionCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAdminSession lets the frontend decide which login options to show.
func (s *server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":    s.isAuthenticated(r),
		"auth_disabled":    s.authDisabled(),
		"password_enabled": s.passwordEnabled(),
		"oidc_enabled":     s.cfg.OIDC.Enabled(),
	})
}

// handleGetSettings returns the current settings. Secrets are masked; only
// the last characters stay visible so the admin can recognise which key is
// configured. The model and token cap are shown in full (not sensitive).
func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	id, secret, key := s.effectiveCredentials()
	model, maxTokens := s.effectiveModel()
	writeJSON(w, http.StatusOK, map[string]any{
		"spotify_client_id":     id, // public identifier, shown in full
		"spotify_client_secret": settings.Mask(secret),
		"anthropic_api_key":     settings.Mask(key),
		"anthropic_model":       model,
		"anthropic_max_tokens":  maxTokens,
		"timezone":              s.effectiveTimezone(),
	})
}

// handleListModels returns the models available to the configured Anthropic
// API key, so the admin can pick a cheaper/faster one to save tokens.
func (s *server) handleListModels(w http.ResponseWriter, r *http.Request) {
	_, agent := s.clients()
	models, err := agent.ListModels(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "could not fetch models (is the Anthropic API key set?): " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleUpdateSettings applies changed credentials, persists them, and
// hot-swaps the Spotify and AI clients. Empty fields are left unchanged.
func (s *server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req settings.Values
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req == (settings.Values{}) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nothing to update"})
		return
	}
	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown timezone " + req.Timezone + " (use an IANA name like Europe/Berlin)"})
			return
		}
	}
	if _, err := s.settings.Update(req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist settings: " + err.Error()})
		return
	}
	s.rebuildClients()
	s.handleGetSettings(w, r)
}
