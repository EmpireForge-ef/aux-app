package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func categoryTools() []Tool {
	return []Tool{
		{
			Name:        "get_categories",
			Description: "List the browse categories ('genres') used to tag content in Spotify's browse tab, paged. Removed from development-mode apps by Spotify in February 2026; fails with 403 unless the app has extended quota.",
			Schema: schema(map[string]any{
				"limit":  integer("Maximum number of items to return (default set by Spotify, max usually 50)."),
				"offset": integer("Index of the first item to return, for paging."),
				"locale": str("Optional desired language, as an ISO 639-1 language code and ISO 3166-1 alpha-2 country code joined by an underscore, e.g. 'es_MX'."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Limit  int    `json:"limit"`
					Offset int    `json:"offset"`
					Locale string `json:"locale"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Limit > 0 {
					opts = append(opts, spotify.Limit(args.Limit))
				}
				if args.Offset > 0 {
					opts = append(opts, spotify.Offset(args.Offset))
				}
				if args.Locale != "" {
					opts = append(opts, spotify.Locale(args.Locale))
				}
				return c.GetCategories(ctx, opts...)
			},
		},
		{
			Name:        "get_category",
			Description: "Get a single browse category by its ID, e.g. 'dinner'. Removed from development-mode apps by Spotify in February 2026; fails with 403 unless the app has extended quota.",
			Schema: schema(map[string]any{
				"id":     str("The category ID, e.g. 'dinner'."),
				"locale": str("Optional desired language, as an ISO 639-1 language code and ISO 3166-1 alpha-2 country code joined by an underscore, e.g. 'es_MX'."),
			}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID     string `json:"id"`
					Locale string `json:"locale"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Locale != "" {
					opts = append(opts, spotify.Locale(args.Locale))
				}
				return c.GetCategory(ctx, args.ID, opts...)
			},
		},
	}
}
