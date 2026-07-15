package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// TempPlaylists tracks playlists the AI treats as throwaway, editable "queues"
// (the weekday queues). Edits to a tracked queue skip the confirmation gate.
type TempPlaylists interface {
	Add(id string)
	Has(id string) bool
	Remove(id string)
}

// WeekdayQueues stores the reusable per-weekday queue playlists.
type WeekdayQueues interface {
	WeekdayQueue(weekday int) (playlistID, lastUsed string, ok bool)
	SetWeekdayQueue(weekday int, playlistID, name, lastUsed string)
	MarkWeekdayUsed(weekday int, lastUsed string)
}

type tempKey struct{}
type weekdayKey struct{}
type nowKey struct{}

// WithTempPlaylists attaches the temp-playlist registry to a context.
func WithTempPlaylists(ctx context.Context, tp TempPlaylists) context.Context {
	return context.WithValue(ctx, tempKey{}, tp)
}

func tempFromContext(ctx context.Context) TempPlaylists {
	tp, _ := ctx.Value(tempKey{}).(TempPlaylists)
	return tp
}

// WithWeekdayQueues attaches the weekday-queue store to a context.
func WithWeekdayQueues(ctx context.Context, wq WeekdayQueues) context.Context {
	return context.WithValue(ctx, weekdayKey{}, wq)
}

func weekdayFromContext(ctx context.Context) WeekdayQueues {
	wq, _ := ctx.Value(weekdayKey{}).(WeekdayQueues)
	return wq
}

// WithLocalNow attaches the current local time (in the user's timezone), used
// to pick and reset the weekday queue.
func WithLocalNow(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, t)
}

func localNow(ctx context.Context) time.Time {
	if t, ok := ctx.Value(nowKey{}).(time.Time); ok && !t.IsZero() {
		return t
	}
	return time.Now()
}

func tempTools() []Tool {
	return []Tool{
		{
			Name:        "get_daily_queue",
			Description: "Get today's reusable queue playlist. There is one per weekday ('Aux Queue · Monday', etc.); the app automatically clears it the first time it's used in a new week, so the user keeps roughly a week to save favourites into their own playlists before they're cleared. Use this whenever the user wants to play or queue songs, build a listening session, or a queue they can edit — do NOT create new playlists for that (they pile up). Add tracks with add_items_to_playlist and play it with play(context_uri = the returned uri). Removing/replacing items in it needs no confirmation since it's a queue. Only use create_playlist for a dedicated, named playlist the user explicitly asks to keep.",
			Schema:      schema(map[string]any{}),
			Handler: func(ctx context.Context, c *spotify.Client, input json.RawMessage) (any, error) {
				wq := weekdayFromContext(ctx)
				if wq == nil {
					return nil, errors.New("weekday queues are unavailable")
				}
				now := localNow(ctx)
				weekday := int(now.Weekday())
				today := now.Format("2006-01-02")
				name := "Aux Queue · " + now.Weekday().String()
				desc := "Weekly editable queue by Aux — cleared each new " + now.Weekday().String() + "."

				id, lastUsed, ok := wq.WeekdayQueue(weekday)
				var uri string
				needCreate := !ok
				if ok {
					// Confirm it still exists (the user may have deleted it).
					if pl, err := c.GetPlaylist(ctx, id, spotify.Fields("id,uri,name")); err != nil {
						needCreate = true
					} else {
						uri, name = pl.URI, pl.Name
					}
				}

				create := func() error {
					pl, err := c.CreatePlaylist(ctx, spotify.PlaylistDetails{
						Name:        name,
						Public:      spotify.Ptr(false),
						Description: desc,
					})
					if err != nil {
						return err
					}
					id, uri = pl.ID, pl.URI
					wq.SetWeekdayQueue(weekday, id, name, today)
					return nil
				}

				wasReset := false
				switch {
				case needCreate:
					if err := create(); err != nil {
						return nil, err
					}
				case lastUsed != today:
					// First use this week for this weekday — clear last week's songs.
					if _, err := c.ReplacePlaylistItems(ctx, id); err != nil {
						if err := create(); err != nil { // playlist gone — recreate
							return nil, err
						}
					} else {
						wq.MarkWeekdayUsed(weekday, today)
					}
					wasReset = true
				}

				// Register it so queue edits (remove/replace items) skip the
				// confirmation gate.
				if tp := tempFromContext(ctx); tp != nil {
					tp.Add(id)
				}
				return map[string]any{
					"id": id, "uri": uri, "name": name,
					"weekday": now.Weekday().String(), "was_reset": wasReset,
				}, nil
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

// IsTempPlaylistEdit reports whether a destructive tool call edits a tracked
// queue playlist, in which case the confirmation gate is skipped. Only item
// edits qualify — deleting (unfollowing) a queue still asks, since the weekday
// queues are persistent.
func IsTempPlaylistEdit(tp TempPlaylists, name string, input json.RawMessage) bool {
	if tp == nil {
		return false
	}
	switch name {
	case "remove_playlist_items", "replace_playlist_items":
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
