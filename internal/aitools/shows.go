package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func showTools() []Tool {
	return []Tool{
		{
			Name:        "get_show",
			Description: "Get catalog information for a single podcast show by its Spotify ID. A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the show."),
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
				return c.GetShow(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_shows",
			Description: "Get catalog information for several podcast shows by their Spotify IDs (max 50). A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify show IDs."),
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
				return c.GetShows(ctx, args.IDs, opts...)
			},
		},
		{
			Name:        "get_show_episodes",
			Description: "List the episodes of a podcast show, paged. A market (or user token) is required for results to appear.",
			Schema:      schema(pageProps(map[string]any{"id": str("The Spotify ID of the show.")}), "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
					pageArgs
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetShowEpisodes(ctx, args.ID, args.opts()...)
			},
		},
		{
			Name:        "get_saved_shows",
			Description: "List the podcast shows saved in the current user's library, paged.",
			Schema:      schema(pageProps(nil)),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetSavedShows(ctx, args.opts()...)
			},
		},
		{
			Name:        "save_shows",
			Description: "Save one or more podcast shows to the current user's library.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify show IDs to save.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SaveShows(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "remove_saved_shows",
			Description: "Remove one or more podcast shows from the current user's library.",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify show IDs to remove."),
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
				if err := c.RemoveSavedShows(ctx, args.IDs, opts...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "check_saved_shows",
			Description: "Check whether podcast shows are already saved in the current user's library. Returns a boolean per ID, in order.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify show IDs to check.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CheckSavedShows(ctx, args.IDs...)
			},
		},
	}
}
