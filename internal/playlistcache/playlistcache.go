// Package playlistcache remembers the track URIs a playlist contains, keyed by
// its Spotify snapshot_id, so the AI can dedupe before adding without
// re-fetching the whole playlist every time. A snapshot mismatch (the playlist
// was edited, even outside Aux) invalidates the entry.
package playlistcache

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// maxPlaylists bounds how many playlists are cached, evicting the least
// recently written.
const maxPlaylists = 60

type entry struct {
	Snapshot string   `json:"snapshot"`
	URIs     []string `json:"uris"`
}

// Store is a persisted, bounded cache of playlist contents.
type Store struct {
	path string

	mu      sync.Mutex
	entries map[string]entry
	order   []string // playlist IDs, least-recently-written first
}

// Load opens (or initialises) the cache at path.
func Load(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	var on disk
	if err := json.Unmarshal(data, &on); err != nil {
		return nil, err
	}
	if on.Entries != nil {
		s.entries = on.Entries
	}
	s.order = on.Order
	return s, nil
}

type disk struct {
	Entries map[string]entry `json:"entries"`
	Order   []string         `json:"order"`
}

// Contents returns the cached URIs for id when the cached snapshot matches
// wantSnapshot; ok is false on a miss or a snapshot mismatch.
func (s *Store) Contents(id, wantSnapshot string) (uris []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, present := s.entries[id]
	if !present || e.Snapshot != wantSnapshot {
		return nil, false
	}
	return append([]string(nil), e.URIs...), true
}

// Store replaces the cached contents for id.
func (s *Store) Store(id, snapshot string, uris []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = entry{Snapshot: snapshot, URIs: dedup(uris)}
	s.touch(id)
	s.evict()
	_ = s.persist()
}

// Invalidate drops the cached entry for id (e.g. after a change whose new
// contents we can't cheaply recompute); the next dedup re-fetches.
func (s *Store) Invalidate(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return
	}
	delete(s.entries, id)
	out := s.order[:0]
	for _, v := range s.order {
		if v != id {
			out = append(out, v)
		}
	}
	s.order = out
	_ = s.persist()
}

func (s *Store) touch(id string) {
	out := s.order[:0]
	for _, v := range s.order {
		if v != id {
			out = append(out, v)
		}
	}
	s.order = append(out, id)
}

func (s *Store) evict() {
	for len(s.order) > maxPlaylists {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.entries, oldest)
	}
}

func (s *Store) persist() error {
	data, err := json.Marshal(disk{Entries: s.entries, Order: s.order})
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func dedup(uris []string) []string {
	seen := make(map[string]bool, len(uris))
	out := make([]string, 0, len(uris))
	for _, u := range uris {
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}
