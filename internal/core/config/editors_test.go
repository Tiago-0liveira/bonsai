package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentEditorPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editors.json")
	initial, err := ReadEditorSettings(path)
	if err != nil || initial.Revision != 0 || len(initial.Environments) != 0 {
		t.Fatalf("initial = %+v, %v", initial, err)
	}
	if _, err := UpdateEditorSettings(path, "windows:host", "vscode", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateEditorSettings(path, "wsl:host:Ubuntu", "zed", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateEditorSettings(path, "wsl:host:Debian", "cursor", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateEditorSettings(path, "wsl:host:Ubuntu", "vscode", 1); !errors.Is(err, ErrEditorRevision) {
		t.Fatalf("stale revision = %v", err)
	}
	saved, err := ReadEditorSettings(path)
	if err != nil || saved.Environments["windows:host"] != "vscode" || saved.Environments["wsl:host:Ubuntu"] != "zed" || saved.Environments["wsl:host:Debian"] != "cursor" {
		t.Fatalf("environment preferences = %+v, %v", saved, err)
	}
	cleared, err := UpdateEditorSettings(path, "wsl:host:Ubuntu", "", saved.Revision)
	if err != nil || cleared.Environments["wsl:host:Ubuntu"] != "" || cleared.Environments["windows:host"] != "vscode" {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}
}

func TestInvalidEditorSettingsAreNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editors.json")
	for _, raw := range []string{`null`, `{}`, `{"version":2,"environments":{}}`, `{broken`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadEditorSettings(path); err == nil {
			t.Fatalf("accepted invalid settings %s", raw)
		}
		if _, err := UpdateEditorSettings(path, "wsl:host:Ubuntu", "zed", 0); err == nil {
			t.Fatalf("overwrote invalid settings %s", raw)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != raw {
			t.Fatalf("invalid settings changed: %q, %v", got, err)
		}
	}
}
