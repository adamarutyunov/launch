package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type UserSettings struct {
	CollapsedGroups map[string]bool `json:"collapsed_groups,omitempty"`
	HiddenTasks     map[string]bool `json:"hidden_tasks,omitempty"`
}

func SettingsFilePath(rootDir string) string {
	return filepath.Join(baseDir(), "settings", sessionID(rootDir)+".json")
}

func LoadSettings(rootDir string) *UserSettings {
	path := SettingsFilePath(rootDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return &UserSettings{CollapsedGroups: make(map[string]bool)}
	}
	var settings UserSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return &UserSettings{CollapsedGroups: make(map[string]bool)}
	}
	if settings.CollapsedGroups == nil {
		settings.CollapsedGroups = make(map[string]bool)
	}
	if settings.HiddenTasks == nil {
		settings.HiddenTasks = make(map[string]bool)
	}
	return &settings
}

func SaveSettings(rootDir string, settings *UserSettings) error {
	path := SettingsFilePath(rootDir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating settings dir: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
