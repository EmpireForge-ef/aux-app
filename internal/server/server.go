// Package server wires the HTTP API: Spotify OAuth, the AI chat endpoint
// (server-sent events), and static serving of the built frontend.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/EmpireForge-ef/aux-app/internal/ai"
	"github.com/EmpireForge-ef/aux-app/internal/chat"
	"github.com/EmpireForge-ef/aux-app/internal/config"
	"github.com/EmpireForge-ef/aux-app/internal/oidcauth"
	"github.com/EmpireForge-ef/aux-app/internal/settings"
	"github.com/EmpireForge-ef/aux-app/internal/spotify"
)

// Run starts the HTTP server and blocks until ctx is cancelled or the server
// fails.
func Run(ctx context.Context, cfg *config.Config, version string) error {
	if warn := redirectURLWarning(cfg.Spotify.RedirectURL); warn != "" {
		log.Printf("warning: %s", warn)
	}

	store, err := settings.Load(cfg.SettingsFile)
	if err != nil {
		return fmt.Errorf("load settings file %s: %w", cfg.SettingsFile, err)
	}
	chats, err := chat.NewStore(cfg.ChatsDir)
	if err != nil {
		return fmt.Errorf("open chats dir %s: %w", cfg.ChatsDir, err)
	}

	s := &server{
		cfg:           cfg,
		version:       version,
		settings:      store,
		chats:         chats,
		adminSessions: newSessionStore(),
		confirms:      make(map[string]chan bool),
	}
	if cfg.OIDC.Enabled() {
		s.oidc = oidcauth.New(oidcauth.Config{
			IssuerURL:     cfg.OIDC.IssuerURL,
			ClientID:      cfg.OIDC.ClientID,
			ClientSecret:  cfg.OIDC.ClientSecret,
			RedirectURL:   cfg.OIDC.RedirectURL,
			Scopes:        strings.Fields(cfg.OIDC.Scopes),
			AllowedEmails: splitAndTrim(cfg.OIDC.AllowedEmails),
		})
		log.Printf("OIDC login enabled (issuer %s)", cfg.OIDC.IssuerURL)
	}
	s.rebuildClients()
	if s.authDisabled() {
		log.Printf("warning: no login method configured — the app runs UNPROTECTED (set AUX_ADMIN_PASSWORD or AUX_OIDC_* before going live)")
	}
	if id, _, _ := s.effectiveCredentials(); id == "" {
		log.Printf("warning: no Spotify client ID configured — set it via AUX_SPOTIFY_CLIENT_ID or the admin settings UI")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)
	mux.HandleFunc("GET /api/admin/session", s.handleAdminSession)
	mux.HandleFunc("GET /api/admin/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("GET /api/admin/oidc/callback", s.handleOIDCCallback)
	mux.HandleFunc("GET /api/admin/settings", s.requireAuth(s.handleGetSettings))
	mux.HandleFunc("PUT /api/admin/settings", s.requireAuth(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/admin/models", s.requireAuth(s.handleListModels))
	mux.HandleFunc("GET /api/auth/login", s.requireAuth(s.handleLogin))
	// The OAuth callback is reached via a redirect from Spotify; the state
	// value minted at /api/auth/login (which is auth-gated) validates it.
	mux.HandleFunc("GET /api/auth/callback", s.handleCallback)
	mux.HandleFunc("GET /api/auth/status", s.requireAuth(s.handleAuthStatus))
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("POST /api/chat", s.requireAuth(s.handleChat))
	mux.HandleFunc("POST /api/chat/confirm", s.requireAuth(s.handleChatConfirm))
	mux.HandleFunc("GET /api/chats", s.requireAuth(s.handleListChats))
	mux.HandleFunc("POST /api/chats", s.requireAuth(s.handleCreateChat))
	mux.HandleFunc("GET /api/chats/{id}", s.requireAuth(s.handleGetChat))
	mux.HandleFunc("PATCH /api/chats/{id}", s.requireAuth(s.handleRenameChat))
	mux.HandleFunc("DELETE /api/chats/{id}", s.requireAuth(s.handleDeleteChat))
	mux.Handle("/", http.FileServer(http.Dir(cfg.StaticDir)))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("aux %s listening on %s (static: %s)", version, cfg.Addr, cfg.StaticDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// redirectURLWarning checks the configured OAuth redirect URL against
// Spotify's requirements: HTTPS everywhere, except plain HTTP on an explicit
// loopback IP (127.0.0.1 or [::1]); "localhost" is never accepted.
func redirectURLWarning(redirect string) string {
	u, err := url.Parse(redirect)
	if err != nil {
		return fmt.Sprintf("spotify redirect URL %q is not a valid URL", redirect)
	}
	host := u.Hostname()
	if host == "localhost" {
		return fmt.Sprintf("spotify redirect URL %q uses 'localhost', which Spotify rejects — use http://127.0.0.1:PORT or http://[::1]:PORT instead", redirect)
	}
	loopback := host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !loopback {
		return fmt.Sprintf("spotify redirect URL %q must use HTTPS (plain HTTP is only allowed on the explicit loopback IPs 127.0.0.1 / [::1])", redirect)
	}
	return ""
}

type server struct {
	cfg           *config.Config
	version       string
	settings      *settings.Store
	chats         *chat.Store
	adminSessions *sessionStore
	oidc          *oidcauth.Authenticator // nil when OIDC is not configured

	// spotify and agent are hot-swapped when the admin changes credentials.
	mu      sync.RWMutex
	spotify *spotify.Manager
	agent   *ai.Agent

	// confirms tracks destructive-action confirmations awaiting a decision
	// from POST /api/chat/confirm, keyed by a per-request confirmation ID.
	confirmMu sync.Mutex
	confirms  map[string]chan bool
}

func (s *server) addConfirm(id string, ch chan bool) {
	s.confirmMu.Lock()
	s.confirms[id] = ch
	s.confirmMu.Unlock()
}

func (s *server) removeConfirm(id string) {
	s.confirmMu.Lock()
	delete(s.confirms, id)
	s.confirmMu.Unlock()
}

// resolveConfirm delivers a decision to a waiting confirmation, reporting
// whether one was pending.
func (s *server) resolveConfirm(id string, approved bool) bool {
	s.confirmMu.Lock()
	ch, ok := s.confirms[id]
	s.confirmMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- approved: // buffered (cap 1); never blocks
	default:
	}
	return true
}

// effectiveCredentials merges the persisted admin settings over the
// environment/file configuration.
func (s *server) effectiveCredentials() (spotifyID, spotifySecret, anthropicKey string) {
	v := s.settings.Get()
	spotifyID, spotifySecret, anthropicKey = s.cfg.Spotify.ClientID, s.cfg.Spotify.ClientSecret, s.cfg.Anthropic.APIKey
	if v.SpotifyClientID != "" {
		spotifyID = v.SpotifyClientID
	}
	if v.SpotifyClientSecret != "" {
		spotifySecret = v.SpotifyClientSecret
	}
	if v.AnthropicAPIKey != "" {
		anthropicKey = v.AnthropicAPIKey
	}
	return
}

// effectiveModel merges the persisted model / max-tokens settings over the
// environment/file configuration.
func (s *server) effectiveModel() (model string, maxTokens int64) {
	v := s.settings.Get()
	model, maxTokens = s.cfg.Anthropic.Model, s.cfg.Anthropic.MaxTokens
	if v.AnthropicModel != "" {
		model = v.AnthropicModel
	}
	if v.AnthropicMaxTokens > 0 {
		maxTokens = v.AnthropicMaxTokens
	}
	return
}

// rebuildClients (re)creates the Spotify manager and AI agent from the
// effective credentials. Called at startup and after settings changes; a
// changed Spotify client ID invalidates the persisted user token anyway, so
// dropping in-flight OAuth state is fine.
func (s *server) rebuildClients() {
	id, secret, key := s.effectiveCredentials()
	model, maxTokens := s.effectiveModel()

	mgr := spotify.NewManager(id, secret, s.cfg.Spotify.RedirectURL, s.cfg.TokenFile)
	if err := mgr.LoadPersisted(); err != nil {
		log.Printf("warning: could not restore spotify token: %v", err)
	}
	agent := ai.New(key, model, maxTokens)

	s.mu.Lock()
	s.spotify = mgr
	s.agent = agent
	s.mu.Unlock()
}

func (s *server) clients() (*spotify.Manager, *ai.Agent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spotify, s.agent
}

// splitAndTrim splits a comma-separated list, dropping empty entries.
func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	mgr, _ := s.clients()
	url, err := mgr.LoginURL()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		http.Redirect(w, r, "/?auth_error="+errMsg, http.StatusFound)
		return
	}
	mgr, _ := s.clients()
	if err := mgr.HandleCallback(r.Context(), q.Get("code"), q.Get("state")); err != nil {
		log.Printf("spotify callback failed: %v", err)
		http.Redirect(w, r, "/?auth_error=callback_failed", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	mgr, _ := s.clients()
	user, err := mgr.CurrentUser(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "error": err.Error()})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          map[string]string{"id": user.ID, "display_name": user.DisplayName},
	})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	mgr, _ := s.clients()
	mgr.Logout()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Chat handlers live in chats.go.
