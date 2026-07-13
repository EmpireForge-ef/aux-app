package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func chapterTools() []Tool {
	return []Tool{
		{
			Name:        "get_chapter",
			Description: "Get catalog information for a single audiobook chapter by its Spotify ID. A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the chapter."),
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
				return c.GetChapter(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "get_chapters",
			Description: "Get catalog information for several audiobook chapters by their Spotify IDs (max 50). A market (or user token) is required for results to appear.",
			Schema: schema(map[string]any{
				"ids":    strArray("Spotify chapter IDs."),
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
				return c.GetChapters(ctx, args.IDs, opts...)
			},
		},
	}
}
