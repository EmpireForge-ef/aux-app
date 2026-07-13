package playlistcache

import (
	"path/filepath"
	"testing"
)

func TestSnapshotValidatedCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	s, _ := Load(path)

	s.Store("pl1", "snapA", []string{"a", "b", "a"}) // dups collapsed
	if uris, ok := s.Contents("pl1", "snapA"); !ok || len(uris) != 2 {
		t.Fatalf("contents = %v ok=%v, want 2 uris", uris, ok)
	}
	// A different snapshot means the playlist changed -> miss.
	if _, ok := s.Contents("pl1", "snapB"); ok {
		t.Error("snapshot mismatch should be a miss")
	}

	// Re-store under a new snapshot; reload from disk to confirm persistence.
	s.Store("pl1", "snapB", []string{"b", "c"})
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if uris, ok := s2.Contents("pl1", "snapB"); !ok || len(uris) != 2 {
		t.Errorf("after re-store: uris = %v ok = %v, want 2", uris, ok)
	}
	if _, ok := s2.Contents("pl1", "snapA"); ok {
		t.Error("old snapshot should no longer hit")
	}

	// Invalidate drops the entry.
	s2.Invalidate("pl1")
	if _, ok := s2.Contents("pl1", "snapB"); ok {
		t.Error("invalidated entry should be a miss")
	}
}

func TestEviction(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "c.json"))
	for i := 0; i < maxPlaylists+10; i++ {
		s.Store(string(rune('a'+i%26))+string(rune('0'+i/26)), "s", []string{"u"})
	}
	if len(s.entries) > maxPlaylists {
		t.Errorf("cache holds %d entries, want <= %d", len(s.entries), maxPlaylists)
	}
}
