package preferences

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Text() != "" {
		t.Error("empty store should render empty text")
	}
	if err := s.Set("Genres", " synthwave, lo-fi "); err != nil {
		t.Fatal(err)
	}
	_ = s.Set("avoid", "explicit lyrics")

	// Reload from disk to confirm persistence + lowercased/trimmed keys.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.List()["genres"]; got != "synthwave, lo-fi" {
		t.Errorf("genres = %q", got)
	}
	if !strings.Contains(s2.Text(), "avoid: explicit lyrics") {
		t.Errorf("text = %q", s2.Text())
	}
	// Empty value deletes.
	_ = s2.Set("avoid", "")
	if _, ok := s2.List()["avoid"]; ok {
		t.Error("empty value should delete the key")
	}
	_ = s2.Clear()
	if len(s2.List()) != 0 {
		t.Error("clear should empty the store")
	}
}
