package agy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncSettingsGCPCreatesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Initially nonexistent -> GCP creates file with gcp block.
	if err := SyncSettings(settingsPath, AuthMethodGCP, "my-gcp-project", "us"); err != nil {
		t.Fatalf("SyncSettings failed: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	gcp, ok := doc["gcp"].(map[string]any)
	if !ok {
		t.Fatalf("missing gcp block in %+v", doc)
	}
	if gcp["project"] != "my-gcp-project" {
		t.Errorf("project = %v, want my-gcp-project", gcp["project"])
	}
	if gcp["location"] != "us" {
		t.Errorf("location = %v, want us", gcp["location"])
	}

	// Add other settings like model and permissions.
	doc["model"] = "Gemini 4 Argon (Medium)"
	doc["agentMode"] = "accept-edits"
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	// Sync again with updated project and default region.
	if err := SyncSettings(settingsPath, AuthMethodGCP, "other-project", ""); err != nil {
		t.Fatalf("SyncSettings update failed: %v", err)
	}

	raw, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	doc = nil
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "Gemini 4 Argon (Medium)" {
		t.Errorf("model = %v, want preserved Gemini 4 Argon (Medium)", doc["model"])
	}
	if doc["agentMode"] != "accept-edits" {
		t.Errorf("agentMode = %v, want preserved accept-edits", doc["agentMode"])
	}
	gcp = doc["gcp"].(map[string]any)
	if gcp["project"] != "other-project" {
		t.Errorf("project = %v, want other-project", gcp["project"])
	}
	if gcp["location"] != RegionGlobal {
		t.Errorf("location = %v, want default %s", gcp["location"], RegionGlobal)
	}
}

func TestSyncSettingsConsumerRemovesGCP(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	initial := map[string]any{
		"model": "Gemini 3.8 Flash (High)",
		"gcp": map[string]any{
			"project":  "my-project",
			"location": "global",
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// Switching to consumer should remove "gcp" and preserve other keys.
	if err := SyncSettings(settingsPath, AuthMethodConsumer, "", ""); err != nil {
		t.Fatalf("SyncSettings failed: %v", err)
	}

	raw, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, hasGCP := doc["gcp"]; hasGCP {
		t.Errorf("expected gcp key to be deleted, got %+v", doc["gcp"])
	}
	if doc["model"] != "Gemini 3.8 Flash (High)" {
		t.Errorf("model = %v, want preserved Gemini 3.8 Flash (High)", doc["model"])
	}

	// Non-existent file with consumer does nothing.
	nonexistent := filepath.Join(dir, "empty", "settings.json")
	if err := SyncSettings(nonexistent, AuthMethodConsumer, "", ""); err != nil {
		t.Fatalf("SyncSettings on nonexistent consumer failed: %v", err)
	}
	if _, err := os.Stat(nonexistent); !os.IsNotExist(err) {
		t.Errorf("expected file to not be created for consumer")
	}
}
