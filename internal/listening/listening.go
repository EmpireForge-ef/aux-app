// Package listening builds a passive profile of the user's music habits: a
// background poller records what they played and when (with time-of-day,
// weekday, and weather context), and the AI queries the aggregated profile to
// learn their patterns. Backed by PostgreSQL via GORM.
package listening

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PlayEvent is one recorded track play with its context.
type PlayEvent struct {
	ID uint `gorm:"primaryKey"`
	// A play is identified by (played_at, track_uri) so re-polling is
	// idempotent.
	PlayedAt   time.Time `gorm:"uniqueIndex:idx_play,priority:1;index"`
	TrackURI   string    `gorm:"uniqueIndex:idx_play,priority:2"`
	TrackName  string
	ArtistURI  string
	ArtistName string
	Genres     []string `gorm:"serializer:json;type:jsonb"`
	Hour       int      // local hour 0–23
	DayOfWeek  int      // 0=Sunday … 6=Saturday (local)
	IsWeekend  bool     `gorm:"index"`
	PartOfDay  string   `gorm:"index"` // night | morning | afternoon | evening
	Weather    string   `gorm:"index"` // clear, rain, … or "" when unknown
	TempC      *float64
}

func (PlayEvent) TableName() string { return "play_events" }

// artistGenres caches an artist's genres so the poller doesn't refetch them.
type artistGenres struct {
	ArtistURI string   `gorm:"primaryKey"`
	Genres    []string `gorm:"serializer:json;type:jsonb"`
	FetchedAt time.Time
}

func (artistGenres) TableName() string { return "artist_genres" }

// pollState holds the single-row ingestion cursor.
type pollState struct {
	ID           uint `gorm:"primaryKey"`
	LastPlayedAt time.Time
}

func (pollState) TableName() string { return "listening_poll_state" }

// Store persists play events, the genre cache, and the poll cursor.
type Store struct{ db *gorm.DB }

// New migrates the listening tables and returns a store.
func New(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&PlayEvent{}, &artistGenres{}, &pollState{}); err != nil {
		return nil, fmt.Errorf("migrate listening tables: %w", err)
	}
	return &Store{db: db}, nil
}

// LastPlayedAt returns the cursor: the most recent play we've recorded.
func (s *Store) LastPlayedAt() time.Time {
	var st pollState
	s.db.First(&st, 1)
	return st.LastPlayedAt
}

// SetLastPlayedAt advances the cursor.
func (s *Store) SetLastPlayedAt(t time.Time) {
	s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).
		Create(&pollState{ID: 1, LastPlayedAt: t})
}

// Insert stores new play events, ignoring duplicates by (played_at, track_uri).
// Returns how many rows were newly inserted.
func (s *Store) Insert(events []PlayEvent) int {
	if len(events) == 0 {
		return 0
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&events)
	return int(res.RowsAffected)
}

// CachedGenres returns cached genres for an artist and whether the cache had a
// fresh entry (younger than 30 days).
func (s *Store) CachedGenres(artistURI string) ([]string, bool) {
	var row artistGenres
	if err := s.db.First(&row, "artist_uri = ?", artistURI).Error; err != nil {
		return nil, false
	}
	if time.Since(row.FetchedAt) > 30*24*time.Hour {
		return nil, false
	}
	return row.Genres, true
}

// CacheGenres stores an artist's genres.
func (s *Store) CacheGenres(artistURI string, genres []string) {
	s.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "artist_uri"}}, UpdateAll: true}).
		Create(&artistGenres{ArtistURI: artistURI, Genres: genres, FetchedAt: time.Now().UTC()})
}

// PartOfDay buckets a local hour into a coarse part of the day.
func PartOfDay(hour int) string {
	switch {
	case hour < 6:
		return "night"
	case hour < 12:
		return "morning"
	case hour < 18:
		return "afternoon"
	default:
		return "evening"
	}
}

// --- profile aggregation ----------------------------------------------------

// Filter narrows the profile to a slice of the listening history.
type Filter struct {
	PartOfDay string
	Weekend   *bool
	Weather   string
	Days      int
}

