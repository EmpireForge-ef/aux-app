// Package playlistcache remembers the track URIs a playlist contains, keyed by
// its Spotify snapshot_id, so the AI can dedupe before adding without
// re-fetching the whole playlist every time. A snapshot mismatch (the playlist
// was edited, even outside Aux) invalidates the entry. Backed by PostgreSQL via
// GORM; the URIs are stored as JSONB.
package playlistcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxPlaylists bounds how many playlists are cached, evicting the least
// recently written.
const maxPlaylists = 60

type cacheRow struct {
	PlaylistID string    `gorm:"primaryKey"`
	Snapshot   string    `json:"snapshot"`
	URIs       []string  `json:"uris" gorm:"serializer:json;type:jsonb"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime:false;index"`
}

func (cacheRow) TableName() string { return "playlist_cache" }

// Store is a bounded cache of playlist contents in PostgreSQL.
type Store struct{ db *gorm.DB }

// New migrates the playlist-cache table and returns a store.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&cacheRow{}); err != nil {
		return nil, fmt.Errorf("migrate playlist cache: %w", err)
	}
	return &Store{db: db}, nil
}

// Contents returns the cached URIs for id when the cached snapshot matches
// wantSnapshot; ok is false on a miss or a snapshot mismatch.
func (s *Store) Contents(id, wantSnapshot string) (uris []string, ok bool) {
	var row cacheRow
	if err := s.db.First(&row, "playlist_id = ?", id).Error; err != nil {
		return nil, false
	}
	if row.Snapshot != wantSnapshot {
		return nil, false
	}
	return append([]string(nil), row.URIs...), true
}

// Store replaces the cached contents for id and evicts the least-recently-used
// entries beyond the cap.
func (s *Store) Store(id, snapshot string, uris []string) {
	row := cacheRow{PlaylistID: id, Snapshot: snapshot, URIs: dedup(uris), UpdatedAt: time.Now().UTC()}
	s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "playlist_id"}},
		UpdateAll: true,
	}).Create(&row)
	var old []string
	s.db.Model(&cacheRow{}).Order("updated_at desc").Offset(maxPlaylists).Pluck("playlist_id", &old)
	if len(old) > 0 {
		s.db.Where("playlist_id IN ?", old).Delete(&cacheRow{})
	}
}

// Invalidate drops the cached entry for id (e.g. after a change whose new
// contents we can't cheaply recompute); the next dedup re-fetches.
func (s *Store) Invalidate(id string) {
	s.db.Delete(&cacheRow{}, "playlist_id = ?", id)
}

// ImportFile loads a pre-database cache from its JSON file, but only when the
// table is still empty. Returns how many entries were imported.
func (s *Store) ImportFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var on struct {
		Entries map[string]struct {
			Snapshot string   `json:"snapshot"`
			URIs     []string `json:"uris"`
		} `json:"entries"`
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(data, &on); err != nil {
		return 0, err
	}
	var total int64
	s.db.Model(&cacheRow{}).Count(&total)
	if total > 0 {
		return 0, nil
	}
	n := 0
	// Import in LRU order so eviction order carries over.
	for _, id := range on.Order {
		if e, ok := on.Entries[id]; ok {
			s.Store(id, e.Snapshot, e.URIs)
			n++
		}
	}
	return n, nil
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
