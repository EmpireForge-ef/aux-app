// Package history remembers the track URIs the AI recently queued or added, so
// it can be told to stop recommending the same songs over and over — across
// chats, not just within one.
package history

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// maxEntries caps how many recent URIs are kept, dropping the oldest.
const maxEntries = 500

// Store is a persisted, recency-ordered, de-duplicated list of track URIs
// (most recent last).
type Store struct {
	path string

	mu   sync.RWMutex
	uris []string
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
	if err := json.Unmarshal(data, &s.uris); err != nil {
		return nil, err
	}
	return s, nil
}

// Add records URIs as recently used; existing ones move to the most-recent end
// and the list is trimmed to the cap.
func (s *Store) Add(uris []string) {
	if len(uris) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	incoming := make(map[string]bool, len(uris))
	for _, u := range uris {
		if u != "" {
			incoming[u] = true
		}
	}
	// Drop existing occurrences of the incoming URIs, then append them at the
	// end so recency is preserved.
	kept := s.uris[:0]
	for _, u := range s.uris {
		if !incoming[u] {
			kept = append(kept, u)
		}
	}
	s.uris = kept
	for _, u := range uris {
		if u != "" {
			s.uris = append(s.uris, u)
		}
	}
	if len(s.uris) > maxEntries {
		s.uris = s.uris[len(s.uris)-maxEntries:]
	}
	_ = s.persist()
}

// Recent returns up to n of the most recently used URIs, newest first.
func (s *Store) Recent(n int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.uris) {
		n = len(s.uris)
	}
	out := make([]string, 0, n)
	for i := len(s.uris) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.uris[i])
	}
	return out
}

func (s *Store) persist() error {
	data, err := json.Marshal(s.uris)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
