// Package tempplaylists tracks the playlists the AI created as throwaway,
// editable "queues". Destructive edits to a tracked temp playlist skip the
// user-confirmation gate, since the playlist is disposable.
package tempplaylists

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// maxIDs caps how many temp-playlist IDs are remembered, dropping the oldest.
const maxIDs = 100

// Store is a persisted, ordered set of temp-playlist IDs.
type Store struct {
	path string

	mu  sync.RWMutex
	ids []string // insertion order; oldest first
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
	if err := json.Unmarshal(data, &s.ids); err != nil {
		return nil, err
	}
	return s, nil
}

// Add records a temp-playlist ID (no-op if already present).
func (s *Store) Add(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.ids {
		if existing == id {
			return
		}
	}
	s.ids = append(s.ids, id)
	if len(s.ids) > maxIDs {
		s.ids = s.ids[len(s.ids)-maxIDs:]
	}
	_ = s.persist()
}

// Has reports whether id is a tracked temp playlist.
func (s *Store) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, existing := range s.ids {
		if existing == id {
			return true
		}
	}
	return false
}

// Remove forgets a temp-playlist ID.
func (s *Store) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.ids[:0]
	for _, existing := range s.ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	s.ids = out
	_ = s.persist()
}

func (s *Store) persist() error {
	data, err := json.Marshal(s.ids)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
