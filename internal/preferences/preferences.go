// Package preferences persists the user's lasting music preferences (favourite
// genres, things to avoid, preferred era, …) so the AI can personalise across
// separate chats.
package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// maxKeys bounds how many preference entries are kept.
const maxKeys = 40

// Store holds key→value preferences persisted to a JSON file.
type Store struct {
	path string

	mu     sync.RWMutex
	values map[string]string
}

// Load opens (or initialises) the store at path.
func Load(path string) (*Store, error) {
	s := &Store{path: path, values: map[string]string{}}
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
	if s.values == nil {
		s.values = map[string]string{}
	}
	return s, nil
}

// List returns a copy of the current preferences.
func (s *Store) List() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// Set stores value under key (an empty value deletes the key) and persists.
func (s *Store) Set(key, value string) error {
	key = strings.TrimSpace(strings.ToLower(key))
	value = strings.TrimSpace(value)
	if key == "" {
		return errors.New("preference key must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.values, key)
	} else {
		if _, exists := s.values[key]; !exists && len(s.values) >= maxKeys {
			return fmt.Errorf("too many preferences (max %d)", maxKeys)
		}
		s.values[key] = value
	}
	return s.persist()
}

// Clear removes all preferences.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = map[string]string{}
	return s.persist()
}

// Text renders the preferences for the system prompt, or "" when empty.
func (s *Store) Text() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(s.values))
	for k := range s.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %s\n", k, s.values[k])
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *Store) persist() error {
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
