package weather

import "testing"

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
		2:  "clouds",
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
