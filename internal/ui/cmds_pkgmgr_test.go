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
	if msg.runDir["dev"] != dir {
		t.Fatalf("dev run dir = %q, want %q", msg.runDir["dev"], dir)
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

func TestLoadScriptsMixedProvidersShowsAllProviders(t *testing.T) {
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
	if msg.manager != "project" {
		t.Fatalf("manager = %q, want project", msg.manager)
	}
	for label, want := range map[string]string{
		"[pnpm] test": "pnpm run test",
		"[cargo] test": "cargo test",
		"[make] test":  "make test",
	} {
		if msg.runCmd[label] != want {
			t.Fatalf("%s command = %q, want %q; all=%+v", label, msg.runCmd[label], want, msg.runCmd)
		}
	}
}

func TestLoadScriptsDepthControlsNestedDiscovery(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	shallow := loadScripts(root, 1)().(scriptsMsg)
	if shallow.err != nil {
		t.Fatal(shallow.err)
	}
	if len(shallow.scripts) != 0 {
		t.Fatalf("depth=1 scripts = %v, want none", shallow.scripts)
	}

	deep := loadScripts(root, 2)().(scriptsMsg)
	if deep.err != nil {
		t.Fatal(deep.err)
	}
	if !reflect.DeepEqual(deep.scripts, []string{"dev"}) {
		t.Fatalf("depth=2 scripts = %v, want [dev]", deep.scripts)
	}
	if deep.runCmd["dev"] != "npm run dev" || deep.runDir["dev"] != web {
		t.Fatalf("nested dev = command %q dir %q, want npm run dev in %q", deep.runCmd["dev"], deep.runDir["dev"], web)
	}
}

func TestLoadScriptsGoOnlyProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := loadScripts(dir)().(scriptsMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.manager != "go" {
		t.Fatalf("manager = %q, want go", msg.manager)
	}
	if msg.runCmd["test"] != "go test ./..." || msg.runCmd["build"] != "go build ./..." {
		t.Fatalf("Go commands = %+v", msg.runCmd)
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
	if msg.manager != "" || len(msg.scripts) != 0 || len(msg.runCmd) != 0 {
		t.Fatalf("empty scripts message = %+v", msg)
	}
}


func TestLoadScriptsGoRootWithNestedWeb(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"scripts":{"dev":"vite","test":"vitest"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := loadScripts(root, 2)().(scriptsMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.manager != "project" {
		t.Fatalf("manager = %q, want project", msg.manager)
	}
	if msg.runCmd["[go] build"] != "go build ./..." {
		t.Fatalf("missing Go build command: %+v", msg.runCmd)
	}
	if msg.runCmd["[npm] dev"] != "npm run dev" {
		t.Fatalf("missing nested npm dev command: %+v", msg.runCmd)
	}
	if msg.runDir["[go] build"] != root {
		t.Fatalf("Go dir = %q, want %q", msg.runDir["[go] build"], root)
	}
	if msg.runDir["[npm] dev"] != web {
		t.Fatalf("npm dir = %q, want %q", msg.runDir["[npm] dev"], web)
	}
}
