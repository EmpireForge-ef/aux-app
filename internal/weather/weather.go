// Package weather fetches the current weather from Open-Meteo (free, no API
// key) for a configured location, so play events can be tagged with the
// conditions they happened in.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Weather is the current condition at a location.
type Weather struct {
	Condition string  `json:"condition"` // short word: clear, clouds, fog, rain, snow, thunderstorm
	TempC     float64 `json:"temp_c"`
}

// forecastTTL is how long a current-weather reading is reused before re-fetching.
// Weather barely moves, so this keeps per-turn injection close to free.
const forecastTTL = 20 * time.Minute

type cachedForecast struct {
	w  Weather
	at time.Time
}

// Client fetches current weather and caches geocoding lookups and recent
// forecasts. It is safe for concurrent use.
type Client struct {
	http *http.Client

	mu   sync.Mutex
	geo  map[string][2]float64     // location string -> {lat, lon}
	fore map[string]cachedForecast // location string -> recent reading
}

// New returns a weather client.
func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 8 * time.Second},
		geo:  map[string][2]float64{},
		fore: map[string]cachedForecast{},
	}
}

// Current returns the current weather at location, which may be "lat,lon" (e.g.
// "52.52,13.40") or a place name (e.g. "Berlin") that is geocoded once and
// cached. Readings are cached for forecastTTL. An empty location yields
// (nil, nil): weather is simply unavailable.
func (c *Client) Current(ctx context.Context, location string) (*Weather, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil, nil
	}
	c.mu.Lock()
	if f, ok := c.fore[location]; ok && time.Since(f.at) < forecastTTL {
		w := f.w
		c.mu.Unlock()
		return &w, nil
	}
	c.mu.Unlock()

	lat, lon, err := c.resolve(ctx, location)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("current", "temperature_2m,weather_code")
	var out struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Code int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := c.getJSON(ctx, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	w := Weather{Condition: conditionFromCode(out.Current.Code), TempC: out.Current.Temp}
	c.mu.Lock()
	c.fore[location] = cachedForecast{w: w, at: time.Now()}
	c.mu.Unlock()
	return &w, nil
}

// resolve turns a location string into coordinates: a "lat,lon" pair is parsed
// directly; anything else is geocoded (and cached).
func (c *Client) resolve(ctx context.Context, location string) (lat, lon float64, err error) {
	if la, lo, ok := parseLatLon(location); ok {
		return la, lo, nil
	}
	c.mu.Lock()
	if v, ok := c.geo[location]; ok {
		c.mu.Unlock()
		return v[0], v[1], nil
	}
	c.mu.Unlock()

	q := url.Values{}
	q.Set("name", location)
	q.Set("count", "1")
	var out struct {
		Results []struct {
			Lat float64 `json:"latitude"`
			Lon float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, "https://geocoding-api.open-meteo.com/v1/search?"+q.Encode(), &out); err != nil {
		return 0, 0, err
	}
	if len(out.Results) == 0 {
		return 0, 0, fmt.Errorf("could not geocode location %q", location)
	}
	la, lo := out.Results[0].Lat, out.Results[0].Lon
	c.mu.Lock()
	c.geo[location] = [2]float64{la, lo}
	c.mu.Unlock()
	return la, lo, nil
}

func (c *Client) getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather API returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// parseLatLon parses "lat,lon" into coordinates; ok is false if the string is
// not a coordinate pair.
func parseLatLon(s string) (lat, lon float64, ok bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	la, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lo, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if la < -90 || la > 90 || lo < -180 || lo > 180 {
		return 0, 0, false
	}
	return la, lo, true
}

// conditionFromCode maps a WMO weather code to a short condition word.
func conditionFromCode(code int) string {
	switch {
	case code <= 1: // 0 clear sky, 1 mainly clear
		return "clear"
	case code <= 3: // 2 partly cloudy, 3 overcast
		return "clouds"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 57:
		return "drizzle"
	case code >= 61 && code <= 67:
		return "rain"
	case code >= 71 && code <= 77:
		return "snow"
	case code >= 80 && code <= 82:
		return "rain"
	case code >= 85 && code <= 86:
		return "snow"
	case code >= 95:
		return "thunderstorm"
	default:
		return "unknown"
	}
}
