// Package ai runs the Anthropic-powered agent loop: it streams model output,
// executes Spotify tool calls, and reports progress through events.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/EmpireForge-ef/aux-app/internal/aitools"
)

// maxTurns bounds the tool-use loop for a single user message.
const maxTurns = 40

// maxToolResultChars truncates oversized tool results so a single API listing
// cannot blow up the model context.
const maxToolResultChars = 50_000

const systemPrompt = `You are Aux, an AI assistant with full control over the user's Spotify account through tools.

You can search the catalog, inspect and edit playlists, manage the user's library (tracks, albums, shows, episodes, audiobooks), follow/unfollow, browse categories and new releases, and control playback on the user's devices.

Guidelines:
- Playlist items are addressed by Spotify URIs (spotify:track:..., spotify:episode:...); catalog lookups use bare IDs. Convert between them as needed.
- List endpoints are paged; fetch more pages with limit/offset when the user needs complete data.
- Playback tools require Spotify Premium and an active or named device; if playback fails, check available devices first.
- This app runs against Spotify's development-mode API (February 2026). Playlist contents are only readable for playlists the user owns or collaborates on — Spotify withholds the contents of other playlists, including its own editorial and algorithmic ones. Search returns at most 10 results per type per call (page with offset). Some catalog fields (track/artist popularity, user email/country) are withheld.
- A 403 Forbidden is an app-level restriction, never a scope problem — do not advise re-authorizing to fix one.
- Destructive actions (removing saved tracks/albums/episodes/shows/audiobooks, removing or replacing playlist items, unfollowing) are gated: the app automatically asks the user to approve each one before it runs, so you do not need to ask for confirmation in text — just call the tool and briefly state what you are doing. If the user declines, acknowledge it and move on. Non-destructive actions (adding items, creating playlists, saving) run without a prompt.
- Spotify no longer offers a recommendations endpoint to this app, so build "vibe" or "songs like X" playlists yourself: search by genre/mood/year, draw on the user's top and saved tracks and their followed/searched artists' catalogs, then dedupe. When you recommend, briefly say why a track fits ("because you listen to X").
- You have a small persistent memory of the user's music preferences via the remember_preference / list_preferences / forget_preference tools. When the user states a durable taste (favourite genres, artists to avoid, no explicit lyrics, preferred era), save it so future chats stay personalised. Their saved preferences are provided to you each turn — honour them unless the user overrides them for a specific request.
- The Spotify queue is add-only: once a track is queued it cannot be removed, reordered, or cleared. So when the user wants a queue they can edit (change songs, reorder, remove), do NOT use add_to_queue/add_tracks_to_queue — instead create_temp_playlist, add the tracks to it, and play it (play with context_uri set to the temp playlist's uri). Editing a temp playlist needs no confirmation. Reuse the temp playlist you already made in this conversation rather than creating a new one each time, and delete_temp_playlist when the user is done with it. Use add_to_queue/add_tracks_to_queue only for a simple fire-and-forget "play these next".
- Don't recommend the same songs every time. Each turn you are given a list of recently queued/added track URIs; when you generate a new selection (a vibe playlist, a queue, "more like this"), exclude those URIs and choose fresh tracks, so the user gets variety across requests. Only repeat a specific track if the user explicitly asks for it. Use list_recent_tracks to see a larger window when building a big selection. When you need more variety, widen the search (different artists, years, sub-genres) or draw on get_recently_played and the user's library.
- Adapt how strictly you avoid repeats to the user's taste, stored as the 'repeat_tolerance' preference: 'low' means they want constant novelty (avoid recent tracks strictly), 'high' means they enjoy hearing favourites again (repeating is fine, weight familiar tracks in). If it isn't set, infer it from feedback — e.g. "I keep hearing the same songs" means low tolerance, "play my favourites more" means high — and save it with remember_preference so it sticks.
- Be concise. After finishing a multi-step task, summarize what changed in one or two sentences.`

// Event is one server-sent event pushed to the frontend during a chat turn.
type Event struct {
	Type       string          `json:"type"` // text | tool_use | tool_result | confirm | done | error
	Text       string          `json:"text,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	OK         *bool           `json:"ok,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	ConfirmID  string          `json:"confirm_id,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	Message    string          `json:"message,omitempty"`
}

