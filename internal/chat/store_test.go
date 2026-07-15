package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/EmpireForge-ef/aux-app/internal/dbtest"
)

func newStore(t *testing.T) *Store {
	gdb := dbtest.Open(t)
	s, err := NewStore(gdb)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	dbtest.Truncate(t, gdb, "chats")
	return s
}

func TestChatRoundTrip(t *testing.T) {
	s := newStore(t)
	c, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Title != "New chat" {
		t.Fatalf("unexpected new chat: %+v", c.Meta)
	}

	c.Title = TitleFrom("Make me a 90s playlist")
	c.Messages = append(c.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock("hi")))
	c.Transcript = append(c.Transcript, Entry{Role: "user", Text: "hi"})
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Make me a 90s playlist" || len(got.Messages) != 1 || len(got.Transcript) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if _, err := s.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestRenameDoesNotBumpUpdatedAt(t *testing.T) {
	s := newStore(t)
	c, _ := s.Create()
	// Backdate so a bump would be detectable.
	old := time.Now().UTC().Add(-time.Hour)
	if err := s.db.Model(&Chat{}).Where("id = ?", c.ID).UpdateColumn("updated_at", old).Error; err != nil {
		t.Fatal(err)
	}
	meta, err := s.Rename(c.ID, "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Renamed" {
		t.Errorf("title = %q", meta.Title)
	}
	if meta.UpdatedAt.After(old.Add(time.Minute)) {
		t.Errorf("Rename bumped UpdatedAt: %v", meta.UpdatedAt)
	}
}

func TestListOrderAndDelete(t *testing.T) {
	s := newStore(t)
	a, _ := s.Create()
	time.Sleep(5 * time.Millisecond)
	b, _ := s.Create()
	// Save b so it becomes most-recently-updated.
	_ = s.Save(b)

	metas, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 || metas[0].ID != b.ID {
		t.Fatalf("List order wrong: %+v", metas)
	}

	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	metas, _ = s.List()
	if len(metas) != 1 {
		t.Errorf("after delete, %d chats remain", len(metas))
	}
}
