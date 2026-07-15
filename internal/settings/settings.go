// Package settings persists runtime-changeable credentials (Spotify app
// credentials, Anthropic API key) and preferences (model, timezone) so they can
// be managed from the admin UI instead of the environment. Backed by a
// single-row PostgreSQL table via GORM; the values are cached in memory for the
// hot read path.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Timezone is an IANA name (e.g. "Europe/Berlin"); empty leaves the
	// configured/server zone in place.
	Timezone string `json:"timezone,omitempty"`
	// Location is a "lat,lon" pair or a place name for weather tagging; empty
	// leaves the configured value in place.
	Location string `json:"location,omitempty"`
}

// record is the single-row GORM model holding the settings blob.
type record struct {
	ID     uint   `gorm:"primaryKey"`
	Values Values `gorm:"serializer:json;type:jsonb"`
}

func (record) TableName() string { return "settings" }

// Store loads and saves Values in PostgreSQL, caching them in memory.
type Store struct {
	db *gorm.DB

	mu     sync.RWMutex
	values Values
}

// New migrates the settings table and loads the current values.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&record{}); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	var r record
	err := db.First(&r, 1).Error
	if err == nil {
		s.values = r.Values
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
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
	if v.Timezone != "" {
		s.values.Timezone = strings.TrimSpace(v.Timezone)
	}
	if v.Location != "" {
		s.values.Location = strings.TrimSpace(v.Location)
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&record{ID: 1, Values: s.values}).Error; err != nil {
		return Values{}, err
	}
	return s.values, nil
}

// ImportFile loads pre-database settings from a JSON file, but only when no
// settings row exists yet (so it never clobbers live data).
func (s *Store) ImportFile(path string) error {
	var total int64
	s.db.Model(&record{}).Count(&total)
	if total > 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var v Values
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	_, err = s.Update(v)
	return err
}

// Mask abbreviates a secret for display: the last four characters stay visible
// for recognition, everything else is blurred. Empty input stays empty
// (meaning "not set").
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "••••••••"
	}
	return "••••••••" + secret[len(secret)-4:]
}
