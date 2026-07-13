package aitools

import "testing"

// TestRegistry guards the tool registry against duplicate names, missing
// handlers, and accidental removals of whole API areas.
func TestRegistry(t *testing.T) {
	tools := All()

	// The working (non-deprecated) surface is ~74 tools; a much lower count
	// means an area was lost.
	if len(tools) < 60 {
		t.Fatalf("expected at least 60 tools, got %d", len(tools))
	}

	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool with empty name")
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		if tool.Handler == nil {
			t.Errorf("tool %q has no handler", tool.Name)
		}
	}

	if params := Params(tools); len(params) != len(tools) {
		t.Errorf("Params returned %d entries for %d tools", len(params), len(tools))
	}

	// Tools that Spotify deprecated or removed for development-mode apps must
	// no longer be registered.
	for _, name := range []string{
		"get_audio_features", "get_multiple_audio_features", "get_audio_analysis",
		"get_recommendations", "get_available_genre_seeds", "get_available_markets",
		"get_categories", "get_category", "get_new_releases", "get_related_artists",
		"get_artist_top_tracks", "get_user", "get_user_playlists",
		"current_user_follows_playlist", "get_featured_playlists", "get_category_playlists",
	} {
		if seen[name] {
			t.Errorf("deprecated tool %q should have been removed", name)
		}
	}

	// Destructive tools must be confirmation-gated.
	confirmable := make(map[string]bool, len(tools))
	for _, tool := range tools {
		confirmable[tool.Name] = tool.Confirm != ""
	}
	for _, name := range []string{
		"remove_saved_tracks", "remove_saved_albums", "remove_saved_episodes",
		"remove_saved_shows", "remove_saved_audiobooks", "remove_playlist_items",
		"replace_playlist_items", "unfollow", "unfollow_playlist",
	} {
		if _, ok := confirmable[name]; !ok {
			t.Errorf("expected destructive tool %q to exist", name)
		} else if !confirmable[name] {
			t.Errorf("destructive tool %q must have a Confirm prompt", name)
		}
	}
}
