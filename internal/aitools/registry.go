// Package aitools exposes the full spotify-go-wrapper API surface as
// Anthropic tool definitions the AI agent can call.
package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
	"github.com/anthropics/anthropic-sdk-go"
)

// Tool couples an Anthropic tool definition with the handler that executes it
// against the user's Spotify client.
type Tool struct {
	Name        string
	Description string
	Schema      anthropic.ToolInputSchemaParam
	Handler     func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error)
	// Confirm, when non-empty, marks the tool as destructive: the agent must
	// obtain the user's confirmation before running it. The string is the
	// question shown to the user.
	Confirm string
}

// All returns every tool, covering the wrapper's complete method surface.
func All() []Tool {
	var tools []Tool
	tools = append(tools, albumTools()...)
	tools = append(tools, artistTools()...)
	tools = append(tools, trackTools()...)
	tools = append(tools, playlistTools()...)
	tools = append(tools, playerTools()...)
	tools = append(tools, searchTools()...)
	tools = append(tools, userTools()...)
	tools = append(tools, showTools()...)
	tools = append(tools, episodeTools()...)
	tools = append(tools, audiobookTools()...)
	tools = append(tools, chapterTools()...)
	tools = append(tools, tempTools()...)
	return tools
}

// Params converts the registry into the SDK's tool union slice.
func Params(tools []Tool) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: t.Schema,
		}})
	}
	return out
}

// --- schema helpers -------------------------------------------------------

func schema(props map[string]any, required ...string) anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: props, Required: required}
}

func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func number(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func strArray(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

// decode unmarshals tool input into T with strict-ish error wrapping.
func decode[T any](input json.RawMessage) (T, error) {
	var v T
	if len(input) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(input, &v); err != nil {
		return v, fmt.Errorf("invalid tool input: %w", err)
	}
	return v, nil
}

// pageArgs are the common paging/market inputs accepted by list endpoints.
type pageArgs struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Market string `json:"market"`
}

func (p pageArgs) opts() []spotify.RequestOption {
	var opts []spotify.RequestOption
	if p.Limit > 0 {
		opts = append(opts, spotify.Limit(p.Limit))
	}
	if p.Offset > 0 {
		opts = append(opts, spotify.Offset(p.Offset))
	}
	if p.Market != "" {
		opts = append(opts, spotify.Market(p.Market))
	}
	return opts
}

// pageProps returns the schema properties matching pageArgs, so every list
// tool documents paging identically.
func pageProps(extra map[string]any) map[string]any {
	props := map[string]any{
		"limit":  integer("Maximum number of items to return (default set by Spotify, max usually 50)."),
		"offset": integer("Index of the first item to return, for paging."),
		"market": str("Optional ISO 3166-1 alpha-2 country code, e.g. 'DE'."),
	}
	for k, v := range extra {
		props[k] = v
	}
	return props
}

// isForbidden reports whether err is a Spotify 403, which for
// development-mode app registrations marks endpoints Spotify has walled off
// rather than a scope problem.
func isForbidden(err error) bool {
	var apiErr *spotify.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// ok is a small success payload for endpoints that return no body.
func ok() map[string]any {
	return map[string]any{"status": "ok"}
}
