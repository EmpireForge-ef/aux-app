// Package tempplaylists tracks the playlists the AI created as throwaway,
// editable "queues". Destructive edits to a tracked temp playlist skip the
// user-confirmation gate, since the playlist is disposable. Backed by
// PostgreSQL via GORM.
package tempplaylists

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxIDs caps how many temp-playlist IDs are remembered, dropping the oldest.
const maxIDs = 100

type temp struct {
	ID        string `gorm:"primaryKey"`
	CreatedAt time.Time
}

func (temp) TableName() string { return "temp_playlists" }

// weekdayQueue is the reusable queue playlist for one weekday (0=Sunday). The
// AI adds songs to it and the app clears it the first time it's used in a new
// week, giving the user a rolling week to save favourites.
type weekdayQueue struct {
	Weekday    int `gorm:"primaryKey"` // 0=Sunday … 6=Saturday
	PlaylistID string
	Name       string
	LastUsed   string // local date YYYY-MM-DD of the last time it was used
}

func (weekdayQueue) TableName() string { return "weekday_queues" }

// Store is an ordered set of temp-playlist IDs in PostgreSQL.
type Store struct{ db *gorm.DB }

// New migrates the temp-playlists table and returns a store.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&temp{}, &weekdayQueue{}); err != nil {
		return nil, fmt.Errorf("migrate temp playlists: %w", err)
	}
	return &Store{db: db}, nil
}

// WeekdayQueue returns the stored queue playlist for a weekday (0=Sunday), and
// the local date it was last used.
func (s *Store) WeekdayQueue(weekday int) (playlistID, lastUsed string, ok bool) {
	var q weekdayQueue
	if err := s.db.First(&q, "weekday = ?", weekday).Error; err != nil {
		return "", "", false
	}
	return q.PlaylistID, q.LastUsed, true
}

// SetWeekdayQueue records (or replaces) the queue playlist for a weekday.
func (s *Store) SetWeekdayQueue(weekday int, playlistID, name, lastUsed string) {
	s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "weekday"}}, UpdateAll: true}).
		Create(&weekdayQueue{Weekday: weekday, PlaylistID: playlistID, Name: name, LastUsed: lastUsed})
}

// MarkWeekdayUsed updates the last-used date for a weekday's queue.
func (s *Store) MarkWeekdayUsed(weekday int, lastUsed string) {
	s.db.Model(&weekdayQueue{}).Where("weekday = ?", weekday).Update("last_used", lastUsed)
}

// Add records a temp-playlist ID (no-op if already present) and trims the
// oldest beyond the cap.
func (s *Store) Add(id string) {
	if id == "" {
		return
	}
	s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&temp{ID: id, CreatedAt: time.Now().UTC()})
	var old []string
	s.db.Model(&temp{}).Order("created_at desc").Offset(maxIDs).Pluck("id", &old)
	if len(old) > 0 {
		s.db.Where("id IN ?", old).Delete(&temp{})
	}
}

// Has reports whether id is a tracked temp playlist.
func (s *Store) Has(id string) bool {
	var count int64
	s.db.Model(&temp{}).Where("id = ?", id).Count(&count)
	return count > 0
}

// Remove forgets a temp-playlist ID.
func (s *Store) Remove(id string) {
	s.db.Delete(&temp{}, "id = ?", id)
}

// ImportFile loads pre-database temp-playlist IDs from a JSON array, but only
// when the table is still empty. Returns how many were imported.
func (s *Store) ImportFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return 0, err
	}
	var total int64
	s.db.Model(&temp{}).Count(&total)
	if total > 0 {
		return 0, nil
	}
	n := 0
	now := time.Now().UTC()
	for i, id := range ids {
		if id == "" {
			continue
		}
		// Preserve order via monotonically increasing timestamps.
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&temp{ID: id, CreatedAt: now.Add(time.Duration(i) * time.Millisecond)}).Error; err == nil {
			n++
		}
	}
	return n, nil
}
