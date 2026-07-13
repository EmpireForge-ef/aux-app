package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func trackTools() []Tool {
	return []Tool{
		{
			Name:        "get_track",
			Description: "Get catalog information for a single track by its Spotify ID.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the track."),
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
				return c.GetTrack(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_tracks",
			Description: "Get catalog information for several tracks by their Spotify IDs (max 50).",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify track IDs."),
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
				return c.GetTracks(ctx, args.IDs, opts...)
			},
		},
		{
			Name:        "get_saved_tracks",
			Description: "List the tracks saved in the current user's library ('Liked Songs'), paged.",
			Schema:      schema(pageProps(nil)),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetSavedTracks(ctx, args.opts()...)
			},
		},
		{
			Name:        "save_tracks",
			Description: "Save one or more tracks to the current user's library ('Liked Songs').",
			Schema:      schema(map[string]any{"ids": strArray("Spotify track IDs to save.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SaveTracks(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "remove_saved_tracks",
			Confirm:     "Remove these tracks from your Liked Songs?",
			Description: "Remove one or more tracks from the current user's library ('Liked Songs').",
			Schema:      schema(map[string]any{"ids": strArray("Spotify track IDs to remove.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.RemoveSavedTracks(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "check_saved_tracks",
			Description: "Check whether tracks are already saved in the current user's library. Returns a boolean per ID, in order.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify track IDs to check.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CheckSavedTracks(ctx, args.IDs...)
			},
		},
	}
}
