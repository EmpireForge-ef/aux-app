package listening

import (
	"context"
	"log/slog"
	"strings"
	"time"

	spotify "github.com/EmpireForge-ef/spotify-go-wrapper"

	"github.com/EmpireForge-ef/aux-app/internal/weather"
)

// Poller periodically records the user's recent plays. It is driven entirely by
// closures so it doesn't depend on the server package.
type Poller struct {
	store    *Store
	weather  *weather.Client
	interval time.Duration
	client   func() (*spotify.Client, bool) // current Spotify client (false = not connected)
	location func() string                  // weather location ("" disables weather)
	timezone func() *time.Location          // for local-time bucketing (nil = UTC)
}

// NewPoller builds a poller. interval <= 0 defaults to 20 minutes.
func NewPoller(store *Store, w *weather.Client, interval time.Duration, client func() (*spotify.Client, bool), location func() string, timezone func() *time.Location) *Poller {
	if interval <= 0 {
		interval = 20 * time.Minute
	}
	return &Poller{store: store, weather: w, interval: interval, client: client, location: location, timezone: timezone}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	// A short initial delay lets startup (and a first Spotify connect) settle.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := p.pollOnce(ctx); err != nil {
				slog.Warn("listening poll failed", "err", err)
			}
			timer.Reset(p.interval)
		}
	}
}

// pollOnce records plays since the cursor. It is safe to call repeatedly; the
// (played_at, track_uri) unique index makes ingestion idempotent.
func (p *Poller) pollOnce(ctx context.Context) error {
	client, ok := p.client()
	if !ok || client == nil {
		return nil // Spotify not connected yet
	}
	after := p.store.LastPlayedAt()
	opts := []spotify.RequestOption{spotify.Limit(50)}
	if !after.IsZero() {
		opts = append(opts, spotify.PlayedAfter(after))
	}
	page, err := client.GetRecentlyPlayed(ctx, opts...)
	if err != nil {
		return err
	}
	if page == nil || len(page.Items) == 0 {
		return nil
	}

	// One weather reading is fetched per poll and stamped on the batch; events
	// are at most one interval old, so this is a close-enough approximation.
	var wx *weather.Weather
	if loc := p.location(); loc != "" {
		if w, werr := p.weather.Current(ctx, loc); werr != nil {
			slog.Warn("weather fetch failed", "err", werr)
		} else {
			wx = w
		}
	}

	zone := time.UTC
	if p.timezone != nil {
		if z := p.timezone(); z != nil {
			zone = z
		}
	}

	events := make([]PlayEvent, 0, len(page.Items))
	newest := after
	for _, ph := range page.Items {
		if ph.PlayedAt.After(newest) {
			newest = ph.PlayedAt
		}
		local := ph.PlayedAt.In(zone)
		ev := PlayEvent{
			PlayedAt:  ph.PlayedAt,
			TrackURI:  ph.Track.URI,
			TrackName: ph.Track.Name,
			Hour:      local.Hour(),
			DayOfWeek: int(local.Weekday()),
			IsWeekend: local.Weekday() == time.Saturday || local.Weekday() == time.Sunday,
			PartOfDay: PartOfDay(local.Hour()),
		}
		if len(ph.Track.Artists) > 0 {
			ev.ArtistURI = ph.Track.Artists[0].URI
			ev.ArtistName = ph.Track.Artists[0].Name
		}
		if wx != nil {
			ev.Weather = wx.Condition
			t := wx.TempC
			ev.TempC = &t
		}
		events = append(events, ev)
	}

	p.resolveGenres(ctx, client, events)
	inserted := p.store.Insert(events)
	if newest.After(after) {
		p.store.SetLastPlayedAt(newest)
	}
	if inserted > 0 {
		slog.Info("recorded plays", "count", inserted)
	}
	return nil
}

// resolveGenres fills each event's Genres from the artist-genre cache, fetching
// (and caching) any uncached artists in a single batched call.
func (p *Poller) resolveGenres(ctx context.Context, client *spotify.Client, events []PlayEvent) {
	ids := map[string]struct{}{} // artist IDs still needing a lookup
	for _, ev := range events {
		if ev.ArtistURI == "" {
			continue
		}
		if _, ok := p.store.CachedGenres(ev.ArtistURI); ok {
			continue
		}
		if id := artistID(ev.ArtistURI); id != "" {
			ids[id] = struct{}{}
		}
	}
	if len(ids) > 0 {
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		if artists, err := client.GetArtists(ctx, list...); err != nil {
			slog.Warn("genre lookup failed", "err", err)
		} else {
			for _, a := range artists {
				if a != nil {
					p.store.CacheGenres(a.URI, a.Genres)
				}
			}
		}
	}
	for i := range events {
		if events[i].ArtistURI == "" {
			continue
		}
		if g, ok := p.store.CachedGenres(events[i].ArtistURI); ok {
			events[i].Genres = g
		}
	}
}

// artistID extracts the bare ID from a "spotify:artist:ID" URI.
func artistID(uri string) string {
	const prefix = "spotify:artist:"
	if strings.HasPrefix(uri, prefix) {
		return uri[len(prefix):]
	}
	return ""
}
