package history

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
	dbtest.Truncate(t, gdb, "history_entries")
	return s
}

func TestAddRecencyAndDedup(t *testing.T) {
	s := newStore(t)
	s.Add([]string{"a", "b", "c"})
	s.Add([]string{"b"}) // b moves to the most-recent end

	got := s.Recent(10)
	want := []string{"b", "c", "a"} // newest first
	if len(got) != len(want) {
		t.Fatalf("Recent = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Recent = %v, want %v", got, want)
		}
	}
	// Recent(n) limits.
	if two := s.Recent(2); len(two) != 2 || two[0] != "b" {
		t.Errorf("Recent(2) = %v", two)
	}
}

func TestTrimToCap(t *testing.T) {
	s := newStore(t)
	batch := make([]string, maxEntries+50)
	for i := range batch {
		batch[i] = "u" + string(rune('0'+i%10)) + string(rune('a'+i/10%26)) + string(rune('A'+i/260))
	}
	s.Add(batch)
	if got := len(s.Recent(0)); got > maxEntries {
		t.Errorf("kept %d entries, want <= %d", got, maxEntries)
	}
}
