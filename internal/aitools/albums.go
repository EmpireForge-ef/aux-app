package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func albumTools() []Tool {
	return []Tool{
		{
			Name:        "get_album",
			Description: "Get catalog information for a single album by its Spotify ID.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the album."),
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
				return c.GetAlbum(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_albums",
			Description: "Get catalog information for several albums by their Spotify IDs (max 20).",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify album IDs."),
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
				return c.GetAlbums(ctx, args.IDs, opts...)
			},
		},
		{
			Name:        "get_album_tracks",
			Description: "List the tracks of an album, paged.",
			Schema: schema(pageProps(map[string]any{
				"id": str("The Spotify ID of the album."),
			}), "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
					pageArgs
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetAlbumTracks(ctx, args.ID, args.opts()...)
			},
		},
		{
			Name:        "get_saved_albums",
			Description: "List the albums saved in the current user's library, paged.",
			Schema:      schema(pageProps(nil)),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetSavedAlbums(ctx, args.opts()...)
			},
		},
		{
			Name:        "save_albums",
			Description: "Save one or more albums to the current user's library (max 50).",
			Schema:      schema(map[string]any{"ids": strArray("Spotify album IDs to save.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SaveAlbums(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "remove_saved_albums",
			Confirm:     "Remove these albums from your library?",
			Description: "Remove one or more albums from the current user's library (max 50).",
			Schema:      schema(map[string]any{"ids": strArray("Spotify album IDs to remove.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.RemoveSavedAlbums(ctx, args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "check_saved_albums",
			Description: "Check whether albums are already saved in the current user's library. Returns a boolean per ID, in order.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify album IDs to check.")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CheckSavedAlbums(ctx, args.IDs...)
			},
		},
	}
}
