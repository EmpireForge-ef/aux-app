// Package ai runs the Anthropic-powered agent loop: it streams model output,
// executes Spotify tool calls, and reports progress through events.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
- Aux passively learns what the user actually listens to. Call get_listening_profile to see their real habits — top genres/artists/tracks broken down by time of day, weekday vs weekend, and weather. Use it to ground vibe playlists, queues, and "something for right now" requests in what they genuinely play (e.g. check their current part-of-day or rainy-day pattern before choosing), and combine it with their stated preferences. If it reports no data yet, fall back to preferences, top/saved tracks, and search as usual. The profile reflects real plays, so weight it when the user asks for something that fits their taste or the moment.
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
	// Resolved marks a confirm event whose decision was already made, so a
	// client replaying the buffered turn from the start renders it inertly
	// instead of re-opening the dialog. Set only when streaming, never stored.
	Resolved bool `json:"resolved,omitempty"`
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

// Listening exposes the passively-learned listening profile (what the user
// plays by time of day, weekday/weekend, and weather) as a JSON summary.
type Listening interface {
	ProfileJSON(partOfDay string, weekend *bool, weather string, days int) (string, error)
}

// TurnOptions carries the per-turn extras beyond the conversation itself.
type TurnOptions struct {
	Confirm   ConfirmFunc // gate for destructive tools
	Memory    Memory      // user preferences (nil disables the memory tools)
	Now       time.Time   // current time for context; zero means time.Now()
	History   History     // recently-used tracks, to avoid repetition (nil disables)
	Listening Listening   // passively-learned listening profile (nil disables the tool)
	// LearnedProfile is the periodically-distilled summary of the user's
	// listening habits, injected into each turn's context. Empty when none yet.
	LearnedProfile string
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
	client       anthropic.Client
	model        anthropic.Model
	maxTokens    int64
	contextLimit int64
	baseTokens   int // rough token cost of the static system prompt + tools
	tools        map[string]aitools.Tool
	toolParams   []anthropic.ToolUnionParam
}

func New(apiKey, model string, maxTokens, contextLimit int64) *Agent {
	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	registry := aitools.All()
	byName := make(map[string]aitools.Tool, len(registry))
	for _, t := range registry {
		byName[t.Name] = t
	}
	toolParams := append(append(append(aitools.Params(registry), memoryToolParams...), historyToolParams...), listeningToolParams...)
	// Cache the (static, large) tool definitions: one breakpoint on the last
	// tool caches the whole block so it isn't re-billed on every request.
	if n := len(toolParams); n > 0 && toolParams[n-1].OfTool != nil {
		toolParams[n-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}

	return &Agent{
		client:       anthropic.NewClient(opts...),
		model:        anthropic.Model(model),
		maxTokens:    maxTokens,
		contextLimit: contextLimit,
		baseTokens:   estimateJSONTokens(toolParams) + estimateTextTokens(systemPrompt),
		tools:        byName,
		toolParams:   toolParams,
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

// listeningToolParams is the built-in tool for querying the passively-learned
// listening profile.
var listeningToolParams = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name:        "get_listening_profile",
		Description: anthropic.String("Look up what the user ACTUALLY listens to, learned passively from their play history and tagged with time-of-day, weekday/weekend, and weather. Call this when building a vibe playlist or queue, or making recommendations, to ground them in the user's real habits — e.g. check their weekend-evening or rainy-day patterns first. With no arguments you get an overall summary plus a per-part-of-day genre breakdown. Optionally filter to a slice. Returns top genres, artists, and tracks with play counts. It is cheap; prefer it over guessing."),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{
			"part_of_day": map[string]any{"type": "string", "enum": []string{"morning", "afternoon", "evening", "night"}, "description": "Restrict to a part of the day (local time)."},
			"weekend":     map[string]any{"type": "boolean", "description": "true = weekends only, false = weekdays only."},
			"weather":     map[string]any{"type": "string", "description": "Restrict to a weather condition, e.g. 'rain', 'clear', 'clouds', 'snow'."},
			"days":        map[string]any{"type": "integer", "description": "Only consider plays from the last N days."},
		}},
	}},
}

