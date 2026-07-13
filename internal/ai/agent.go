// Package ai runs the Anthropic-powered agent loop: it streams model output,
// executes Spotify tool calls, and reports progress through events.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

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
		toolParams: aitools.Params(registry),
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
func (a *Agent) Chat(ctx context.Context, history []anthropic.MessageParam, text string, sp *spotify.Client, emit func(Event), confirm ConfirmFunc) ([]anthropic.MessageParam, error) {
	messages := append(history, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))

	for turn := 0; turn < maxTurns; turn++ {
		params := anthropic.MessageNewParams{
			Model:     a.model,
			MaxTokens: a.maxTokens,
			System: []anthropic.TextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}},
			Messages: messages,
			Tools:    a.toolParams,
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

			// Gate destructive tools behind user confirmation.
			if tool, known := a.tools[tu.Name]; known && tool.Confirm != "" {
				approved := confirm != nil && confirm(ctx, ConfirmRequest{
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
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	out := string(data)
	if len(out) > maxToolResultChars {
		out = out[:maxToolResultChars] + `... [truncated — use limit/offset to page through smaller chunks]`
	}
	return out, nil
}

// summarize trims a tool result for display in the UI event feed.
func summarize(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
