package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// followTypeProps are the shared inputs of the follow/unfollow/is_following
// tools.
func followTypeProps() map[string]any {
	return map[string]any{
		"type": enum("Whether the IDs refer to artists or users.", "artist", "user"),
		"ids":  strArray("Spotify artist or user IDs (max 50)."),
	}
}

// topProps are the shared inputs of the get_top_artists/get_top_tracks tools.
func topProps() map[string]any {
	return map[string]any{
		"time_range": enum("Time window: short_term (~4 weeks), medium_term (~6 months, default) or long_term (~1 year).", "short_term", "medium_term", "long_term"),
		"limit":      integer("Maximum number of items to return (default set by Spotify, max usually 50)."),
		"offset":     integer("Index of the first item to return, for paging."),
	}
}

// topArgs are the decoded inputs of the get_top_artists/get_top_tracks tools.
type topArgs struct {
	TimeRange string `json:"time_range"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
}

func (a topArgs) opts() []spotify.RequestOption {
	var opts []spotify.RequestOption
	if a.TimeRange != "" {
		opts = append(opts, spotify.TimeRange(spotify.Range(a.TimeRange)))
	}
	if a.Limit > 0 {
		opts = append(opts, spotify.Limit(a.Limit))
	}
	if a.Offset > 0 {
		opts = append(opts, spotify.Offset(a.Offset))
	}
	return opts
}

func userTools() []Tool {
	return []Tool{
		{
			Name:        "get_current_user",
			Description: "Get the profile of the current user. Since February 2026 development-mode apps only receive the display name, ID and images (country, email and subscription level are withheld).",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				return c.GetCurrentUser(ctx)
			},
		},
		{
			Name:        "get_user",
			Description: "Get the public profile of any Spotify user by their user ID. Removed from development-mode apps by Spotify in February 2026; fails with 403 unless the app has extended quota.",
			Schema:      schema(map[string]any{"user_id": str("The Spotify user ID.")}, "user_id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					UserID string `json:"user_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetUser(ctx, args.UserID)
			},
		},
		{
			Name:        "get_top_artists",
			Description: "List the current user's most listened-to artists over a chosen time window, paged.",
			Schema:      schema(topProps()),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[topArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetTopArtists(ctx, args.opts()...)
			},
		},
		{
			Name:        "get_top_tracks",
			Description: "List the current user's most listened-to tracks over a chosen time window, paged.",
			Schema:      schema(topProps()),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[topArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetTopTracks(ctx, args.opts()...)
			},
		},
		{
			Name:        "follow_playlist",
			Description: "Make the current user follow a playlist. 'public' controls whether it appears on the user's profile.",
			Schema: schema(map[string]any{
				"playlist_id": str("The Spotify ID of the playlist to follow."),
				"public":      boolean("Whether the followed playlist appears publicly on the user's profile."),
			}, "playlist_id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					PlaylistID string `json:"playlist_id"`
					Public     bool   `json:"public"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.FollowPlaylist(ctx, args.PlaylistID, args.Public); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "unfollow_playlist",
			Description: "Make the current user unfollow a playlist.",
			Schema:      schema(map[string]any{"playlist_id": str("The Spotify ID of the playlist to unfollow.")}, "playlist_id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					PlaylistID string `json:"playlist_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.UnfollowPlaylist(ctx, args.PlaylistID); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "get_followed_artists",
			Description: "List the artists the current user follows, cursor-paged. Pass the last artist ID of a page as 'after' to fetch the next page.",
			Schema: schema(map[string]any{
				"limit": integer("Maximum number of artists to return (default set by Spotify, max usually 50)."),
				"after": str("Cursor: the last artist ID retrieved from the previous page."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Limit int    `json:"limit"`
					After string `json:"after"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Limit > 0 {
					opts = append(opts, spotify.Limit(args.Limit))
				}
				if args.After != "" {
					opts = append(opts, spotify.After(args.After))
				}
				return c.GetFollowedArtists(ctx, opts...)
			},
		},
		{
			Name:        "follow",
			Description: "Make the current user follow up to 50 artists (stored in the library since February 2026). Following users is no longer supported by the Spotify API.",
			Schema:      schema(followTypeProps(), "type", "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Type string   `json:"type"`
					IDs  []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.Follow(ctx, spotify.FollowType(args.Type), args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "unfollow",
			Description: "Make the current user unfollow up to 50 artists. Unfollowing users is no longer supported by the Spotify API.",
			Schema:      schema(followTypeProps(), "type", "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Type string   `json:"type"`
					IDs  []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.Unfollow(ctx, spotify.FollowType(args.Type), args.IDs...); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name:        "is_following",
			Description: "Check whether the current user follows up to 50 artists. Returns a boolean per ID, in order. Checking users is no longer supported by the Spotify API.",
			Schema:      schema(followTypeProps(), "type", "ids"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Type string   `json:"type"`
					IDs  []string `json:"ids"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.IsFollowing(ctx, spotify.FollowType(args.Type), args.IDs...)
			},
		},
		{
			Name:        "current_user_follows_playlist",
			Description: "Check whether the current user follows the given playlist. Returns a single boolean. Removed from development-mode apps by Spotify in February 2026; fails with 403 unless the app has extended quota.",
			Schema:      schema(map[string]any{"playlist_id": str("The Spotify ID of the playlist to check.")}, "playlist_id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					PlaylistID string `json:"playlist_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CurrentUserFollowsPlaylist(ctx, args.PlaylistID)
			},
		},
	}
}