// runListeningTool handles the built-in listening-profile tool; handled is
// false for any other tool name.
func runListeningTool(name string, input json.RawMessage, l Listening) (handled bool, out string, err error) {
	if name != "get_listening_profile" {
		return false, "", nil
	}
	if l == nil {
		return true, `{"note":"the listening profile is not available"}`, nil
	}
	var args struct {
		PartOfDay string `json:"part_of_day"`
		Weekend   *bool  `json:"weekend"`
		Weather   string `json:"weather"`
		Days      int    `json:"days"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	out, err = l.ProfileJSON(args.PartOfDay, args.Weekend, args.Weather, args.Days)
	if err != nil {
		return true, "", err
	}
	return true, out, nil
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
	// Summarise older turns up front if the stored history is already near the
	// context limit, so the very first request of this turn fits.
	history = a.compactIfNeeded(ctx, history, emit)

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

	reactiveCompactions := 0
	for turn := 0; turn < maxTurns; turn++ {
		// Cache the conversation so far so a multi-tool turn doesn't re-bill the
		// growing pile of tool results on every step within the turn.
		markConversationCache(messages)

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
				return a.stripCache(messages), fmt.Errorf("accumulate stream: %w", err)
			}
			if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" {
					emit(Event{Type: "text", Text: td.Text})
				}
			}
		}
		if err := stream.Err(); err != nil {
			// If the request still overflowed the context window, summarise and
			// retry this turn a couple of times before giving up.
			if isContextLengthError(err) && reactiveCompactions < 2 {
				reactiveCompactions++
				messages = a.compact(ctx, messages, a.keepBudget()/2, emit)
				turn--
				continue
			}
			return a.stripCache(messages), err
		}

		u := msg.Usage
		slog.Debug("anthropic usage",
			"input", u.InputTokens, "cache_write", u.CacheCreationInputTokens,
			"cache_read", u.CacheReadInputTokens, "output", u.OutputTokens)

		messages = append(messages, msg.ToParam())

		if msg.StopReason != anthropic.StopReasonToolUse {
			emit(Event{Type: "done", StopReason: string(msg.StopReason)})
			return a.stripCache(messages), nil
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
			if handled, out, lErr := runListeningTool(tu.Name, tu.Input, opts.Listening); handled {
				success := lErr == nil
				if lErr != nil {
					out = "Error: " + lErr.Error()
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
				slog.Warn("tool failed", "tool", tu.Name, "err", err, "input", string(tu.Input))
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
			return a.stripCache(messages), errors.New("model stopped for tool use but produced no tool calls")
		}
		messages = append(messages, anthropic.NewUserMessage(results...))
	}
	return a.stripCache(messages), fmt.Errorf("agent did not finish within %d turns", maxTurns)
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
	fmt.Fprintf(&b, "Current local time: %s.", now.Format("Monday 2006-01-02 15:04 MST"))
	if opts.Memory != nil {
		if prefs := opts.Memory.Text(); prefs != "" {
			b.WriteString("\n\nThe user's saved music preferences (apply them unless they say otherwise):\n")
			b.WriteString(prefs)
		}
	}
	if opts.LearnedProfile != "" {
		b.WriteString("\n\nLearned listening profile (durable patterns observed from what the user actually plays — weight these when recommending):\n")
		b.WriteString(opts.LearnedProfile)
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

// --- prompt caching ---------------------------------------------------------

// setBlockCacheControl sets (or clears, with a zero value) the cache_control on
// a content block, for the block kinds this agent actually produces.
func setBlockCacheControl(b *anthropic.ContentBlockParamUnion, cc anthropic.CacheControlEphemeralParam) {
	switch {
	case b.OfText != nil:
		b.OfText.CacheControl = cc
	case b.OfToolResult != nil:
		b.OfToolResult.CacheControl = cc
	case b.OfToolUse != nil:
		b.OfToolUse.CacheControl = cc
	case b.OfImage != nil:
		b.OfImage.CacheControl = cc
	case b.OfDocument != nil:
		b.OfDocument.CacheControl = cc
	}
}

// markConversationCache puts a single cache breakpoint on the last content
// block of the conversation, clearing any earlier one first so at most one
// message-level breakpoint exists (Anthropic caps total breakpoints at 4:
// tools + system + this).
func markConversationCache(messages []anthropic.MessageParam) {
	var zero anthropic.CacheControlEphemeralParam
	for _, m := range messages {
		for i := range m.Content {
			setBlockCacheControl(&m.Content[i], zero)
		}
	}
	if len(messages) == 0 {
		return
	}
	last := messages[len(messages)-1]
	if len(last.Content) == 0 {
		return
	}
	setBlockCacheControl(&last.Content[len(last.Content)-1], anthropic.NewCacheControlEphemeralParam())
}

// stripCache clears all cache_control markers before the messages are returned
// for persistence, so the stored history stays clean.
func (a *Agent) stripCache(messages []anthropic.MessageParam) []anthropic.MessageParam {
	var zero anthropic.CacheControlEphemeralParam
	for _, m := range messages {
		for i := range m.Content {
			setBlockCacheControl(&m.Content[i], zero)
		}
	}
	return messages
}

// --- context compaction -----------------------------------------------------

const summarizeSystemPrompt = `You compress a conversation between a user and Aux, an AI that controls the user's Spotify. Produce a dense summary that preserves everything needed to continue seamlessly: the user's requests and intent, durable preferences they stated, and concrete results — playlist names and IDs/URIs created or edited, tracks added, and any pending or half-finished task. Omit small talk. Write it as notes, not prose; no preamble.`

// estimateTextTokens is a cheap, dependency-free token estimate (~4 chars/token)
// used to decide when to compact — deliberately conservative, not exact.
func estimateTextTokens(s string) int { return len(s)/4 + 1 }

// estimateJSONTokens estimates the token cost of any value by its JSON size.
func estimateJSONTokens(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data) / 4
}

func estimateMessagesTokens(messages []anthropic.MessageParam) int {
	total := 0
	for _, m := range messages {
		total += estimateJSONTokens(m)
	}
	return total
}

func (a *Agent) compactAt() int  { return int(a.contextLimit) * 3 / 4 }
func (a *Agent) keepBudget() int { return int(a.contextLimit) * 2 / 5 }

// compactIfNeeded summarises older turns when the estimated request size
// approaches the context limit; otherwise it returns messages unchanged.
func (a *Agent) compactIfNeeded(ctx context.Context, messages []anthropic.MessageParam, emit func(Event)) []anthropic.MessageParam {
	if a.contextLimit <= 0 {
		return messages
	}
	if a.baseTokens+estimateMessagesTokens(messages) < a.compactAt() {
		return messages
	}
	return a.compact(ctx, messages, a.keepBudget(), emit)
}

// compact keeps the most recent messages that fit in keepBudget (estimated
// tokens) verbatim and replaces the older prefix with a one-message summary,
// preserving valid user/assistant alternation. On any failure it returns the
// input unchanged so the turn can still proceed (the reactive retry is the
// backstop).
func (a *Agent) compact(ctx context.Context, messages []anthropic.MessageParam, keepBudget int, emit func(Event)) []anthropic.MessageParam {
	if len(messages) < 4 {
		return messages // nothing meaningful to summarise
	}
	// Walk back from the end, keeping messages until we exceed keepBudget.
	kept, cut := 0, len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		t := estimateJSONTokens(messages[i])
		if cut < len(messages) && kept+t > keepBudget {
			break
		}
		kept += t
		cut = i
	}
	// The summary is a user message, so the kept suffix must start with an
	// assistant message to preserve alternation. History alternates
	// user/assistant from index 0, so an even cut index is a user message —
	// push it into the summarised prefix.
	if cut%2 == 0 {
		cut++
	}
	if cut <= 0 || cut >= len(messages) {
		return messages
	}

	summary, err := a.summarizeMessages(ctx, messages[:cut])
	if err != nil || summary == "" {
		slog.Warn("compaction summary failed", "err", err)
		return messages
	}

	compacted := make([]anthropic.MessageParam, 0, len(messages)-cut+1)
	compacted = append(compacted, anthropic.NewUserMessage(anthropic.NewTextBlock(
		"[Summary of the earlier part of this conversation]\n"+summary)))
	compacted = append(compacted, messages[cut:]...)

	if emit != nil {
		emit(Event{Type: "notice", Message: "Summarised earlier messages to stay within the context limit."})
	}
	return compacted
}

const analyzeSystemPrompt = `You analyse a user's music-listening data and write a concise "learned profile" of durable patterns, for grounding future recommendations. Cover, only where the data supports it: the genres and artists they gravitate to; how taste shifts by time of day (morning/afternoon/evening/night), weekday vs weekend, and weather; and any recent trend or drift. Write 4–8 short, specific bullet lines grounded ONLY in the data provided — no preamble, no caveats, and don't just restate the raw numbers; synthesise them into useful, human patterns.`

// Analyze distils a block of listening data into a short "learned profile" in
// one non-streaming call. Used by the scheduled profile analyzer.
func (a *Agent) Analyze(ctx context.Context, data string) (string, error) {
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 700,
		System:    []anthropic.TextBlockParam{{Text: analyzeSystemPrompt}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(
			"Listening data (aggregated):\n\n" + data))},
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// summarizeMessages asks the model for a compact summary of a slice of history,
// rendered as plain text so tool_use/tool_result pairing doesn't matter here.
func (a *Agent) summarizeMessages(ctx context.Context, messages []anthropic.MessageParam) (string, error) {
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: summarizeSystemPrompt}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(
			"Conversation to summarise:\n\n" + renderForSummary(messages)))},
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// renderForSummary flattens messages to a compact text transcript. Tool calls
// keep their name and (truncated) input — where the playlist IDs and track
// URIs worth preserving live — while bulky tool results are elided.
func renderForSummary(messages []anthropic.MessageParam) string {
	var b strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		for _, block := range m.Content {
			switch {
			case block.OfText != nil:
				fmt.Fprintf(&b, "%s: %s\n", role, block.OfText.Text)
			case block.OfToolUse != nil:
				input, _ := json.Marshal(block.OfToolUse.Input)
				in := string(input)
				if len(in) > 300 {
					in = in[:300] + "…"
				}
				fmt.Fprintf(&b, "%s: [tool %s %s]\n", role, block.OfToolUse.Name, in)
			case block.OfToolResult != nil:
				fmt.Fprintf(&b, "%s: [tool result]\n", role)
			}
		}
	}
	return b.String()
}

// isContextLengthError reports whether err is the API's "prompt is too long"
// rejection, so we can summarise and retry rather than failing the turn.
func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "prompt is too long") || strings.Contains(msg, "too many total text bytes") {
		return true
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 400 && strings.Contains(strings.ToLower(apiErr.Error()), "too long")
	}
	return false
}
