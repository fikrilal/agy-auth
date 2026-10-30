package agy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func defaultSettingsFilePath(tokenFile string) string {
	if override := os.Getenv("AGY_AUTH_AGY_SETTINGS_FILE"); override != "" {
		return override
	}
	if tokenFile == "" {
		tokenFile = defaultTokenFilePath()
	}
	return filepath.Join(filepath.Dir(tokenFile), "settings.json")
}

// SyncSettings updates or cleans up the agy settings.json file to match the active account.
// When switching to a GCP account, it ensures the "gcp" block contains the project and location.
// When switching to a consumer account, it removes the "gcp" block so agy does not attempt
// to use GCP Enterprise/Platform authentication.
func SyncSettings(settingsPath string, authMethod, projectID, region string) error {
	if settingsPath == "" {
		settingsPath = defaultSettingsFilePath("")
	}

	authMethod = strings.ToLower(strings.TrimSpace(authMethod))
	projectID = strings.TrimSpace(projectID)
	region = strings.ToLower(strings.TrimSpace(region))
	isGCP := authMethod == AuthMethodGCP || projectID != ""

	data, err := os.ReadFile(settingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read settings: %w", err)
	}

	if errors.Is(err, os.ErrNotExist) {
		if !isGCP {
			return nil
		}
		data = []byte("{}")
	}

	var doc map[string]any
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		doc = make(map[string]any)
	} else if err := json.Unmarshal(trimmed, &doc); err != nil {
		return fmt.Errorf("parse settings %s: %w", settingsPath, err)
	}

	if doc == nil {
		doc = make(map[string]any)
	}

	if isGCP {
		if region == "" {
			region = RegionGlobal
		}
		var gcpMap map[string]any
		if existing, ok := doc["gcp"].(map[string]any); ok && existing != nil {
			gcpMap = existing
		} else {
			gcpMap = make(map[string]any)
		}
		gcpMap["project"] = projectID
		gcpMap["location"] = region
		doc["gcp"] = gcpMap
	} else {
		if _, ok := doc["gcp"]; !ok {
			return nil
		}
		delete(doc, "gcp")
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	out = append(out, '\n')
	if err := writeFileAtomic(settingsPath, out, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}