// ConfirmRequest describes a destructive tool call awaiting the user's
// approval.
type ConfirmRequest struct {
	Name     string
	Input    json.RawMessage
	Question string
}

// ConfirmFunc asks the user to approve a destructive action, returning true to
// proceed. It should honour ctx cancellation. A nil ConfirmFunc means no
// confirmation channel is available, and destructive actions are declined.
type ConfirmFunc func(ctx context.Context, req ConfirmRequest) bool

// Memory is a small persistent store of user preferences the agent can read
// (for personalisation) and update via built-in tools.
type Memory interface {
	Text() string                // preferences rendered for the prompt, or ""
	List() map[string]string     // all preferences
	Set(key, value string) error // empty value deletes; persists
	Clear() error                // remove everything
}

// History remembers recently queued/added track URIs so the agent can avoid
// recommending the same songs over and over.
type History interface {
	Recent(n int) []string // most-recently-added URIs, newest first
	Add(uris []string)     // record URIs as just used
}

// TurnOptions carries the per-turn extras beyond the conversation itself.
type TurnOptions struct {
	Confirm ConfirmFunc // gate for destructive tools
	Memory  Memory      // user preferences (nil disables the memory tools)
	Now     time.Time   // current time for context; zero means time.Now()
	History History     // recently-used tracks, to avoid repetition (nil disables)
	// SkipConfirm returns true when a destructive tool call should run without
	// asking the user — e.g. it edits a throwaway temp playlist.
	SkipConfirm func(name string, input json.RawMessage) bool
}

// recentInjectN is how many recently-used track URIs are shown to the model
// each turn so it can avoid repeating them.
const recentInjectN = 60

// Agent holds the Anthropic client and the Spotify tool registry. It is
// stateless: conversation history is passed in and returned by Chat, so the
// caller owns persistence.
type Agent struct {
	client     anthropic.Client
	model      anthropic.Model
	maxTokens  int64
	tools      map[string]aitools.Tool
	toolParams []anthropic.ToolUnionParam
}

func New(apiKey, model string, maxTokens int64) *Agent {
	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	registry := aitools.All()
	byName := make(map[string]aitools.Tool, len(registry))
	for _, t := range registry {
		byName[t.Name] = t
	}
	return &Agent{
		client:     anthropic.NewClient(opts...),
		model:      anthropic.Model(model),
		maxTokens:  maxTokens,
		tools:      byName,
		toolParams: append(append(aitools.Params(registry), memoryToolParams...), historyToolParams...),
	}
}

// historyToolParams is the built-in tool for querying the recently-used-track
// history on demand (beyond the compact list injected each turn).
var historyToolParams = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name:        "list_recent_tracks",
		Description: anthropic.String("Return the track URIs you recently queued or added (most recent first). You already receive the newest ones automatically each turn; call this to see a larger window when building a big selection and you want to avoid repeats across more tracks. URIs only, so it's cheap."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{
			"limit": map[string]any{"type": "integer", "description": "How many recent URIs to return (default 150)."},
		}},
	}},
}

