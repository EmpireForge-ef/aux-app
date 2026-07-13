package aitools

import (
	"context"
	"encoding/json"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// TempPlaylists tracks playlists the AI created as throwaway, editable
// "queues". Edits to a tracked temp playlist skip the confirmation gate.
type TempPlaylists interface {
	Add(id string)
	Has(id string) bool
	Remove(id string)
}

type tempKey struct{}

// WithTempPlaylists attaches the temp-playlist registry to a context so tool
// handlers can register/forget temp playlists.
func WithTempPlaylists(ctx context.Context, tp TempPlaylists) context.Context {
	return context.WithValue(ctx, tempKey{}, tp)
}

func tempFromContext(ctx context.Context) TempPlaylists {
	tp, _ := ctx.Value(tempKey{}).(TempPlaylists)
	return tp
}

func tempTools() []Tool {
	return []Tool{
		{
			Name:        "create_temp_playlist",
			Description: "Create a throwaway playlist to use as an editable queue. Spotify's real queue cannot be reordered, cleared, or have items removed once added, so when the user wants a queue they can change, create a temp playlist here, add tracks to it, and play it (play with context_uri = the returned uri). Edits to a temp playlist (replace_playlist_items, remove_playlist_items) do NOT prompt the user for confirmation. Reuse the temp playlist you created earlier in the conversation instead of making a new one each time; delete it with delete_temp_playlist when done.",
			Schema: schema(map[string]any{
				"name": str("Optional name for the temp playlist. Defaults to 'Aux Queue'."),
			}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					Name string `json:"name"`
				}](input)
				if err != nil {
					return nil, err
				}
				name := args.Name
				if name == "" {
					name = "Aux Queue"
				}
				pl, err := c.CreatePlaylist(ctx, spotify.PlaylistDetails{
					Name:        name,
					Public:      spotify.Ptr(false),
					Description: "Temporary editable queue created by Aux.",
				})
				if err != nil {
					return nil, err
				}
				if tp := tempFromContext(ctx); tp != nil {
					tp.Add(pl.ID)
				}
				return map[string]any{"id": pl.ID, "uri": pl.URI, "name": pl.Name, "temp": true}, nil
			},
		},
		{
			Name:        "delete_temp_playlist",
			Description: "Delete a temp playlist created with create_temp_playlist (unfollows it, which removes it for the user). No confirmation is asked since it is a throwaway playlist.",
			Schema:      schema(map[string]any{"id": str("The Spotify ID of the temp playlist to delete.")}, "id"),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				args, err := decode[struct {
					ID string `json:"id"`
				}](input)
				if err != nil {
					return nil, err
				}
				if err := c.UnfollowPlaylist(ctx, args.ID); err != nil {
					return nil, err
				}
				if tp := tempFromContext(ctx); tp != nil {
					tp.Remove(args.ID)
				}
				return ok(), nil
			},
		},
	}
}

// AddedTrackURIs returns the track/episode URIs a tool call adds to the queue
// or a playlist, so they can be recorded as "recently used" and not repeated.
// Non-adding tools return nil.
func AddedTrackURIs(name string, input json.RawMessage) []string {
	switch name {
	case "add_to_queue":
		var a struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(input, &a) == nil && a.URI != "" {
			return []string{a.URI}
		}
	case "add_tracks_to_queue", "add_items_to_playlist", "replace_playlist_items":
		var a struct {
			URIs []string `json:"uris"`
		}
		if json.Unmarshal(input, &a) == nil {
			return a.URIs
		}
	}
	return nil
}

// IsTempPlaylistEdit reports whether a destructive tool call targets a tracked
// temp playlist, in which case the confirmation gate can be skipped.
func IsTempPlaylistEdit(tp TempPlaylists, name string, input json.RawMessage) bool {
	if tp == nil {
		return false
	}
	switch name {
	case "remove_playlist_items", "replace_playlist_items", "unfollow_playlist":
	default:
		return false
	}
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return false
	}
	return tp.Has(args.ID)
}
