package preferences

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
	dbtest.Truncate(t, gdb, "preferences")
	return s
}

func TestSetListText(t *testing.T) {
	s := newStore(t)
	if err := s.Set("Genres", "  synthwave, lo-fi "); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("avoid", "explicit"); err != nil {
		t.Fatal(err)
	}
	got := s.List()
	if got["genres"] != "synthwave, lo-fi" { // key lower-cased, value trimmed
		t.Errorf("genres = %q", got["genres"])
	}
	if txt := s.Text(); txt == "" || txt[0] != '-' {
		t.Errorf("Text() = %q", txt)
	}
	// Empty value deletes.
	if err := s.Set("avoid", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.List()["avoid"]; ok {
		t.Error("avoid should be deleted")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Error("Clear should empty preferences")
	}
}

func TestMaxKeys(t *testing.T) {
	s := newStore(t)
	for i := 0; i < maxKeys; i++ {
		if err := s.Set(string(rune('a'+i%26))+string(rune('0'+i/26)), "v"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set("one-too-many", "v"); err == nil {
		t.Error("expected an error past maxKeys")
	}
	// Updating an existing key is still allowed at the cap.
	if err := s.Set("a0", "changed"); err != nil {
		t.Errorf("updating an existing key at cap should work: %v", err)
	}
}
