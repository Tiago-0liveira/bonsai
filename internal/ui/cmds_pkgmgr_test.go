package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadScriptsCompatibility(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite","build":"vite build"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	msg, ok := loadScripts(dir)().(scriptsMsg)
	if !ok {
		t.Fatalf("loadScripts returned unexpected message type")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.manager != "npm" {
		t.Fatalf("manager = %q, want npm", msg.manager)
	}
	if want := []string{"build", "dev"}; !reflect.DeepEqual(msg.scripts, want) {
		t.Fatalf("scripts = %v, want %v", msg.scripts, want)
	}
	if msg.runCmd["dev"] != "npm run dev" || msg.runCmd["build"] != "npm run build" {
		t.Fatalf("run commands = %+v", msg.runCmd)
	}
}

func TestLoadScriptsNoProviderKeepsAdHocFlow(t *testing.T) {
	msg, ok := loadScripts(t.TempDir())().(scriptsMsg)
	if !ok {
		t.Fatal("loadScripts returned unexpected message type")
	}
	if msg.err != nil || msg.manager != "" || len(msg.scripts) != 0 || len(msg.runCmd) != 0 {
		t.Fatalf("no-provider message = %+v", msg)
	}
}

func TestLoadScriptsDiscoveryError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, ok := loadScripts(dir)().(scriptsMsg)
	if !ok {
		t.Fatal("loadScripts returned unexpected message type")
	}
	if msg.err == nil {
		t.Fatal("expected discovery error")
	}
}

func TestLoadScriptsMixedProvidersKeepsLegacyPriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"pnpm@10.17.0","scripts":{"test":"vitest"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname=\"x\"\nversion=\"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := loadScripts(dir)().(scriptsMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.manager != "pnpm" || !reflect.DeepEqual(msg.scripts, []string{"test"}) || msg.runCmd["test"] != "pnpm run test" {
		t.Fatalf("legacy mixed-provider scripts message = %+v", msg)
	}
}

func TestLoadScriptsEmptyCommandList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"empty"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := loadScripts(dir)().(scriptsMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.manager != "npm" || len(msg.scripts) != 0 || len(msg.runCmd) != 0 {
		t.Fatalf("empty scripts message = %+v", msg)
	}
}
