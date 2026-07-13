// Package chat persists conversations so users can leave and return to
// them. Each chat is one JSON file holding both the model-facing message
// history (replayed to the Anthropic API when the chat continues) and a
// display transcript for re-rendering the UI.
package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
)

// ErrNotFound is returned when a chat ID does not exist.
var ErrNotFound = errors.New("chat not found")

// Entry is one display-transcript element: a user or assistant message, or
// a tool invocation chip.
type Entry struct {
	Role string `json:"role"` // user | assistant | tool | error
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name, for role "tool"
	OK   *bool  `json:"ok,omitempty"`   // tool outcome, for role "tool"
}

// Meta is the listing view of a chat.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Chat is a full persisted conversation.
type Chat struct {
	Meta
	// Messages is the exact Anthropic message history, replayed when the
	// conversation continues.
	Messages []anthropic.MessageParam `json:"messages"`
	// Transcript is what the user saw, for re-rendering old chats.
	Transcript []Entry `json:"transcript"`
}

// Store keeps one JSON file per chat in a directory. It serialises access
// per chat so concurrent messages to the same conversation don't interleave.
type Store struct {
	dir string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewStore initialises the chat directory.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create chats dir: %w", err)
	}
	return &Store{dir: dir, locks: make(map[string]*sync.Mutex)}, nil
}

// Lock returns the mutex serialising operations on one chat.
func (s *Store) Lock(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	return l
}

func (s *Store) path(id string) (string, error) {
	// IDs are generated as hex; reject anything else so a crafted ID can
	// never traverse out of the chat directory.
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return "", ErrNotFound
	}
	return filepath.Join(s.dir, id+".json"), nil
}

// Create makes a new empty chat.
func (s *Store) Create() (*Chat, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	c := &Chat{Meta: Meta{
		ID:        hex.EncodeToString(buf),
		Title:     "New chat",
		CreatedAt: now,
		UpdatedAt: now,
	}}
	if err := s.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Get loads a chat by ID.
func (s *Store) Get(id string) (*Chat, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var c Chat
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse chat %s: %w", id, err)
	}
	return &c, nil
}

// Save persists a chat, bumping its UpdatedAt.
func (s *Store) Save(c *Chat) error {
	p, err := s.path(c.ID)
	if err != nil {
		return err
	}
	c.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Delete removes a chat.
func (s *Store) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
	return nil
}

// List returns all chats' metadata, most recently updated first.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	metas := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		c, err := s.Get(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue // skip unreadable files rather than failing the listing
		}
		metas = append(metas, c.Meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].UpdatedAt.After(metas[j].UpdatedAt) })
	return metas, nil
}

// TitleFrom derives a chat title from the first user message.
func TitleFrom(message string) string {
	title := strings.Join(strings.Fields(message), " ")
	const max = 60
	if utf8.RuneCountInString(title) > max {
		runes := []rune(title)
		title = string(runes[:max-1]) + "…"
	}
	if title == "" {
		return "New chat"
	}
	return title
}
