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
	dbtest.Truncate(t, gdb, "temp_playlists", "weekday_queues")
	return s
}

func TestWeekdayQueue(t *testing.T) {
	s := newStore(t)
	if _, _, ok := s.WeekdayQueue(1); ok {
		t.Fatal("should be empty before set")
	}
	s.SetWeekdayQueue(1, "pl-mon", "Aux Queue · Monday", "2026-07-13")
	id, last, ok := s.WeekdayQueue(1)
	if !ok || id != "pl-mon" || last != "2026-07-13" {
		t.Fatalf("get = %q %q %v", id, last, ok)
	}
	// Marking used only updates the date, not the playlist.
	s.MarkWeekdayUsed(1, "2026-07-20")
	id, last, _ = s.WeekdayQueue(1)
	if id != "pl-mon" || last != "2026-07-20" {
		t.Errorf("after mark: %q %q", id, last)
	}
	// A different weekday is independent.
	if _, _, ok := s.WeekdayQueue(2); ok {
		t.Error("weekday 2 should be independent/empty")
	}
	// Re-setting replaces the playlist (e.g. after a delete + recreate).
	s.SetWeekdayQueue(1, "pl-mon-2", "Aux Queue · Monday", "2026-07-27")
	if id, _, _ := s.WeekdayQueue(1); id != "pl-mon-2" {
		t.Errorf("re-set id = %q", id)
	}
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
