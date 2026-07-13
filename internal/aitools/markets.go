package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func marketTools() []Tool {
	return []Tool{
		{
			Name:        "get_available_markets",
			Description: "List the ISO 3166-1 alpha-2 country codes in which Spotify is available. Removed from development-mode apps by Spotify in February 2026; fails with 403 unless the app has extended quota.",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, _ json.RawMessage) (any, error) {
				return c.GetAvailableMarkets(ctx)
			},
		},
	}
}
