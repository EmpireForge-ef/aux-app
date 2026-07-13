package server

import "testing"

func TestRedirectURLWarning(t *testing.T) {
	tests := []struct {
		url      string
		wantWarn bool
	}{
		{"http://127.0.0.1:8080/api/auth/callback", false},
		{"http://[::1]:8080/api/auth/callback", false},
		{"https://aux.example.com/api/auth/callback", false},
		{"http://localhost:8080/api/auth/callback", true},  // localhost is rejected by Spotify
		{"https://localhost:8080/api/auth/callback", true}, // even over HTTPS
		{"http://aux.example.com/api/auth/callback", true}, // plain HTTP off-loopback
	}
	for _, tt := range tests {
		if warn := redirectURLWarning(tt.url); (warn != "") != tt.wantWarn {
			t.Errorf("redirectURLWarning(%q) = %q, want warning: %v", tt.url, warn, tt.wantWarn)
		}
	}
}
