// Package spotify wraps github.com/EmpireForge-ef/spotify-go-wrapper with the
// OAuth authorization-code flow and token persistence for a single user.
package spotify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// Scopes covers every capability the wrapper exposes so the AI can use the
// entire API surface on the user's behalf.
var Scopes = []string{
	spotify.ScopeUGCImageUpload,
	spotify.ScopeUserReadPlaybackState,
	spotify.ScopeUserModifyPlaybackState,
	spotify.ScopeUserReadCurrentlyPlaying,
	spotify.ScopePlaylistReadPrivate,
	spotify.ScopePlaylistReadCollaborative,
	spotify.ScopePlaylistModifyPrivate,
	spotify.ScopePlaylistModifyPublic,
	spotify.ScopeUserFollowModify,
	spotify.ScopeUserFollowRead,
	spotify.ScopeUserReadPlaybackPosition,
	spotify.ScopeUserTopRead,
	spotify.ScopeUserReadRecentlyPlayed,
	spotify.ScopeUserLibraryModify,
	spotify.ScopeUserLibraryRead,
	spotify.ScopeUserReadEmail,
	spotify.ScopeUserReadPrivate,
}

// Manager owns the Spotify authenticator, the persisted user token, and the
// API client built from it.
type Manager struct {
	auth      *spotify.Authenticator
	tokenFile string

	mu     sync.RWMutex
	client *spotify.Client
	state  string
}

func NewManager(clientID, clientSecret, redirectURL, tokenFile string) *Manager {
	return &Manager{
		auth: spotify.NewAuthenticator(clientID,
			spotify.WithClientSecret(clientSecret),
			spotify.WithRedirectURL(redirectURL),
		),
		tokenFile: tokenFile,
	}
}

// LoadPersisted restores a previously saved token, if any, so users don't
// have to re-authorize after a restart.
func (m *Manager) LoadPersisted() error {
	data, err := os.ReadFile(m.tokenFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var tok spotify.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return fmt.Errorf("parse token file %s: %w", m.tokenFile, err)
	}
	m.setClient(&tok)
	return nil
}

// LoginURL returns the Spotify consent URL and remembers the state value for
// callback validation.
func (m *Manager) LoginURL() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	state := hex.EncodeToString(buf)

	m.mu.Lock()
	m.state = state
	m.mu.Unlock()

	return m.auth.AuthURL(state, Scopes), nil
}

// HandleCallback validates the OAuth state, exchanges the code, and installs
// the authenticated client.
func (m *Manager) HandleCallback(ctx context.Context, code, state string) error {
	m.mu.RLock()
	expected := m.state
	m.mu.RUnlock()
	if expected == "" || state != expected {
		return errors.New("oauth state mismatch")
	}

	tok, err := m.auth.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	m.persist(tok)
	m.setClient(tok)
	return nil
}

func (m *Manager) setClient(tok *spotify.Token) {
	src := m.auth.TokenSource(tok, m.persist)
	client := spotify.NewClient(src)

	m.mu.Lock()
	m.client = client
	m.mu.Unlock()
}

func (m *Manager) persist(tok *spotify.Token) {
	if m.tokenFile == "" {
		return
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return
	}
	_ = os.WriteFile(m.tokenFile, data, 0o600)
}

// Client returns the authenticated Spotify client, or false if the user has
// not connected their account yet.
func (m *Manager) Client() (*spotify.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client, m.client != nil
}

// CurrentUser fetches the connected user's profile, or nil if not connected.
func (m *Manager) CurrentUser(ctx context.Context) (*spotify.PrivateUser, error) {
	c, ok := m.Client()
	if !ok {
		return nil, nil
	}
	return c.GetCurrentUser(ctx)
}

// Logout drops the client and deletes the persisted token.
func (m *Manager) Logout() {
	m.mu.Lock()
	m.client = nil
	m.state = ""
	m.mu.Unlock()
	if m.tokenFile != "" {
		_ = os.Remove(m.tokenFile)
	}
}
