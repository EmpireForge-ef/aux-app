package aitools

import "testing"

// TestRegistry guards the tool registry against duplicate names, missing
// handlers, and accidental removals of whole API areas.
func TestRegistry(t *testing.T) {
	tools := All()

	// The wrapper exposes ~75 methods; a lower count means an area was lost.
	if len(tools) < 70 {
		t.Fatalf("expected at least 70 tools, got %d", len(tools))
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
}
