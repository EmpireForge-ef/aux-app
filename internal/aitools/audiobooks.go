package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func audiobookTools() []Tool {
	return []Tool{
		{
			Name:        "get_audiobook",
			Description: "Get catalog information for a single audiobook by its Spotify ID. A market (or user token) is required for results to appear; audiobooks are only available in a few markets (e.g. US, UK, CA, IE, NZ, AU).",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the audiobook."),
				"market": str("Optional ISO 3166-1 alpha-2 country code."),
			}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID     string `json:"id"`
					Market string `json:"market"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				return c.GetAudiobook(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_audiobooks",
			Description: "Get catalog information for several audiobooks by their Spotify IDs (max 50). A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify audiobook IDs."),
				"market": str("Optional ISO 3166-1 alpha-2 country code."),
			}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs    []string `json:"ids"`
					Market string   `json:"market"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				return c.GetAudiobooks(ctx, args.IDs, opts...)
			},
		},
		{
			Name:        "get_audiobook_chapters",
			Description: "List the chapters of an audiobook, paged. A market (or user token) is required for results to appear.",
			Schema:      schema(pageProps(map[string]any{"id": str("The Spotify ID of the audiobook.")}), "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
					pageArgs
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetAudiobookChapters(ctx, args.ID, args.opts()...)
			},
		},
		{
			Name:        "get_saved_audiobooks",
			Description: "List the audiobooks saved in the current user's library, paged.",
			Schema:      schema(pageProps(nil)),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetSavedAudiobooks(ctx, args.opts()...)
			},
		},
		{
			Name:        "save_audiobooks",
			Description: "Save one or more audiobooks to the current user's library.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify audiobook IDs to save.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SaveAudiobooks(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "remove_saved_audiobooks",
			Confirm:     "Remove these audiobooks from your library?",
			Description: "Remove one or more audiobooks from the current user's library.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify audiobook IDs to remove.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.RemoveSavedAudiobooks(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "check_saved_audiobooks",
			Description: "Check whether audiobooks are already saved in the current user's library. Returns a boolean per ID, in order.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify audiobook IDs to check.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CheckSavedAudiobooks(ctx, args.IDs...)
			},
		},
	}
}
