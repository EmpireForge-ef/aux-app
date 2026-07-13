package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func audioTools() []Tool {
	return []Tool{
		{
			Name:        "get_audio_features",
			Description: "Get the audio features (danceability, energy, tempo, valence, ...) of a single track. Deprecated by Spotify; fails for apps registered after 2024-11-27.",
			Schema:      schema(map[string]any{"id": str("The Spotify ID of the track.")}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetAudioFeatures(ctx, args.ID)
			},
		},
		{
			Name:        "get_multiple_audio_features",
			Description: "Get the audio features of up to 100 tracks at once, in ID order. Deprecated by Spotify; fails for apps registered after 2024-11-27.",
			Schema:      schema(map[string]any{"ids": strArray("Spotify track IDs (max 100).")}, "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					IDs []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetMultipleAudioFeatures(ctx, args.IDs...)
			},
		},
		{
			Name:        "get_audio_analysis",
			Description: "Get the low-level, time-resolved audio analysis (bars, beats, sections, segments, tatums) of a track. Deprecated by Spotify; fails for apps registered after 2024-11-27.",
			Schema:      schema(map[string]any{"id": str("The Spotify ID of the track.")}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetAudioAnalysis(ctx, args.ID)
			},
		},
	}
}
