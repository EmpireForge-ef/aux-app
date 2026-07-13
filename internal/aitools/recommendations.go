package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// attributeBounds is one tunable attribute's min/max/target tuning, all
// optional.
type attributeBounds struct {
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	Target *float64 `json:"target"`
}

func recommendationTools() []Tool {
	return []Tool{
		{
			Name:        "get_recommendations",
			Description: "Generate track recommendations from 1-5 seed artists, genres and tracks combined, optionally tuned by track attributes. Deprecated by Spotify; fails for apps registered after 2024-11-27.",
			Schema: schema(map[string]any{
				"seed_artists": strArray("Seed artist IDs."),
				"seed_genres":  strArray("Seed genre names (see get_available_genre_seeds)."),
				"seed_tracks":  strArray("Seed track IDs."),
				"attributes": map[string]any{
					"type":        "object",
					"description": "Tunable track attributes keyed by name (acousticness, danceability, duration_ms, energy, instrumentalness, key, liveness, loudness, mode, popularity, speechiness, tempo, time_signature, valence). Each value is an object with optional numeric 'min', 'max' and 'target' fields.",
					"additionalProperties": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"min":    number("Hard floor for the attribute."),
							"max":    number("Hard ceiling for the attribute."),
							"target": number("Preferred value; results are ranked by proximity."),
						},
					},
				},
				"limit":  integer("Maximum number of recommended tracks to return (1-100)."),
				"market": str("Optional ISO 3166-1 alpha-2 country code, e.g. 'DE'."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					SeedArtists []string                   `json:"seed_artists"`
					SeedGenres  []string                   `json:"seed_genres"`
					SeedTracks  []string                   `json:"seed_tracks"`
					Attributes  map[string]attributeBounds `json:"attributes"`
					Limit       int                        `json:"limit"`
					Market      string                     `json:"market"`
				}](input)
				if err != nil {
					return nil, err
				}
				seeds := spotify.Seeds{
					Artists: args.SeedArtists,
					Genres:  args.SeedGenres,
					Tracks:  args.SeedTracks,
				}
				var attrs spotify.TrackAttributes
				if len(args.Attributes) > 0 {
					attrs = spotify.TrackAttributes{}
					for name, b := range args.Attributes {
						if b.Min != nil {
							attrs = attrs.Min(name, *b.Min)
						}
						if b.Max != nil {
							attrs = attrs.Max(name, *b.Max)
						}
						if b.Target != nil {
							attrs = attrs.Target(name, *b.Target)
						}
					}
				}
				var opts []spotify.RequestOption
				if args.Limit > 0 {
					opts = append(opts, spotify.Limit(args.Limit))
				}
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				return c.GetRecommendations(ctx, seeds, attrs, opts...)
			},
		},
		{
			Name:        "get_available_genre_seeds",
			Description: "List the genre names usable as recommendation seeds. Deprecated by Spotify; fails for apps registered after 2024-11-27.",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				return c.GetAvailableGenreSeeds(ctx)
			},
		},
	}
}
