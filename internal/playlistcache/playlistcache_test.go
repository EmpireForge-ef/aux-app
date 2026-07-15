package playlistcache

import (
	"testing"

	"github.com/EmpireForge-ef/aux-app/internal/dbtest"
)

func newStore(t *testing.T) *Store {
	gdb := dbtest.Open(t)
	s, err := New(gdb)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	dbtest.Truncate(t, gdb, "playlist_cache")
	return s
}

func TestSnapshotValidatedCache(t *testing.T) {
	s := newStore(t)
	s.Store("pl1", "snapA", []string{"a", "b", "a"}) // dups collapsed
	if uris, ok := s.Contents("pl1", "snapA"); !ok || len(uris) != 2 {
		t.Fatalf("contents = %v ok=%v, want 2 uris", uris, ok)
	}
	// A different snapshot means the playlist changed -> miss.
	if _, ok := s.Contents("pl1", "snapB"); ok {
		t.Error("snapshot mismatch should be a miss")
	}
	// Re-store under a new snapshot replaces the entry.
	s.Store("pl1", "snapB", []string{"b", "c"})
	if uris, ok := s.Contents("pl1", "snapB"); !ok || len(uris) != 2 {
		t.Errorf("after re-store: uris=%v ok=%v", uris, ok)
	}
	// Invalidate drops it.
	s.Invalidate("pl1")
	if _, ok := s.Contents("pl1", "snapB"); ok {
		t.Error("invalidated entry should be a miss")
	}
}

func TestEviction(t *testing.T) {
	s := newStore(t)
	for i := 0; i < maxPlaylists+10; i++ {
		id := "p" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		s.Store(id, "s", []string{"u"})
	}
	var count int64
	s.db.Model(&cacheRow{}).Count(&count)
	if count > maxPlaylists {
		t.Errorf("cache holds %d entries, want <= %d", count, maxPlaylists)
	}
}
