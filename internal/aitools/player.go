package aitools

import (
	"context"
	"encoding/json"
	"time"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// deviceOpt turns an optional device ID into request options targeting that
// device; empty means the user's currently active device.
func deviceOpt(id string) []spotify.RequestOption {
	if id == "" {
		return nil
	}
	return []spotify.RequestOption{spotify.DeviceID(id)}
}

// deviceProp is the shared schema for the optional device_id input.
func deviceProp() map[string]any {
	return str("Optional device ID to target (see get_available_devices). Defaults to the currently active device.")
}

// playbackAfter returns the playback state after a control command so the
// model sees the effect (what's playing, on which device) without a separate
// get_playback_state call. A short settle delay lets Spotify apply the change.
func playbackAfter(ctx context.Context, c *spotify.Client) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}
	st, err := c.GetPlaybackState(ctx)
	if err != nil {
		return map[string]any{"status": "ok", "note": "command sent; could not read playback state: " + err.Error()}, nil
	}
	if st == nil {
		return map[string]any{"status": "ok", "note": "no active device is reporting playback"}, nil
	}
	return st, nil
}

func playerTools() []Tool {
	return []Tool{
		{
			Name:        "get_playback_state",
			Description: "Get the user's current playback state (device, item, progress, shuffle/repeat). Returns null when nothing is available. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"market": str("Optional ISO 3166-1 alpha-2 country code."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Market string `json:"market"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				return c.GetPlaybackState(ctx, opts...)
			},
		},
		{
			Name:        "transfer_playback",
			Description: "Transfer playback to another device. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"device_id": str("The device ID to transfer playback to (see get_available_devices)."),
				"play":      boolean("Whether playback should start on the new device (true) or stay in its current state (false)."),
			}, "device_id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					DeviceID string `json:"device_id"`
					Play     bool   `json:"play"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.TransferPlayback(ctx, args.DeviceID, args.Play); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "get_available_devices",
			Description: "List the devices the user can currently play Spotify content on. Requires Spotify Premium.",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				return c.GetAvailableDevices(ctx)
			},
		},
		{
			Name:        "get_currently_playing",
			Description: "Get the item the user is playing right now. Returns null when nothing is playing. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"market": str("Optional ISO 3166-1 alpha-2 country code."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Market string `json:"market"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				return c.GetCurrentlyPlaying(ctx, opts...)
			},
		},
		{
			Name:        "play",
			Description: "Start or resume playback. Without inputs it resumes current playback; pass context_uri or uris to play something specific. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"context_uri":     str("Spotify URI of an album, artist or playlist to play. Mutually exclusive with uris."),
				"uris":            strArray("Explicit list of track/episode URIs to play. Mutually exclusive with context_uri."),
				"offset_position": integer("Zero-based index of the item within the context to start with. Only valid with context_uri."),
				"offset_uri":      str("URI of the item within the context to start with. Only valid with context_uri."),
				"position_ms":     integer("Position in milliseconds to seek to within the first played item."),
				"device_id":       deviceProp(),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ContextURI     string   `json:"context_uri"`
					URIs           []string `json:"uris"`
					OffsetPosition *int     `json:"offset_position"`
					OffsetURI      string   `json:"offset_uri"`
					PositionMS     int      `json:"position_ms"`
					DeviceID       string   `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				var play *spotify.PlayOptions
				if args.ContextURI != "" || len(args.URIs) > 0 || args.OffsetPosition != nil || args.OffsetURI != "" || args.PositionMS > 0 {
					play = &spotify.PlayOptions{
						ContextURI: args.ContextURI,
						URIs:       args.URIs,
						PositionMS: args.PositionMS,
					}
					if args.OffsetPosition != nil {
						play.Offset = &spotify.PlayOffset{Position: spotify.Ptr(*args.OffsetPosition)}
					} else if args.OffsetURI != "" {
						play.Offset = &spotify.PlayOffset{URI: args.OffsetURI}
					}
				}
				if err := c.Play(ctx, play, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "pause",
			Description: "Pause playback. Requires Spotify Premium.",
			Schema:      schema(map[string]any{"device_id": deviceProp()}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.Pause(ctx, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "skip_to_next",
			Description: "Skip to the next item in the playback queue. Requires Spotify Premium.",
			Schema:      schema(map[string]any{"device_id": deviceProp()}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SkipToNext(ctx, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "skip_to_previous",
			Description: "Skip to the previous item. Requires Spotify Premium.",
			Schema:      schema(map[string]any{"device_id": deviceProp()}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SkipToPrevious(ctx, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "seek_to_position",
			Description: "Seek to a position (in milliseconds) within the currently playing item. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"position_ms": integer("Position in milliseconds to seek to."),
				"device_id":   deviceProp(),
			}, "position_ms"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					PositionMS int    `json:"position_ms"`
					DeviceID   string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SeekToPosition(ctx, args.PositionMS, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return playbackAfter(ctx, c)
			},
		},
		{
			Name:        "set_repeat",
			Description: "Set the repeat mode: 'off', 'track' (repeat current track) or 'context' (repeat album/playlist). Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"state":     enum("The repeat mode.", "off", "track", "context"),
				"device_id": deviceProp(),
			}, "state"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					State    string `json:"state"`
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SetRepeat(ctx, spotify.RepeatState(args.State), deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "set_volume",
			Description: "Set the playback volume (0-100 percent). Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"volume_percent": integer("Volume as a percentage from 0 to 100."),
				"device_id":      deviceProp(),
			}, "volume_percent"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					VolumePercent int    `json:"volume_percent"`
					DeviceID      string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SetVolume(ctx, args.VolumePercent, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "set_shuffle",
			Description: "Turn shuffle mode on or off. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"state":     boolean("true to enable shuffle, false to disable it."),
				"device_id": deviceProp(),
			}, "state"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					State    bool   `json:"state"`
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.SetShuffle(ctx, args.State, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "get_recently_played",
			Description: "List the tracks the user played most recently, with cursor paging. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"limit":  integer("Maximum number of items to return (max 50)."),
				"after":  integer("Unix millisecond timestamp; return items played after this time. Cannot be combined with before."),
				"before": integer("Unix millisecond timestamp; return items played before this time. Cannot be combined with after."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Limit  int   `json:"limit"`
					After  int64 `json:"after"`
					Before int64 `json:"before"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Limit > 0 {
					opts = append(opts, spotify.Limit(args.Limit))
				}
				if args.After > 0 {
					opts = append(opts, spotify.PlayedAfter(time.UnixMilli(args.After)))
				}
				if args.Before > 0 {
					opts = append(opts, spotify.PlayedBefore(time.UnixMilli(args.Before)))
				}
				return c.GetRecentlyPlayed(ctx, opts...)
			},
		},
		{
			Name:        "get_queue",
			Description: "Get the user's playback queue: the currently playing item and what comes next. Requires Spotify Premium.",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				return c.GetQueue(ctx)
			},
		},
		{
			Name:        "add_to_queue",
			Description: "Append a track or episode URI to the user's playback queue. Requires Spotify Premium.",
			Schema: schema(map[string]any{
				"uri":       str("The Spotify URI of the track or episode to queue."),
				"device_id": deviceProp(),
			}, "uri"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					URI      string `json:"uri"`
					DeviceID string `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.AddToQueue(ctx, args.URI, deviceOpt(args.DeviceID)...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "add_tracks_to_queue",
			Description: "Append several tracks/episodes to the playback queue in order, with one call instead of many. Requires Spotify Premium. Reports how many were queued; if one fails the rest are skipped so the order stays intact.",
			Schema: schema(map[string]any{
				"uris":      strArray("Spotify track/episode URIs to queue, in the order they should play."),
				"device_id": deviceProp(),
			}, "uris"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					URIs     []string `json:"uris"`
					DeviceID string   `json:"device_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				opts := deviceOpt(args.DeviceID)
				for i, uri := range args.URIs {
					if err := c.AddToQueue(ctx, uri, opts...); err != nil {
						return map[string]any{
							"queued": i,
							"total":  len(args.URIs),
							"error":  "stopped at position " + uri + ": " + err.Error(),
						}, nil
					}
				}
				return map[string]any{"status": "ok", "queued": len(args.URIs)}, nil
			},
		},
	}
}
