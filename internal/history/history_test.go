package history

import (
	"path/filepath"
	"testing"
)

func TestHistoryRecencyDedupPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hist.json")
	s, _ := Load(path)
	s.Add([]string{"a", "b"})
	s.Add([]string{"c"})
	s.Add([]string{"a"}) // re-adding 'a' moves it to most-recent

	// Reload to confirm persistence.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Recent(10) // newest first
	want := []string{"a", "c", "b"}
	if len(got) != len(want) {
		t.Fatalf("recent = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("recent[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if r := s2.Recent(1); len(r) != 1 || r[0] != "a" {
		t.Errorf("Recent(1) = %v, want [a]", r)
	}
}
