// Package preferences persists the user's lasting music preferences (favourite
// genres, things to avoid, preferred era, …) so the AI can personalise across
// separate chats. Backed by a PostgreSQL key/value table via GORM.
package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxKeys bounds how many preference entries are kept.
const maxKeys = 40

type pref struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

func (pref) TableName() string { return "preferences" }

// Store holds key→value preferences in PostgreSQL.
type Store struct{ db *gorm.DB }

// New migrates the preferences table and returns a store.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&pref{}); err != nil {
		return nil, fmt.Errorf("migrate preferences: %w", err)
	}
	return &Store{db: db}, nil
}

// List returns the current preferences.
func (s *Store) List() map[string]string {
	var rows []pref
	s.db.Find(&rows)
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
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
	if value == "" {
		return s.db.Delete(&pref{}, "key = ?", key).Error
	}
	var total, existing int64
	s.db.Model(&pref{}).Count(&total)
	s.db.Model(&pref{}).Where("key = ?", key).Count(&existing)
	if existing == 0 && total >= maxKeys {
		return fmt.Errorf("too many preferences (max %d)", maxKeys)
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&pref{Key: key, Value: value}).Error
}

// Clear removes all preferences.
func (s *Store) Clear() error {
	return s.db.Where("1 = 1").Delete(&pref{}).Error
}

// Text renders the preferences for the system prompt, or "" when empty.
func (s *Store) Text() string {
	vals := s.List()
	if len(vals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %s\n", k, vals[k])
	}
	return strings.TrimRight(b.String(), "\n")
}

// ImportFile loads pre-database preferences from a JSON file, but only when the
// table is still empty (so it never clobbers live data). Returns how many were
// imported.
func (s *Store) ImportFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var vals map[string]string
	if err := json.Unmarshal(data, &vals); err != nil {
		return 0, err
	}
	var total int64
	s.db.Model(&pref{}).Count(&total)
	if total > 0 {
		return 0, nil
	}
	n := 0
	for k, v := range vals {
		if err := s.Set(k, v); err == nil {
			n++
		}
	}
	return n, nil
}
