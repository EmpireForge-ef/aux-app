// Package chat persists conversations so users can leave and return to them.
// Each chat holds both the model-facing message history (replayed to the
// Anthropic API when the chat continues) and a display transcript for
// re-rendering the UI. Storage is PostgreSQL via GORM; the message history and
// transcript are stored as JSONB columns.
package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a chat ID does not exist.
var ErrNotFound = errors.New("chat not found")

// Entry is one display-transcript element: a user or assistant message, or a
// tool invocation chip.
type Entry struct {
	Role string `json:"role"` // user | assistant | tool | error
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"` // tool name, for role "tool"
	OK   *bool  `json:"ok,omitempty"`   // tool outcome, for role "tool"
}

// Meta is the listing view of a chat.
type Meta struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime:false;index"`
}

// Chat is a full persisted conversation. It is also the GORM model (table
// "chats"); Messages and Transcript are stored as JSONB.
type Chat struct {
	Meta
	// Messages is the exact Anthropic message history, replayed when the
	// conversation continues.
	Messages []anthropic.MessageParam `json:"messages" gorm:"serializer:json;type:jsonb"`
	// Transcript is what the user saw, for re-rendering old chats.
	Transcript []Entry `json:"transcript" gorm:"serializer:json;type:jsonb"`
}

// TableName pins the table name (GORM would otherwise not pluralise "Chat"
// through the embedded Meta cleanly across versions).
func (Chat) TableName() string { return "chats" }

// Store persists chats in PostgreSQL. It serialises turns per chat in memory so
// concurrent messages to the same conversation don't interleave.
type Store struct {
	db *gorm.DB

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewStore migrates the chats table and returns a store.
func NewStore(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&Chat{}); err != nil {
		return nil, fmt.Errorf("migrate chats: %w", err)
	}
	return &Store{db: db, locks: make(map[string]*sync.Mutex)}, nil
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

func newID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Create makes a new empty chat.
func (s *Store) Create() (*Chat, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	c := &Chat{Meta: Meta{ID: id, Title: "New chat", CreatedAt: now, UpdatedAt: now}}
	if err := s.db.Create(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}

// Get loads a chat by ID.
func (s *Store) Get(id string) (*Chat, error) {
	var c Chat
	err := s.db.First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Save persists a chat, bumping its UpdatedAt.
func (s *Store) Save(c *Chat) error {
	c.UpdatedAt = time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = c.UpdatedAt
	}
	return s.db.Save(c).Error
}

// Rename changes a chat's title, preserving its position in the list (it does
// not bump UpdatedAt). Returns the updated metadata.
func (s *Store) Rename(id, title string) (Meta, error) {
	res := s.db.Model(&Chat{}).Where("id = ?", id).UpdateColumn("title", title)
	if res.Error != nil {
		return Meta{}, res.Error
	}
	if res.RowsAffected == 0 {
		return Meta{}, ErrNotFound
	}
	c, err := s.Get(id)
	if err != nil {
		return Meta{}, err
	}
	return c.Meta, nil
}

// Delete removes a chat.
func (s *Store) Delete(id string) error {
	res := s.db.Delete(&Chat{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.mu.Lock()
	delete(s.locks, id)
	s.mu.Unlock()
	return nil
}

// List returns all chats' metadata, most recently updated first. It selects
// only the metadata columns, not the (large) JSONB history.
func (s *Store) List() ([]Meta, error) {
	var metas []Meta
	err := s.db.Model(&Chat{}).
		Select("id", "title", "created_at", "updated_at").
		Order("updated_at desc").
		Find(&metas).Error
	if err != nil {
		return nil, err
	}
	return metas, nil
}

// ImportDir loads any pre-database chat JSON files from dir into the table,
// skipping IDs that already exist. Best-effort: unreadable files are skipped.
// Returns how many chats were imported.
func (s *Store) ImportDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	imported := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c Chat
		if err := json.Unmarshal(data, &c); err != nil || c.ID == "" {
			continue
		}
		var count int64
		s.db.Model(&Chat{}).Where("id = ?", c.ID).Count(&count)
		if count > 0 {
			continue
		}
		if c.CreatedAt.IsZero() {
			c.CreatedAt = time.Now().UTC()
		}
		if c.UpdatedAt.IsZero() {
			c.UpdatedAt = c.CreatedAt
		}
		if err := s.db.Create(&c).Error; err == nil {
			imported++
		}
	}
	return imported, nil
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
