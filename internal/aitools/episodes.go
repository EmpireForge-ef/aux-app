package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func episodeTools() []Tool {
	return []Tool{
		{
			Name:        "get_episode",
			Description: "Get catalog information for a single podcast episode by its Spotify ID. A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the episode."),
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
				return c.GetEpisode(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_episodes",
			Description: "Get catalog information for several podcast episodes by their Spotify IDs (max 50). A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify episode IDs."),
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
				return c.GetEpisodes(ctx, args.IDs, opts...)
			},
		},
		{
			Name:        "get_saved_episodes",
			Description: "List the podcast episodes saved in the current user's library, paged.",
			Schema:      schema(pageProps(nil)),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetSavedEpisodes(ctx, args.opts()...)
			},
		},
		{
			Name:        "save_episodes",
			Description: "Save one or more podcast episodes to the current user's library.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify episode IDs to save.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SaveEpisodes(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "remove_saved_episodes",
			Confirm:     "Remove these episodes from your library?",
			Description: "Remove one or more podcast episodes from the current user's library.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify episode IDs to remove.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.RemoveSavedEpisodes(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "check_saved_episodes",
			Description: "Check whether podcast episodes are already saved in the current user's library. Returns a boolean per ID, in order.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify episode IDs to check.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CheckSavedEpisodes(ctx, args.IDs...)
			},
		},
	}
}