func (f Filter) scope(db *gorm.DB) *gorm.DB {
	if f.PartOfDay != "" {
		db = db.Where("part_of_day = ?", f.PartOfDay)
	}
	if f.Weekend != nil {
		db = db.Where("is_weekend = ?", *f.Weekend)
	}
	if f.Weather != "" {
		db = db.Where("weather = ?", f.Weather)
	}
	if f.Days > 0 {
		db = db.Where("played_at >= ?", time.Now().UTC().AddDate(0, 0, -f.Days))
	}
	return db
}

// Count is a labelled tally used throughout the profile output.
type Count struct {
	Label string `json:"label"`
	N     int    `json:"plays"`
}

// ProfileResult is the summary returned to the AI.
type ProfileResult struct {
	TotalPlays        int                `json:"total_plays"`
	Filter            map[string]any     `json:"filter,omitempty"`
	TopGenres         []Count            `json:"top_genres,omitempty"`
	TopArtists        []Count            `json:"top_artists,omitempty"`
	TopTracks         []Count            `json:"top_tracks,omitempty"`
	GenresByPartOfDay map[string][]Count `json:"top_genres_by_part_of_day,omitempty"`
	Note              string             `json:"note,omitempty"`
}

func (s *Store) topGenres(f Filter, limit int) []Count {
	var rows []Count
	s.db.Table("play_events, jsonb_array_elements_text(genres) AS g").
		Select("g AS label, count(*) AS n").
		Scopes(f.scope).
		Group("g").Order("n DESC").Limit(limit).Scan(&rows)
	return rows
}

func (s *Store) topArtists(f Filter, limit int) []Count {
	var rows []Count
	s.db.Model(&PlayEvent{}).
		Select("artist_name AS label, count(*) AS n").
		Scopes(f.scope).Where("artist_name <> ''").
		Group("artist_name").Order("n DESC").Limit(limit).Scan(&rows)
	return rows
}

func (s *Store) topTracks(f Filter, limit int) []Count {
	var rows []Count
	s.db.Model(&PlayEvent{}).
		Select("track_name || ' — ' || artist_name AS label, count(*) AS n").
		Scopes(f.scope).Where("track_name <> ''").
		Group("track_name, artist_name").Order("n DESC").Limit(limit).Scan(&rows)
	return rows
}

func (s *Store) count(f Filter) int {
	var n int64
	s.db.Model(&PlayEvent{}).Scopes(f.scope).Count(&n)
	return int(n)
}

// Profile builds an aggregated listening profile for the given filter. When no
// part-of-day filter is set it also includes a per-part-of-day genre breakdown,
// so the AI sees the daily rhythm in one call.
func (s *Store) Profile(f Filter) ProfileResult {
	res := ProfileResult{TotalPlays: s.count(f), Filter: f.describe()}
	if res.TotalPlays == 0 {
		res.Note = "No listening data recorded for this slice yet. The profile builds up passively as the user listens; it needs a few days of history to be meaningful."
		return res
	}
	res.TopGenres = s.topGenres(f, 12)
	res.TopArtists = s.topArtists(f, 10)
	res.TopTracks = s.topTracks(f, 10)
	if f.PartOfDay == "" {
		res.GenresByPartOfDay = map[string][]Count{}
		for _, part := range []string{"morning", "afternoon", "evening", "night"} {
			pf := f
			pf.PartOfDay = part
			if g := s.topGenres(pf, 5); len(g) > 0 {
				res.GenresByPartOfDay[part] = g
			}
		}
	}
	return res
}

// ProfileJSON is the adapter the agent's built-in tool calls: it runs Profile
// and returns its JSON.
func (s *Store) ProfileJSON(partOfDay string, weekend *bool, weather string, days int) (string, error) {
	res := s.Profile(Filter{PartOfDay: partOfDay, Weekend: weekend, Weather: weather, Days: days})
	data, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (f Filter) describe() map[string]any {
	m := map[string]any{}
	if f.PartOfDay != "" {
		m["part_of_day"] = f.PartOfDay
	}
	if f.Weekend != nil {
		m["weekend"] = *f.Weekend
	}
	if f.Weather != "" {
		m["weather"] = f.Weather
	}
	if f.Days > 0 {
		m["days"] = f.Days
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
