package aitools

import (
	"context"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"
)

// PlaylistCache remembers a playlist's track URIs keyed by its snapshot_id, so
// the AI can dedupe before adding without re-fetching the whole playlist each
// time.
type PlaylistCache interface {
	Contents(id, snapshot string) (uris []string, ok bool)
	Store(id, snapshot string, uris []string)
	Invalidate(id string)
}

type cacheKey struct{}

// WithPlaylistCache attaches the playlist-contents cache to a context.
func WithPlaylistCache(ctx context.Context, pc PlaylistCache) context.Context {
	return context.WithValue(ctx, cacheKey{}, pc)
}

func cacheFromContext(ctx context.Context) PlaylistCache {
	pc, _ := ctx.Value(cacheKey{}).(PlaylistCache)
	return pc
}

// playableURI extracts the track/episode URI of a playlist item.
func playableURI(it spotify.PlaylistItem) string {
	switch {
	case it.Item.Track != nil:
		return it.Item.Track.URI
	case it.Item.Episode != nil:
		return it.Item.Episode.URI
	}
	return ""
}

// fetchAllPlaylistURIs pages through a playlist's items and returns every URI.
func fetchAllPlaylistURIs(ctx context.Context, c *spotify.Client, id string) ([]string, error) {
	var uris []string
	for offset := 0; ; offset += 100 {
		p, err := c.GetPlaylistItems(ctx, id, spotify.Limit(100), spotify.Offset(offset))
		if err != nil {
			return nil, err
		}
		for _, it := range p.Items {
			if u := playableURI(it); u != "" {
				uris = append(uris, u)
			}
		}
		if len(p.Items) == 0 || offset+len(p.Items) >= p.Total {
			break
		}
	}
	return uris, nil
}

// playlistSnapshot fetches just the playlist's current snapshot_id — a tiny
// request used to validate the cache.
func playlistSnapshot(ctx context.Context, c *spotify.Client, id string) (string, error) {
	pl, err := c.GetPlaylist(ctx, id, spotify.Fields("snapshot_id"))
	if err != nil {
		return "", err
	}
	return pl.SnapshotID, nil
}

// currentPlaylistURIs returns the URIs a playlist currently contains, using
// the cache when its snapshot still matches and only fetching the full item
// list on a miss (first time, or the playlist was edited — even outside Aux).
func currentPlaylistURIs(ctx context.Context, c *spotify.Client, id string) (uris []string, err error) {
	snapshot, err := playlistSnapshot(ctx, c, id)
	if err != nil {
		return nil, err
	}
	cache := cacheFromContext(ctx)
	if cache != nil {
		if cached, ok := cache.Contents(id, snapshot); ok {
			return cached, nil
		}
	}
	uris, err = fetchAllPlaylistURIs(ctx, c, id)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		cache.Store(id, snapshot, uris)
	}
	return uris, nil
}

// recacheAfterMutation stores the playlist's new contents against its current
// canonical snapshot (which the mutation response does not give us), so the
// next dedup is a snapshot check with no full re-fetch. Best-effort.
func recacheAfterMutation(ctx context.Context, c *spotify.Client, id string, contents []string) {
	cache := cacheFromContext(ctx)
	if cache == nil {
		return
	}
	snapshot, err := playlistSnapshot(ctx, c, id)
	if err != nil {
		cache.Invalidate(id) // couldn't confirm the snapshot; force a re-fetch
		return
	}
	cache.Store(id, snapshot, contents)
}

func toSet(uris []string) map[string]bool {
	set := make(map[string]bool, len(uris))
	for _, u := range uris {
		set[u] = true
	}
	return set
}
