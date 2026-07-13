package aitools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

func playlistTools() []Tool {
	return []Tool{
		{
			Name:        "get_playlist",
			Description: "Get a playlist owned by any Spotify user, including its metadata and the first page of items.",
			Schema: schema(map[string]any{
				"id":     str("The Spotify ID of the playlist."),
				"market": str("Optional ISO 3166-1 alpha-2 country code."),
				"fields": str("Optional Spotify fields filter, e.g. 'items(added_at,track(name,href))'."),
			}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID     string `json:"id"`
					Market string `json:"market"`
					Fields string `json:"fields"`
				}](input)
				if err != nil {
					return nil, err
				}
				var opts []spotify.RequestOption
				if args.Market != "" {
					opts = append(opts, spotify.Market(args.Market))
				}
				if args.Fields != "" {
					opts = append(opts, spotify.Fields(args.Fields))
				}
				return c.GetPlaylist(ctx, args.ID, opts...)
			},
		},
		{
			Name:        "change_playlist_details",
			Description: "Change a playlist's name, description, public or collaborative flags. Use this to rename or re-describe a playlist; omitted fields are left untouched.",
			Schema: schema(map[string]any{
				"id":            str("The Spotify ID of the playlist."),
				"name":          str("New playlist name."),
				"description":   str("New playlist description, shown on the playlist page."),
				"public":        boolean("Whether the playlist appears on the owner's profile."),
				"collaborative": boolean("Whether other users may modify the playlist. Only allowed on private playlists."),
			}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID            string `json:"id"`
					Name          string `json:"name"`
					Description   string `json:"description"`
					Public        *bool  `json:"public"`
					Collaborative *bool  `json:"collaborative"`
				}](input)
				if err != nil {
					return nil, err
				}
				details := spotify.PlaylistDetails{
					Name:          args.Name,
					Description:   args.Description,
					Public:        args.Public,
					Collaborative: args.Collaborative,
				}
				if err := c.ChangePlaylistDetails(ctx, args.ID, details); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
		{
			Name: "get_playlist_items",
			Description: "List the items (tracks or episodes) of a playlist, paged. Use this to inspect a playlist's contents before editing it. " +
				"Contents are only available for playlists the user owns or collaborates on; Spotify withholds them for other playlists (including Spotify-made ones).",
			Schema: schema(pageProps(map[string]any{
				"id":     str("The Spotify ID of the playlist."),
				"fields": str("Optional Spotify fields filter, e.g. 'items(added_at,item(name,uri))'."),
			}), "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					pageArgs
					ID     string `json:"id"`
					Fields string `json:"fields"`
				}](input)
				if err != nil {
					return nil, err
				}
				opts := args.opts()
				if args.Fields != "" {
					opts = append(opts, spotify.Fields(args.Fields))
				}
				items, err := c.GetPlaylistItems(ctx, args.ID, opts...)
				if isForbidden(err) {
					return nil, fmt.Errorf("Spotify withholds the contents of playlists the user neither owns nor collaborates on (this is an app-level restriction, not a token problem): %w", err)
				}
				return items, err
			},
		},
		{
			Name:        "add_items_to_playlist",
			Description: "Add up to 100 items to a playlist by their full Spotify URIs (e.g. spotify:track:4iV5W9uYEdYUVa79Axb7Rh). Appends by default; pass 'position' to insert at a zero-based index. Tracks already in the playlist are skipped automatically (Spotify has no 'is it already there?' endpoint, so the app checks for you and caches the contents so it only reads the playlist once). Set allow_duplicates true to add anyway. Returns how many were added vs skipped and the new snapshot ID.",
			Schema: schema(map[string]any{
				"id":               str("The Spotify ID of the playlist."),
				"uris":             strArray("Full Spotify track or episode URIs to add, e.g. spotify:track:4iV5W9uYEdYUVa79Axb7Rh."),
				"position":         integer("Optional zero-based position to insert the items at; omit to append."),
				"allow_duplicates": boolean("Add items even if they are already in the playlist. Defaults to false (duplicates skipped)."),
			}, "id", "uris"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID              string   `json:"id"`
					URIs            []string `json:"uris"`
					Position        *int     `json:"position"`
					AllowDuplicates bool     `json:"allow_duplicates"`
				}](input)
				if err != nil {
					return nil, err
				}

				toAdd := args.URIs
				var skipped []string
				var existing []string
				if !args.AllowDuplicates {
					existing, err = currentPlaylistURIs(ctx, c, args.ID)
					if err != nil {
						return nil, err
					}
					have := toSet(existing)
					toAdd = toAdd[:0]
					for _, u := range args.URIs {
						if have[u] {
							skipped = append(skipped, u)
						} else {
							toAdd = append(toAdd, u)
						}
					}
				}

				result := map[string]any{"added": len(toAdd), "skipped": len(skipped)}
				if len(skipped) > 0 {
					result["skipped_uris"] = skipped
				}
				if len(toAdd) == 0 {
					return result, nil // nothing new to add
				}

				var opts []spotify.RequestOption
				if args.Position != nil {
					opts = append(opts, spotify.AtPosition(*args.Position))
				}
				id, err := c.AddItemsToPlaylist(ctx, args.ID, toAdd, opts...)
				if err != nil {
					return nil, err
				}
				recacheAfterMutation(ctx, c, args.ID, append(existing, toAdd...))
				result["snapshot_id"] = id
				return result, nil
			},
		},
		{
			Name:        "replace_playlist_items",
			Confirm:     "Replace the entire playlist contents? This clears the current items.",
			Description: "Replace the entire contents of a playlist with up to 100 items given as full Spotify URIs (e.g. spotify:track:xyz). Pass an empty list to clear the playlist. Returns the new snapshot ID.",
			Schema: schema(map[string]any{
				"id":   str("The Spotify ID of the playlist."),
				"uris": strArray("Full Spotify track or episode URIs that become the playlist's new contents; empty clears the playlist."),
			}, "id", "uris"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID   string   `json:"id"`
					URIs []string `json:"uris"`
				}](input)
				if err != nil {
					return nil, err
				}
				id, err := c.ReplacePlaylistItems(ctx, args.ID, args.URIs...)
				if err != nil {
					return nil, err
				}
				recacheAfterMutation(ctx, c, args.ID, args.URIs) // new contents are exactly these
				return map[string]any{"snapshot_id": id}, nil
			},
		},
		{
			Name:        "reorder_playlist_items",
			Description: "Move a range of items to a new position within a playlist. Use this to change track order without removing and re-adding items. Returns the new snapshot ID.",
			Schema: schema(map[string]any{
				"id":            str("The Spotify ID of the playlist."),
				"range_start":   integer("Zero-based position of the first item to move."),
				"insert_before": integer("Zero-based position the items should be moved to (they are inserted before this index)."),
				"range_length":  integer("Number of consecutive items to move. Defaults to 1."),
				"snapshot_id":   str("Optional snapshot ID pinning the playlist version to operate on."),
			}, "id", "range_start", "insert_before"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID           string `json:"id"`
					RangeStart   int    `json:"range_start"`
					InsertBefore int    `json:"insert_before"`
					RangeLength  int    `json:"range_length"`
					SnapshotID   string `json:"snapshot_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				id, err := c.ReorderPlaylistItems(ctx, args.ID, spotify.ReorderPlaylistItemsRequest{
					RangeStart:   args.RangeStart,
					InsertBefore: args.InsertBefore,
					RangeLength:  args.RangeLength,
					SnapshotID:   args.SnapshotID,
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"snapshot_id": id}, nil
			},
		},
		{
			Name:        "remove_playlist_items",
			Confirm:     "Remove these items from the playlist?",
			Description: "Remove all occurrences of up to 100 items from a playlist by their full Spotify URIs (e.g. spotify:track:xyz). Returns the new snapshot ID.",
			Schema: schema(map[string]any{
				"id":          str("The Spotify ID of the playlist."),
				"uris":        strArray("Full Spotify track or episode URIs to remove; every occurrence is removed."),
				"snapshot_id": str("Optional snapshot ID pinning the playlist version to operate on; omit to target the latest version."),
			}, "id", "uris"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID         string   `json:"id"`
					URIs       []string `json:"uris"`
					SnapshotID string   `json:"snapshot_id"`
				}](input)
				if err != nil {
					return nil, err
				}
				id, err := c.RemovePlaylistItems(ctx, args.ID, args.URIs, args.SnapshotID)
				if err != nil {
					return nil, err
				}
				if pc := cacheFromContext(ctx); pc != nil {
					pc.Invalidate(args.ID) // contents unknown here; re-fetch next time
				}
				return map[string]any{"snapshot_id": id}, nil
			},
		},
		{
			Name:        "get_current_user_playlists",
			Description: "List the playlists owned or followed by the current user, paged. Use this first to find the playlist the user wants to edit.",
			Schema: schema(map[string]any{
				"limit":  integer("Maximum number of items to return (default set by Spotify, max usually 50)."),
				"offset": integer("Index of the first item to return, for paging."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[pageArgs](input)
				if err != nil {
					return nil, err
				}
				return c.GetCurrentUserPlaylists(ctx, args.opts()...)
			},
		},
		{
			Name:        "create_playlist",
			Description: "Create a new empty playlist for the current user. Add tracks afterwards with add_items_to_playlist.",
			Schema: schema(map[string]any{
				"name":          str("Name of the new playlist."),
				"description":   str("Optional playlist description."),
				"public":        boolean("Whether the playlist appears on the owner's profile."),
				"collaborative": boolean("Whether other users may modify the playlist. Only allowed on private playlists."),
			}, "name"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Name          string `json:"name"`
					Description   string `json:"description"`
					Public        *bool  `json:"public"`
					Collaborative *bool  `json:"collaborative"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.CreatePlaylist(ctx, spotify.PlaylistDetails{
					Name:          args.Name,
					Description:   args.Description,
					Public:        args.Public,
					Collaborative: args.Collaborative,
				})
			},
		},
		{
			Name:        "get_playlist_cover_image",
			Description: "Get the current cover art images of a playlist.",
			Schema:      schema(map[string]any{"id": str("The Spotify ID of the playlist.")}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
				}](input)
				if err != nil {
					return nil, err
				}
				return c.GetPlaylistCoverImage(ctx, args.ID)
			},
		},
		{
			Name:        "set_playlist_cover_image",
			Description: "Upload a custom cover image for a playlist. The image must be a JPEG of at most 256KB, supplied base64-encoded; requires the ugc-image-upload scope.",
			Schema: schema(map[string]any{
				"id":          str("The Spotify ID of the playlist."),
				"jpeg_base64": str("The JPEG image data, base64-encoded. Raw image must be at most 256KB."),
			}, "id", "jpeg_base64"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID         string `json:"id"`
					JPEGBase64 string `json:"jpeg_base64"`
				}](input)
				if err != nil {
					return nil, err
				}
				jpeg, err := base64.StdEncoding.DecodeString(args.JPEGBase64)
				if err != nil {
					return nil, fmt.Errorf("invalid jpeg_base64: %w", err)
				}
				if err := c.SetPlaylistCoverImage(ctx, args.ID, jpeg); err != nil {
					return nil, err
				}
				return ok(), nil
			},
		},
	}
}
