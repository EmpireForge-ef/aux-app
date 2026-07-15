package tempplaylists

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
	dbtest.Truncate(t, gdb, "temp_playlists")
	return s
}

func TestAddHasRemove(t *testing.T) {
	s := newStore(t)
	if s.Has("p1") {
		t.Error("p1 should not be present yet")
	}
	s.Add("p1")
	s.Add("p1") // idempotent
	if !s.Has("p1") {
		t.Error("p1 should be present")
	}
	s.Remove("p1")
	if s.Has("p1") {
		t.Error("p1 should be removed")
	}
	s.Add("")
	if s.Has("") {
		t.Error("empty id should be ignored")
	}
}
