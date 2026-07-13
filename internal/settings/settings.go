// Package settings persists runtime-changeable credentials (Spotify app
// credentials, Anthropic API key) so they can be managed from the admin UI
// instead of the environment.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
)

// Values are the settings the admin can change at runtime. Values set here
// override the environment/file configuration. Empty / zero fields mean
// "unset" and leave the configured value in place.
type Values struct {
	SpotifyClientID     string `json:"spotify_client_id,omitempty"`
	SpotifyClientSecret string `json:"spotify_client_secret,omitempty"`
	AnthropicAPIKey     string `json:"anthropic_api_key,omitempty"`
	AnthropicModel      string `json:"anthropic_model,omitempty"`
	AnthropicMaxTokens  int64  `json:"anthropic_max_tokens,omitempty"`
}

// Store loads and saves Values on disk. The file is created with 0600 since
// it holds secrets.
type Store struct {
	path string

	mu     sync.RWMutex
	values Values
}

// Load opens (or initialises) the store at path.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &s.values); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns the current values.
func (s *Store) Get() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values
}

// Update overwrites the non-empty fields of v and persists the result.
func (s *Store) Update(v Values) (Values, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.SpotifyClientID != "" {
		s.values.SpotifyClientID = strings.TrimSpace(v.SpotifyClientID)
	}
	if v.SpotifyClientSecret != "" {
		s.values.SpotifyClientSecret = strings.TrimSpace(v.SpotifyClientSecret)
	}
	if v.AnthropicAPIKey != "" {
		s.values.AnthropicAPIKey = strings.TrimSpace(v.AnthropicAPIKey)
	}
	if v.AnthropicModel != "" {
		s.values.AnthropicModel = strings.TrimSpace(v.AnthropicModel)
	}
	if v.AnthropicMaxTokens > 0 {
		s.values.AnthropicMaxTokens = v.AnthropicMaxTokens
	}
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return Values{}, err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return Values{}, err
	}
	return s.values, nil
}

// Mask abbreviates a secret for display: the last four characters stay
// visible for recognition, everything else is blurred. Empty input stays
// empty (meaning "not set").
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "••••••••"
	}
	return "••••••••" + secret[len(secret)-4:]
}
