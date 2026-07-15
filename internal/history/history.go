// Package history remembers the track URIs the AI recently queued or added, so
// it can be told to stop recommending the same songs over and over — across
// chats, not just within one. Backed by PostgreSQL via GORM: one row per URI,
// ordered by insertion.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gorm.io/gorm"
)

// maxEntries caps how many recent URIs are kept, dropping the oldest.
const maxEntries = 500

type entry struct {
	ID  uint   `gorm:"primaryKey"`
	URI string `gorm:"index"`
}

func (entry) TableName() string { return "history_entries" }

// Store is a recency-ordered, de-duplicated list of track URIs in PostgreSQL.
type Store struct{ db *gorm.DB }

// New migrates the history table and returns a store.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&entry{}); err != nil {
		return nil, fmt.Errorf("migrate history: %w", err)
	}
	return &Store{db: db}, nil
}

// Add records URIs as recently used: existing ones move to the most-recent end
// (higher IDs) and the list is trimmed to the cap.
func (s *Store) Add(uris []string) {
	if len(uris) == 0 {
		return
	}
	seen := make(map[string]bool, len(uris))
	clean := make([]string, 0, len(uris))
	for _, u := range uris {
		if u != "" && !seen[u] {
			seen[u] = true
			clean = append(clean, u)
		}
	}
	if len(clean) == 0 {
		return
	}
	_ = s.db.Transaction(func(tx *gorm.DB) error {
		// Re-insert existing URIs at the end so recency is preserved.
		tx.Where("uri IN ?", clean).Delete(&entry{})
		rows := make([]entry, len(clean))
		for i, u := range clean {
			rows[i] = entry{URI: u}
		}
		tx.Create(&rows)
		// Trim everything older than the newest maxEntries.
		var ids []uint
		tx.Model(&entry{}).Order("id desc").Offset(maxEntries).Limit(1).Pluck("id", &ids)
		if len(ids) == 1 {
			tx.Where("id <= ?", ids[0]).Delete(&entry{})
		}
		return nil
	})
}

// Recent returns up to n of the most recently used URIs, newest first. n<=0
// returns all of them.
func (s *Store) Recent(n int) []string {
	q := s.db.Model(&entry{}).Order("id desc")
	if n > 0 {
		q = q.Limit(n)
	}
	var uris []string
	q.Pluck("uri", &uris)
	return uris
}

// ImportFile loads pre-database history from a JSON array of URIs (oldest
// first), but only when the table is still empty. Returns how many were
// imported.
func (s *Store) ImportFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var uris []string
	if err := json.Unmarshal(data, &uris); err != nil {
		return 0, err
	}
	var total int64
	s.db.Model(&entry{}).Count(&total)
	if total > 0 || len(uris) == 0 {
		return 0, nil
	}
	s.Add(uris)
	return len(uris), nil
}
