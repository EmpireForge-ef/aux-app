package aitools

import (
	"encoding/json"
	"strings"
	"testing"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func TestSlimTrackDropsBulk(t *testing.T) {
	raw := &spotify.Track{
		SimplifiedTrack: spotify.SimplifiedTrack{
			Name:       "Bohemian Rhapsody",
			Artists:    []spotify.SimplifiedArtist{{Name: "Queen", ID: "q1"}},
			DurationMS: 354000,
			URI:        "spotify:track:abc",
			ID:         "abc",
		},
		Album:      spotify.SimplifiedAlbum{Name: "A Night at the Opera"},
		Popularity: 88,
	}
	out, err := json.Marshal(Slim(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"name":"Bohemian Rhapsody"`, `"artists":["Queen"]`, `"album":"A Night at the Opera"`, `"duration":"5:54"`} {
		if !strings.Contains(s, want) {
			t.Errorf("slim track missing %s in %s", want, s)
		}
	}
	// Bulk fields must be gone.
	if strings.Contains(s, "popularity") || strings.Contains(s, "external_urls") || strings.Contains(s, "available_markets") {
		t.Errorf("slim track still carries bulk fields: %s", s)
	}
}

func TestSlimPagingWrapsItems(t *testing.T) {
	p := &spotify.Paging[spotify.Track]{
		Items: []spotify.Track{{SimplifiedTrack: spotify.SimplifiedTrack{Name: "One"}}},
		Total: 42,
		Next:  "https://api.spotify.com/next",
	}
	out, _ := json.Marshal(Slim(p))
	s := string(out)
	if !strings.Contains(s, `"total":42`) || !strings.Contains(s, `"has_more":true`) || !strings.Contains(s, `"name":"One"`) {
		t.Errorf("slim paging = %s", s)
	}
	if strings.Contains(s, "api.spotify.com/next") {
		t.Errorf("slim paging should drop the next URL: %s", s)
	}
}
