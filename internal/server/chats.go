package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/EmpireForge-ef/aux-app/internal/ai"
	"github.com/EmpireForge-ef/aux-app/internal/aitools"
	"github.com/EmpireForge-ef/aux-app/internal/chat"
)

func (s *server) handleListChats(w http.ResponseWriter, r *http.Request) {
	metas, err := s.chats.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": metas})
}

func (s *server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	c, err := s.chats.Create()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c.Meta)
}

func (s *server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	c, err := s.chats.Get(r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, chat.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meta": c.Meta, "transcript": c.Transcript})
}

func (s *server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	if err := s.chats.Delete(r.PathValue("id")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, chat.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRenameChat sets a chat's title.
func (s *server) handleRenameChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	title := strings.Join(strings.Fields(req.Title), " ")
	if title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title must not be empty"})
		return
	}
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}

	// Serialise with any in-flight message turn on the same chat.
	lock := s.chats.Lock(id)
	lock.Lock()
	defer lock.Unlock()

	meta, err := s.chats.Rename(id, title)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, chat.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// handleChat runs one user turn in a persisted chat and streams the agent's
// response as server-sent events (event names mirror ai.Event.Type: text,
// tool_use, tool_result, done, error). The updated conversation — model
// history and display transcript — is persisted afterwards, also when the
// turn fails partway.
func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID  string `json:"chat_id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" || req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id and message are required"})
		return
	}

	// Serialise turns per chat so concurrent requests can't interleave
	// histories.
	lock := s.chats.Lock(req.ChatID)
	lock.Lock()
	defer lock.Unlock()

	c, err := s.chats.Get(req.ChatID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, chat.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	// The transcript mirrors what the emit events render in the UI: text
	// deltas coalesce into one assistant entry, tool chips resolve in place.
	if len(c.Messages) == 0 {
		c.Title = chat.TitleFrom(req.Message)
	}
	c.Transcript = append(c.Transcript, chat.Entry{Role: "user", Text: req.Message})
	var textBuf strings.Builder
	flushText := func() {
		if textBuf.Len() > 0 {
			c.Transcript = append(c.Transcript, chat.Entry{Role: "assistant", Text: textBuf.String()})
			textBuf.Reset()
		}
	}

	emit := func(ev ai.Event) {
		switch ev.Type {
		case "text":
			textBuf.WriteString(ev.Text)
		case "tool_use":
			flushText()
			c.Transcript = append(c.Transcript, chat.Entry{Role: "tool", Name: ev.Name})
		case "tool_result":
			for i := len(c.Transcript) - 1; i >= 0; i-- {
				if c.Transcript[i].Role == "tool" && c.Transcript[i].OK == nil {
					c.Transcript[i].OK = ev.OK
					break
				}
			}
		case "error":
			flushText()
			c.Transcript = append(c.Transcript, chat.Entry{Role: "error", Text: ev.Message})
		}
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
		flusher.Flush()
	}

	mgr, agent := s.clients()
	sp, _ := mgr.Client() // nil is fine; tools explain how to connect

	// confirm gates destructive tools: it emits a confirm event, then blocks
	// until POST /api/chat/confirm delivers the user's decision (or the
	// request is cancelled or the confirmation times out — both decline).
	confirm := func(ctx context.Context, cr ai.ConfirmRequest) bool {
		id, err := randomToken()
		if err != nil {
			return false
		}
		ch := make(chan bool, 1)
		s.addConfirm(id, ch)
		defer s.removeConfirm(id)

		emit(ai.Event{Type: "confirm", ConfirmID: id, Name: cr.Name, Input: cr.Input, Summary: cr.Question})

		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Minute):
			return false
		case approved := <-ch:
			return approved
		}
	}

	// Tool handlers reach the temp-playlist registry and playlist cache
	// through the context.
	turnCtx := aitools.WithTempPlaylists(r.Context(), s.temps)
	turnCtx = aitools.WithPlaylistCache(turnCtx, s.plcache)
	messages, chatErr := agent.Chat(turnCtx, c.Messages, req.Message, sp, emit, ai.TurnOptions{
		Confirm: confirm,
		Memory:  s.prefs,
		History: s.history,
		// Skip the confirmation prompt for edits to throwaway temp playlists.
		SkipConfirm: func(name string, input json.RawMessage) bool {
			return aitools.IsTempPlaylistEdit(s.temps, name, input)
		},
	})
	if chatErr != nil {
		log.Printf("chat error (chat %s): %v", req.ChatID, chatErr)
		emit(ai.Event{Type: "error", Message: chatErr.Error()})
	}
	flushText()

	c.Messages = messages
	if err := s.chats.Save(c); err != nil {
		log.Printf("persist chat %s failed: %v", req.ChatID, err)
		emit(ai.Event{Type: "error", Message: "saving the conversation failed: " + err.Error()})
	}
}

// handleChatConfirm delivers the user's approve/deny decision for a pending
// destructive action back to the waiting chat turn.
func (s *server) handleChatConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConfirmID string `json:"confirm_id"`
		Approved  bool   `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfirmID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm_id is required"})
		return
	}
	if !s.resolveConfirm(req.ConfirmID, req.Approved) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no pending confirmation (it may have expired)"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
