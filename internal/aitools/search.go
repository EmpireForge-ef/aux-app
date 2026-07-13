package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func searchTools() []Tool {
	return []Tool{
		{
			Name: "search",
			Description: "Search the Spotify catalog for albums, artists, playlists, tracks, shows, episodes, or audiobooks. " +
				"Supports field filters in the query such as artist:, album:, track:, year:, genre:, isrc:, upc:, tag:new, tag:hipster. Development-mode apps are capped at limit 10 per type (default 5) since February 2026.",
			Schema: schema(pageProps(map[string]any{
				"query": str("The search query."),
				"types": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": []string{"album", "artist", "playlist", "track", "show", "episode", "audiobook"}},
					"description": "Item types to search across.",
				},
			}), "query", "types"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					pageArgs
					Query string   `json:"query"`
					Types []string `json:"types"`
				}](input)
				if err != nil {
					return nil, err
				}
				types := make([]spotify.SearchType, 0, len(args.Types))
				for _, t := range args.Types {
					types = append(types, spotify.SearchType(t))
				}
				return c.Search(ctx, args.Query, types, args.opts()...)
			},
		},
	}
}
