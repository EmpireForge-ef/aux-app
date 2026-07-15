package listening

import (
	"testing"
	"time"

	"github.com/EmpireForge-ef/aux-app/internal/dbtest"
)

func newStore(t *testing.T) *Store {
	gdb := dbtest.Open(t)
	s, err := New(gdb)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	dbtest.Truncate(t, gdb, "play_events", "artist_genres", "listening_poll_state")
	return s
}

func ev(playedAt time.Time, uri, track, artist string, genres []string, part string, weekend bool, wx string) PlayEvent {
	return PlayEvent{
		PlayedAt: playedAt, TrackURI: uri, TrackName: track,
		ArtistURI: "spotify:artist:" + artist, ArtistName: artist,
		Genres: genres, PartOfDay: part, IsWeekend: weekend, Weather: wx,
	}
}

func TestInsertDedupAndProfile(t *testing.T) {
	s := newStore(t)
	base := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	events := []PlayEvent{
		ev(base, "spotify:track:1", "Song A", "Slowdive", []string{"shoegaze", "dream pop"}, "morning", false, "rain"),
		ev(base.Add(time.Minute), "spotify:track:2", "Song B", "Ride", []string{"shoegaze"}, "morning", false, "rain"),
		ev(base.Add(2*time.Minute), "spotify:track:3", "Song C", "Daft Punk", []string{"house"}, "evening", true, "clear"),
	}
	if n := s.Insert(events); n != 3 {
		t.Fatalf("first insert = %d, want 3", n)
	}
	// Re-inserting the same events is idempotent.
	if n := s.Insert(events); n != 0 {
		t.Errorf("duplicate insert = %d, want 0", n)
	}

	all := s.Profile(Filter{})
	if all.TotalPlays != 3 {
		t.Fatalf("total = %d, want 3", all.TotalPlays)
	}
	if len(all.TopGenres) == 0 || all.TopGenres[0].Label != "shoegaze" || all.TopGenres[0].N != 2 {
		t.Errorf("top genre = %+v, want shoegaze x2", all.TopGenres)
	}
	// Per-part-of-day breakdown present when no part filter.
	if len(all.GenresByPartOfDay["morning"]) == 0 {
		t.Errorf("expected a morning genre breakdown: %+v", all.GenresByPartOfDay)
	}

	// Filter to mornings: only shoegaze, no house.
	morning := s.Profile(Filter{PartOfDay: "morning"})
	if morning.TotalPlays != 2 {
		t.Errorf("morning total = %d, want 2", morning.TotalPlays)
	}
	for _, g := range morning.TopGenres {
		if g.Label == "house" {
			t.Errorf("house should not appear in the morning slice")
		}
	}

	// Weather filter.
	rainy := s.Profile(Filter{Weather: "rain"})
	if rainy.TotalPlays != 2 {
		t.Errorf("rainy total = %d, want 2", rainy.TotalPlays)
	}

	// Empty slice returns a helpful note.
	none := s.Profile(Filter{PartOfDay: "night"})
	if none.TotalPlays != 0 || none.Note == "" {
		t.Errorf("empty slice = %+v, want a note", none)
	}
}

func TestGenreCacheAndCursor(t *testing.T) {
	s := newStore(t)
	if _, ok := s.CachedGenres("spotify:artist:x"); ok {
		t.Error("should miss before caching")
	}
	s.CacheGenres("spotify:artist:x", []string{"jazz"})
	if g, ok := s.CachedGenres("spotify:artist:x"); !ok || len(g) != 1 || g[0] != "jazz" {
		t.Errorf("cache = %v ok=%v", g, ok)
	}

	if !s.LastPlayedAt().IsZero() {
		t.Error("cursor should start zero")
	}
	when := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	s.SetLastPlayedAt(when)
	if !s.LastPlayedAt().Equal(when) {
		t.Errorf("cursor = %v, want %v", s.LastPlayedAt(), when)
	}
}

func TestPartOfDay(t *testing.T) {
	cases := map[int]string{0: "night", 5: "night", 6: "morning", 11: "morning", 12: "afternoon", 17: "afternoon", 18: "evening", 23: "evening"}
	for h, want := range cases {
		if got := PartOfDay(h); got != want {
			t.Errorf("PartOfDay(%d) = %q, want %q", h, got, want)
		}
	}
}
