package tempplaylists

import (
	"path/filepath"
	"testing"
)

func TestTempPlaylistsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temp.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Has("p1") {
		t.Error("empty store should not contain anything")
	}
	s.Add("p1")
	s.Add("p1") // idempotent
	s.Add("p2")

	// Reload from disk to confirm persistence.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Has("p1") || !s2.Has("p2") {
		t.Error("added ids should persist")
	}
	s2.Remove("p1")
	if s2.Has("p1") {
		t.Error("removed id should be gone")
	}
	if !s2.Has("p2") {
		t.Error("p2 should remain")
	}
}
