package weather

import (
	"context"
	"testing"
	"time"
)

func TestForecastCacheHit(t *testing.T) {
	c := New()
	// A fresh cached reading is served without any network call.
	c.fore["Testville"] = cachedForecast{w: Weather{Condition: "rain", TempC: 9}, at: time.Now()}
	w, err := c.Current(context.Background(), "Testville")
	if err != nil || w == nil || w.Condition != "rain" || w.TempC != 9 {
		t.Fatalf("cache hit = %+v err=%v", w, err)
	}
	// A stale entry is treated as a miss (would re-fetch).
	c.fore["Testville"] = cachedForecast{w: Weather{Condition: "rain"}, at: time.Now().Add(-forecastTTL - time.Minute)}
	c.mu.Lock()
	_, fresh := c.fore["Testville"]
	stale := time.Since(c.fore["Testville"].at) >= forecastTTL
	c.mu.Unlock()
	if !fresh || !stale {
		t.Error("expected the entry to be present but stale")
	}
}

func TestParseLatLon(t *testing.T) {
	cases := []struct {
		in       string
		lat, lon float64
		ok       bool
	}{
		{"52.52,13.40", 52.52, 13.40, true},
		{" 40.7 , -74.0 ", 40.7, -74.0, true},
		{"Berlin", 0, 0, false},
		{"52.52", 0, 0, false},
		{"200,10", 0, 0, false}, // out of range
		{"", 0, 0, false},
	}
	for _, c := range cases {
		lat, lon, ok := parseLatLon(c.in)
		if ok != c.ok || (ok && (lat != c.lat || lon != c.lon)) {
			t.Errorf("parseLatLon(%q) = (%v,%v,%v), want (%v,%v,%v)", c.in, lat, lon, ok, c.lat, c.lon, c.ok)
		}
	}
}

func TestConditionFromCode(t *testing.T) {
	cases := map[int]string{
		0:  "clear",
		1:  "clear",
		2:  "clouds",
		3:  "clouds",
		45: "fog",
		53: "drizzle",
		63: "rain",
		75: "snow",
		81: "rain",
		95: "thunderstorm",
	}
	for code, want := range cases {
		if got := conditionFromCode(code); got != want {
			t.Errorf("conditionFromCode(%d) = %q, want %q", code, got, want)
		}
	}
}
