package cliproxy

import (
	"slices"
	"testing"
	"time"
)

func TestPersistedAntigravityHintsCarryUpdatedAtAndStillParse(t *testing.T) {
	fixed := time.Date(2026, 10, 13, 19, 45, 0, 0, time.UTC)
	previousNow := antigravityNowFunc
	antigravityNowFunc = func() time.Time { return fixed }
	defer func() { antigravityNowFunc = previousNow }()

	hints := antigravityModelCapabilityHints{
		ModelIDs:          map[string]struct{}{"gemini-4-argon-high": {}, "gemini-3-flash": {}},
		WebSearchModelIDs: map[string]struct{}{},
	}
	value := buildPersistedAntigravityCapabilityHintsValue("project-x", "https://daily.example", hints)
	if value["updated_at"] != "2026-10-13T19:45:00Z" {
		t.Fatalf("updated_at = %v", value["updated_at"])
	}
	parsed, ok := parsePersistedAntigravityCapabilityHints(map[string]any{antigravityPersistedHintsMetaKey: value}, "project-x", "https://daily.example")
	if !ok || len(parsed.ModelIDs) != 2 {
		t.Fatalf("persisted value no longer parses: ok=%v ids=%v", ok, parsed.ModelIDs)
	}
}

func TestDiffModelIDs(t *testing.T) {
	previous := []*ModelInfo{{ID: "gemini-3-flash"}, {ID: "gemini-3.1-pro-low"}, nil}
	next := []*ModelInfo{{ID: "gemini-3-flash"}, {ID: "gemini-4-argon-high"}}
	added, removed := diffModelIDs(previous, next)
	if !slices.Equal(added, []string{"gemini-4-argon-high"}) || !slices.Equal(removed, []string{"gemini-3.1-pro-low"}) {
		t.Fatalf("added=%v removed=%v", added, removed)
	}
	if added, removed := diffModelIDs(next, next); len(added)+len(removed) != 0 {
		t.Fatal("identical lists reported a change")
	}
}
