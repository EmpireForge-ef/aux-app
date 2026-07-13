package aitools

import (
	"fmt"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// This file projects the wrapper's verbose response objects down to the few
// fields the model actually needs (name, artists, uri, album, duration, ...).
// Slim is applied centrally to every tool result before it is marshalled, so
// results are far smaller — cheaper in tokens, faster, and no longer
// truncated mid-object.

type slimTrack struct {
	Name     string   `json:"name"`
	Artists  []string `json:"artists,omitempty"`
	Album    string   `json:"album,omitempty"`
	URI      string   `json:"uri,omitempty"`
	ID       string   `json:"id,omitempty"`
	Duration string   `json:"duration,omitempty"`
	Explicit bool     `json:"explicit,omitempty"`
}

type slimArtist struct {
	Name   string   `json:"name"`
	URI    string   `json:"uri,omitempty"`
	ID     string   `json:"id,omitempty"`
	Genres []string `json:"genres,omitempty"`
}

type slimAlbum struct {
	Name    string   `json:"name"`
	Artists []string `json:"artists,omitempty"`
	URI     string   `json:"uri,omitempty"`
	ID      string   `json:"id,omitempty"`
	Type    string   `json:"type,omitempty"`
	Tracks  int      `json:"tracks,omitempty"`
}

type slimPlaylist struct {
	Name   string `json:"name"`
	Owner  string `json:"owner,omitempty"`
	URI    string `json:"uri,omitempty"`
	ID     string `json:"id,omitempty"`
	Tracks int    `json:"tracks,omitempty"`
}

// mmss renders a millisecond duration as "m:ss".
func mmss(ms int) string {
	if ms <= 0 {
		return ""
	}
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func artistNames(as []spotify.SimplifiedArtist) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func slimTrackOf(t spotify.Track) slimTrack {
	return slimTrack{
		Name:     t.Name,
		Artists:  artistNames(t.Artists),
		Album:    t.Album.Name,
		URI:      t.URI,
		ID:       t.ID,
		Duration: mmss(t.DurationMS),
		Explicit: t.Explicit,
	}
}

func slimSimplifiedTrackOf(t spotify.SimplifiedTrack) slimTrack {
	return slimTrack{
		Name:     t.Name,
		Artists:  artistNames(t.Artists),
		URI:      t.URI,
		ID:       t.ID,
		Duration: mmss(t.DurationMS),
		Explicit: t.Explicit,
	}
}

func slimArtistOf(a spotify.Artist) slimArtist {
	return slimArtist{Name: a.Name, URI: a.URI, ID: a.ID, Genres: a.Genres}
}

func slimAlbumOf(a spotify.SimplifiedAlbum) slimAlbum {
	return slimAlbum{
		Name:    a.Name,
		Artists: artistNames(a.Artists),
		URI:     a.URI,
		ID:      a.ID,
		Type:    a.AlbumType,
		Tracks:  a.TotalTracks,
	}
}

func slimPlaylistOf(p spotify.SimplifiedPlaylist) slimPlaylist {
	return slimPlaylist{Name: p.Name, Owner: p.Owner.DisplayName, URI: p.URI, ID: p.ID, Tracks: p.Items.Total}
}

// slimPlayable projects a track/episode union to a slim map, or nil.
func slimPlayable(it spotify.PlayableItem) any {
	switch {
	case it.Track != nil:
		return slimTrackOf(*it.Track)
	case it.Episode != nil:
		return map[string]any{"name": it.Episode.Name, "uri": it.Episode.URI, "id": it.Episode.ID, "type": "episode"}
	default:
		return nil
	}
}

// page wraps slimmed items with the total and a has_more hint, dropping the
// bulky href/next URLs the model doesn't need.
func page(items []any, total int, next string) map[string]any {
	m := map[string]any{"items": items, "total": total}
	if next != "" {
		m["has_more"] = true
	}
	return m
}

func mapSlice[T any](in []T, f func(T) any) []any {
	out := make([]any, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

// Slim projects a raw tool result to its compact form. Unrecognised types are
// returned unchanged.
func Slim(v any) any {
	switch t := v.(type) {
	case *spotify.Track:
		return slimTrackOf(*t)
	case []*spotify.Track:
		return mapSlice(t, func(x *spotify.Track) any { return slimTrackOf(*x) })
	case *spotify.Artist:
		return slimArtistOf(*t)
	case []*spotify.Artist:
		return mapSlice(t, func(x *spotify.Artist) any { return slimArtistOf(*x) })
	case []spotify.Artist:
		return mapSlice(t, func(x spotify.Artist) any { return slimArtistOf(x) })
	case *spotify.Album:
		a := slimAlbumOf(t.SimplifiedAlbum)
		return map[string]any{"album": a, "tracks": mapSlice(t.Tracks.Items, func(x spotify.SimplifiedTrack) any { return slimSimplifiedTrackOf(x) })}
	case *spotify.Paging[spotify.Track]:
		return page(mapSlice(t.Items, func(x spotify.Track) any { return slimTrackOf(x) }), t.Total, t.Next)
	case *spotify.Paging[spotify.SimplifiedTrack]:
		return page(mapSlice(t.Items, func(x spotify.SimplifiedTrack) any { return slimSimplifiedTrackOf(x) }), t.Total, t.Next)
	case *spotify.Paging[spotify.SavedTrack]:
		return page(mapSlice(t.Items, func(x spotify.SavedTrack) any { return slimTrackOf(x.Track) }), t.Total, t.Next)
	case *spotify.Paging[spotify.Artist]:
		return page(mapSlice(t.Items, func(x spotify.Artist) any { return slimArtistOf(x) }), t.Total, t.Next)
	case *spotify.Paging[spotify.SimplifiedAlbum]:
		return page(mapSlice(t.Items, func(x spotify.SimplifiedAlbum) any { return slimAlbumOf(x) }), t.Total, t.Next)
	case *spotify.Paging[spotify.SavedAlbum]:
		return page(mapSlice(t.Items, func(x spotify.SavedAlbum) any { return slimAlbumOf(x.Album.SimplifiedAlbum) }), t.Total, t.Next)
	case *spotify.Paging[spotify.SimplifiedPlaylist]:
		return page(mapSlice(t.Items, func(x spotify.SimplifiedPlaylist) any { return slimPlaylistOf(x) }), t.Total, t.Next)
	case *spotify.Paging[spotify.PlaylistItem]:
		return page(mapSlice(t.Items, func(x spotify.PlaylistItem) any { return slimPlayable(x.Item) }), t.Total, t.Next)
	case *spotify.CursorPaging[spotify.Artist]:
		return page(mapSlice(t.Items, func(x spotify.Artist) any { return slimArtistOf(x) }), t.Total, t.Next)
	case *spotify.CursorPaging[spotify.PlayHistory]:
		return page(mapSlice(t.Items, func(x spotify.PlayHistory) any { return slimTrackOf(x.Track) }), t.Total, t.Next)
	case *spotify.SearchResults:
		return slimSearch(t)
	case *spotify.Playlist:
		return map[string]any{
			"name":        t.Name,
			"owner":       t.Owner.DisplayName,
			"id":          t.ID,
			"uri":         t.URI,
			"description": t.Description,
			"total":       t.Items.Total,
			"items":       mapSlice(t.Items.Items, func(x spotify.PlaylistItem) any { return slimPlayable(x.Item) }),
		}
	case *spotify.PlaybackState:
		return slimPlayback(t)
	case *spotify.CurrentlyPlaying:
		return slimCurrently(t)
	case *spotify.Queue:
		return map[string]any{
			"now_playing": slimPlayable(t.CurrentlyPlaying),
			"up_next":     mapSlice(t.Queue, func(x spotify.PlayableItem) any { return slimPlayable(x) }),
		}
	case []spotify.Device:
		return mapSlice(t, func(d spotify.Device) any {
			return map[string]any{"id": d.ID, "name": d.Name, "type": d.Type, "active": d.IsActive, "volume": d.VolumePercent}
		})
	default:
		return v
	}
}

func slimSearch(r *spotify.SearchResults) map[string]any {
	out := map[string]any{}
	if r.Tracks != nil {
		out["tracks"] = mapSlice(r.Tracks.Items, func(x spotify.Track) any { return slimTrackOf(x) })
	}
	if r.Artists != nil {
		out["artists"] = mapSlice(r.Artists.Items, func(x spotify.Artist) any { return slimArtistOf(x) })
	}
	if r.Albums != nil {
		out["albums"] = mapSlice(r.Albums.Items, func(x spotify.SimplifiedAlbum) any { return slimAlbumOf(x) })
	}
	if r.Playlists != nil {
		out["playlists"] = mapSlice(r.Playlists.Items, func(x spotify.SimplifiedPlaylist) any { return slimPlaylistOf(x) })
	}
	return out
}

func slimCurrently(c *spotify.CurrentlyPlaying) map[string]any {
	return map[string]any{
		"is_playing": c.IsPlaying,
		"item":       slimPlayable(c.Item),
		"progress":   mmss(c.ProgressMS),
	}
}

func slimPlayback(p *spotify.PlaybackState) map[string]any {
	m := slimCurrently(&p.CurrentlyPlaying)
	m["device"] = map[string]any{"name": p.Device.Name, "type": p.Device.Type, "volume": p.Device.VolumePercent}
	m["repeat"] = p.RepeatState
	m["shuffle"] = p.ShuffleState
	return m
}