// runHistoryTool handles the built-in history tool; handled is false for any
// other tool name.
func runHistoryTool(name string, input json.RawMessage, hist History) (handled bool, out string, err error) {
	if name != "list_recent_tracks" {
		return false, "", nil
	}
	if hist == nil {
		return true, `{"items":[]}`, nil
	}
	var args struct {
		Limit int `json:"limit"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	if args.Limit <= 0 {
		args.Limit = 150
	}
	data, _ := json.Marshal(map[string]any{"items": hist.Recent(args.Limit)})
	return true, string(data), nil
}

// memoryToolParams are built-in, non-Spotify tools for persisting user
// preferences. They are handled inside the agent loop, not via the Spotify
// tool registry.
var memoryToolParams = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name:        "remember_preference",
		Description: anthropic.String("Save a lasting user music preference so it persists across chats (e.g. key 'genres' value 'synthwave, lo-fi'; key 'avoid' value 'explicit lyrics'; key 'era' value '80s-90s'). Use short, stable keys. Call this when the user states a durable taste, not for one-off requests."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{
			"key":   map[string]any{"type": "string", "description": "Short preference topic, e.g. 'genres', 'avoid', 'era', 'favorite_artists'."},
			"value": map[string]any{"type": "string", "description": "The preference value."},
		}, Required: []string{"key", "value"}},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "list_preferences",
		Description: anthropic.String("List the user's saved music preferences."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "forget_preference",
		Description: anthropic.String("Delete a saved preference by key, or pass key '*' to clear all preferences."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{
			"key": map[string]any{"type": "string", "description": "The preference key to delete, or '*' to clear all."},
		}, Required: []string{"key"}},
	}},
}

// runMemoryTool handles the built-in preference tools; handled is false for
// any other tool name.
func runMemoryTool(name string, input json.RawMessage, mem Memory) (handled bool, out string, err error) {
	switch name {
	case "remember_preference", "list_preferences", "forget_preference":
		handled = true
	default:
		return false, "", nil
	}
	if mem == nil {
		return true, "", errors.New("preference memory is unavailable")
	}
	var args struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	switch name {
	case "remember_preference":
		if e := mem.Set(args.Key, args.Value); e != nil {
			return true, "", e
		}
		return true, `{"status":"ok"}`, nil
	case "forget_preference":
		if args.Key == "*" {
			if e := mem.Clear(); e != nil {
				return true, "", e
			}
			return true, `{"status":"ok","cleared":true}`, nil
		}
		if e := mem.Set(args.Key, ""); e != nil {
			return true, "", e
		}
		return true, `{"status":"ok"}`, nil
	default: // list_preferences
		data, _ := json.Marshal(mem.List())
		return true, string(data), nil
	}
}

// Model reports the model the agent is currently configured to use.
func (a *Agent) Model() string { return string(a.model) }

// ModelInfo is a model the account can use, as returned by ListModels.
type ModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	MaxTokens   int64  `json:"max_tokens"`
}

// ListModels fetches the models available to the configured API key from the
// Anthropic Models API, newest first.
func (a *Agent) ListModels(ctx context.Context) ([]ModelInfo, error) {
	iter := a.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var out []ModelInfo
	for iter.Next() {
		m := iter.Current()
		out = append(out, ModelInfo{ID: m.ID, DisplayName: m.DisplayName, MaxTokens: m.MaxTokens})
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Chat runs one user turn on top of history: it streams assistant text and
// executes tool calls until the model finishes, emitting events along the
// way. It returns the updated history — also when it fails partway, so the
// caller can persist whatever the model already did. sp may be nil when the
// user has not connected Spotify yet; tools then return an instructive error
// to the model. Destructive tools are gated through confirm before they run.
func (a *Agent) Chat(ctx context.Context, history []anthropic.MessageParam, text string, sp *spotify.Client, emit func(Event), opts TurnOptions) ([]anthropic.MessageParam, error) {
	messages := append(history, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))

	// The stable system prompt stays first (cached); volatile per-turn context
	// (current time, user preferences) goes in a second block so it doesn't
	// invalidate the cache prefix on every message.
	system := []anthropic.TextBlockParam{{
		Text:         systemPrompt,
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}}
	if ctxText := turnContext(opts); ctxText != "" {
		system = append(system, anthropic.TextBlockParam{Text: ctxText})
	}

	for turn := 0; turn < maxTurns; turn++ {
		params := anthropic.MessageNewParams{
			Model:     a.model,
			MaxTokens: a.maxTokens,
			System:    system,
			Messages:  messages,
			Tools:     a.toolParams,
		}

		stream := a.client.Messages.NewStreaming(ctx, params)
		msg := anthropic.Message{}
		for stream.Next() {
			event := stream.Current()
			if err := msg.Accumulate(event); err != nil {
				return messages, fmt.Errorf("accumulate stream: %w", err)
			}
			if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" {
					emit(Event{Type: "text", Text: td.Text})
				}
			}
		}
		if err := stream.Err(); err != nil {
			return messages, err
		}

		messages = append(messages, msg.ToParam())

		if msg.StopReason != anthropic.StopReasonToolUse {
			emit(Event{Type: "done", StopReason: string(msg.StopReason)})
			return messages, nil
		}

		var results []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			tu, isTool := block.AsAny().(anthropic.ToolUseBlock)
			if !isTool {
				continue
			}
			emit(Event{Type: "tool_use", Name: tu.Name, Input: tu.Input})

			// Built-in preference / history tools are handled locally, not via
			// Spotify.
			if handled, out, memErr := runMemoryTool(tu.Name, tu.Input, opts.Memory); handled {
				success := memErr == nil
				if memErr != nil {
					out = "Error: " + memErr.Error()
				}
				emit(Event{Type: "tool_result", Name: tu.Name, OK: &success, Summary: summarize(out)})
				results = append(results, anthropic.NewToolResultBlock(tu.ID, out, !success))
				continue
			}
			if handled, out, histErr := runHistoryTool(tu.Name, tu.Input, opts.History); handled {
				success := histErr == nil
				if histErr != nil {
					out = "Error: " + histErr.Error()
				}
				emit(Event{Type: "tool_result", Name: tu.Name, OK: &success, Summary: summarize(out)})
				results = append(results, anthropic.NewToolResultBlock(tu.ID, out, !success))
				continue
			}

			// Gate destructive tools behind user confirmation, unless the call
			// is exempt (e.g. editing a throwaway temp playlist).
			exempt := opts.SkipConfirm != nil && opts.SkipConfirm(tu.Name, tu.Input)
			if tool, known := a.tools[tu.Name]; known && tool.Confirm != "" && !exempt {
				approved := opts.Confirm != nil && opts.Confirm(ctx, ConfirmRequest{
					Name:     tu.Name,
					Input:    tu.Input,
					Question: tool.Confirm,
				})
				if !approved {
					declined := false
					emit(Event{Type: "tool_result", Name: tu.Name, OK: &declined, Summary: "declined by the user"})
					results = append(results, anthropic.NewToolResultBlock(tu.ID,
						"The user declined this action, so it was not performed. Acknowledge this and continue without it.", false))
					continue
				}
			}

			out, err := a.runTool(ctx, tu.Name, tu.Input, sp)
			success := err == nil
			if err != nil {
				log.Printf("tool %s failed: %v (input: %s)", tu.Name, err, tu.Input)
				out = "Error: " + err.Error()
			}
			emit(Event{Type: "tool_result", Name: tu.Name, OK: &success, Summary: summarize(out)})
			results = append(results, anthropic.NewToolResultBlock(tu.ID, out, !success))

			// Remember tracks the model just queued/added so it doesn't repeat
			// them in future selections.
			if success && opts.History != nil {
				if uris := aitools.AddedTrackURIs(tu.Name, tu.Input); len(uris) > 0 {
					opts.History.Add(uris)
				}
			}
		}
		if len(results) == 0 {
			return messages, errors.New("model stopped for tool use but produced no tool calls")
		}
		messages = append(messages, anthropic.NewUserMessage(results...))
	}
	return messages, fmt.Errorf("agent did not finish within %d turns", maxTurns)
}

func (a *Agent) runTool(ctx context.Context, name string, input json.RawMessage, sp *spotify.Client) (string, error) {
	tool, ok := a.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if sp == nil {
		return "", errors.New("the user has not connected their Spotify account yet — ask them to click 'Connect Spotify' in the header")
	}
	result, err := tool.Handler(ctx, sp, input)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(aitools.Slim(result))
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	out := string(data)
	if len(out) > maxToolResultChars {
		out = out[:maxToolResultChars] + `... [truncated — use limit/offset to page through smaller chunks]`
	}
	return out, nil
}

// turnContext builds the volatile per-turn system block: the current local
// time and the user's saved preferences, so the model is time-aware and
// personalises without being told each time.
func turnContext(opts TurnOptions) string {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Current local time: %s.", now.Format("Monday 2006-01-02 15:04"))
	if opts.Memory != nil {
		if prefs := opts.Memory.Text(); prefs != "" {
			b.WriteString("\n\nThe user's saved music preferences (apply them unless they say otherwise):\n")
			b.WriteString(prefs)
		}
	}
	if opts.History != nil {
		if recent := opts.History.Recent(recentInjectN); len(recent) > 0 {
			b.WriteString("\n\nRecently queued/added tracks — do NOT queue or add these again when generating new selections (playlists, queues, \"more like this\") unless the user explicitly asks to repeat a specific one. Pick different tracks:\n")
			b.WriteString(strings.Join(recent, " "))
		}
	}
	return b.String()
}

// summarize trims a tool result for display in the UI event feed.
func summarize(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
