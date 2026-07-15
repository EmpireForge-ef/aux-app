package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
	s.runsMu.Lock()
	_, running := s.runs[r.PathValue("id")]
	s.runsMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"meta": c.Meta, "transcript": c.Transcript, "running": running})
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

// errTurnRunning signals that a chat already has a turn in flight.
var errTurnRunning = errors.New("a turn is already running for this chat")

// handleChat starts one user turn in a persisted chat and streams the agent's
// response as server-sent events (event names mirror ai.Event.Type: user,
// text, tool_use, tool_result, confirm, done, error). Crucially the turn runs
// on a background goroutine that is NOT tied to this request, so it keeps going
// — and is persisted — even if the client disconnects (a phone browser being
// backgrounded). The client reconnects via GET /api/chat/stream to resume, and
// POST /api/chat/stop cancels the turn.
func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID  string `json:"chat_id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" || req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id and message are required"})
		return
	}

	rn, err := s.startTurn(req.ChatID, req.Message)
	if err != nil {
		if errors.Is(err, errTurnRunning) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.streamRun(w, r, rn, 0)
}

// handleChatStream reattaches to an in-flight turn and streams its buffered
// then live events from index `from`, so a client that dropped (mobile
// backgrounding, a reload) can pick the turn back up. 204 means no turn is
// running, and the client should fall back to the saved transcript.
func (s *server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id is required"})
		return
	}
	from := 0
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			from = n
		}
	}
	s.runsMu.Lock()
	rn := s.runs[chatID]
	s.runsMu.Unlock()
	if rn == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.streamRun(w, r, rn, from)
}

// handleChatStop cancels the in-flight turn for a chat (the Stop button). The
// turn stops at its next cancellation point, persists whatever it already did,
// and emits a "stopped" done event.
func (s *server) handleChatStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID string `json:"chat_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id is required"})
		return
	}
	s.runsMu.Lock()
	rn := s.runs[req.ChatID]
	s.runsMu.Unlock()
	if rn != nil {
		rn.cancel()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// startTurn registers a new in-flight turn for a chat and launches the
// background goroutine that runs it. It fails with errTurnRunning if one is
// already running for that chat.
func (s *server) startTurn(chatID, message string) (*run, error) {
	s.runsMu.Lock()
	if _, ok := s.runs[chatID]; ok {
		s.runsMu.Unlock()
		return nil, errTurnRunning
	}
	// Detached from any request context so the turn survives client
	// disconnects; only Stop (rn.cancel) ends it early.
	ctx, cancel := context.WithCancel(context.Background())
	rn := newRun(cancel)
	s.runs[chatID] = rn
	s.runsMu.Unlock()

	go s.runTurn(ctx, chatID, message, rn)
	return rn, nil
}

// streamRun writes a run's events to an SSE response until the run finishes or
// the client disconnects. On disconnect the run keeps executing server-side.
func (s *server) streamRun(w http.ResponseWriter, r *http.Request, rn *run, from int) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	send := func(ev ai.Event) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return nil // skip an unmarshalable event rather than abort
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	_ = rn.stream(r.Context(), from, send)
}

// runTurn executes one turn end to end on a background goroutine: it loads the
// chat, runs the agent while buffering events into rn, and persists the result
// — even if no client is connected. The per-chat lock serialises it against
// renames and further turns.
func (s *server) runTurn(ctx context.Context, chatID, message string, rn *run) {
	defer func() {
		rn.finish()
		s.runsMu.Lock()
		delete(s.runs, chatID)
		s.runsMu.Unlock()
		rn.cancel() // release the context
	}()

	lock := s.chats.Lock(chatID)
	lock.Lock()
	defer lock.Unlock()

	c, err := s.chats.Get(chatID)
	if err != nil {
		rn.append(ai.Event{Type: "error", Message: "loading the chat failed: " + err.Error()})
		return
	}

	// Echo the user's message as the first event so a client that reconnects
	// or reloads and replays the buffer reconstructs the whole turn (the
	// message isn't persisted to the transcript until the turn is saved).
	rn.append(ai.Event{Type: "user", Text: message})

	// The transcript mirrors what the emit events render in the UI: text
	// deltas coalesce into one assistant entry, tool chips resolve in place.
	if len(c.Messages) == 0 {
		c.Title = chat.TitleFrom(message)
	}
	c.Transcript = append(c.Transcript, chat.Entry{Role: "user", Text: message})
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
		rn.append(ev)
	}

	mgr, agent := s.clients()
	sp, _ := mgr.Client() // nil is fine; tools explain how to connect

	// confirm gates destructive tools: it emits a confirm event, then blocks
	// until POST /api/chat/confirm delivers the user's decision (or the turn
	// is stopped or the confirmation times out — both decline). markResolved
	// lets a from-scratch replay skip re-prompting for an answered action.
	confirm := func(ctx context.Context, cr ai.ConfirmRequest) bool {
		id, err := randomToken()
		if err != nil {
			return false
		}
		ch := make(chan bool, 1)
		s.addConfirm(id, ch)
		defer s.removeConfirm(id)
		defer rn.markResolved(id)

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

	now := time.Now()
	if loc := s.turnLocation(); loc != nil {
		now = now.In(loc)
	}

	// Tool handlers reach the temp-playlist registry and playlist cache
	// through the context.
	turnCtx := aitools.WithTempPlaylists(ctx, s.temps)
	turnCtx = aitools.WithPlaylistCache(turnCtx, s.plcache)
	turnCtx = aitools.WithWeekdayQueues(turnCtx, s.temps)
	turnCtx = aitools.WithLocalNow(turnCtx, now)
	messages, chatErr := agent.Chat(turnCtx, c.Messages, message, sp, emit, ai.TurnOptions{
		Confirm:        confirm,
		Memory:         s.prefs,
		History:        s.history,
		Listening:      s.listening,
		LearnedProfile: s.listening.LearnedProfile(),
		Weather:        s.currentWeather(ctx),
		Now:            now,
		// Skip the confirmation prompt for edits to throwaway temp playlists.
		SkipConfirm: func(name string, input json.RawMessage) bool {
			return aitools.IsTempPlaylistEdit(s.temps, name, input)
		},
	})
	if chatErr != nil {
		if errors.Is(chatErr, context.Canceled) {
			flushText()
			emit(ai.Event{Type: "done", StopReason: "stopped"})
		} else {
			slog.Error("chat turn failed", "chat", chatID, "err", chatErr)
			emit(ai.Event{Type: "error", Message: chatErr.Error()})
		}
	}
	flushText()

	c.Messages = messages
	if err := s.chats.Save(c); err != nil {
		slog.Error("persist chat failed", "chat", chatID, "err", err)
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
