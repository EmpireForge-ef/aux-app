package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func artistTools() []Tool {
	return []Tool{
		{
			Name:        "get_artist",
			Description: "Get catalog information for a single artist by their Spotify ID.",
			Schema: schema(map[string]any{
				"id": str("The Spotify ID of the artist."),
			}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetArtist(ctx, args.ID)
			},
		},
		{
			Name:        "get_artists",
			Description: "Get catalog information for several artists by their Spotify IDs (max 50).",
			Schema: schema(map[string]any{
				"ids": strArray("Spotify artist IDs."),
			}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetArtists(ctx, args.IDs...)
			},
		},
		{
			Name:        "get_artist_albums",
			Description: "List an artist's albums, paged. Optionally filter by the artist's relationship to the album.",
			Schema: schema(pageProps(map[string]any{
				"id": str("The Spotify ID of the artist."),
				"include_groups": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": []string{"album", "single", "appears_on", "compilation"}},
					"description": "Optional album relationship filters; only albums matching one of these groups are returned.",
				},
			}), "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID            string   `json:"id"`
					IncludeGroups []string `json:"include_groups"`
					pageArgs
				}](input)
				if err != nil {
					return nil, err
				}
				opts := args.opts()
				if len(args.IncludeGroups) > 0 {
					opts = append(opts, spotify.IncludeGroups(args.IncludeGroups...))
				}
				return c.GetArtistAlbums(ctx, args.ID, opts...)
			},
		},
	}
}
