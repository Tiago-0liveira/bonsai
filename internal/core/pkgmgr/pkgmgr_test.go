package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectCompatibilityAdapter(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`)

	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm == nil {
		t.Fatal("expected a PackageManager")
	}
	if pm.Name() != "npm" {
		t.Fatalf("Name = %q, want npm", pm.Name())
	}
	if got, want := pm.GetScripts(), []string{"build", "dev", "test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GetScripts = %v, want %v", got, want)
	}
	if got := pm.RunCommand("dev"); got != "npm run dev" {
		t.Fatalf("RunCommand(dev) = %q", got)
	}
}

func TestDetectCompatibilityPriority(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"test":"vitest"}}`)
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	write(t, dir, "Makefile", "test:\n\t@echo make\n")

	pm, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pm == nil || pm.Name() != "pnpm" {
		t.Fatalf("compatibility manager = %#v, want pnpm", pm)
	}
	if got := pm.RunCommand("test"); got != "pnpm run test" {
		t.Fatalf("RunCommand(test) = %q", got)
	}
}

func TestDetectMissing(t *testing.T) {
	pm, err := Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm != nil {
		t.Fatalf("expected nil PackageManager, got %v", pm)
	}
}

func commandByID(t *testing.T, project *Project, id string) Command {
	t.Helper()
	for _, cmd := range project.Commands {
		if cmd.ID == id {
			return cmd
		}
	}
	t.Fatalf("command %q not found; commands=%+v", id, project.Commands)
	return Command{}
}

func argByID(t *testing.T, cmd Command, id string) Argument {
	t.Helper()
	for _, arg := range cmd.Args {
		if arg.ID == id {
			return arg
		}
	}
	t.Fatalf("argument %q not found in command %q: %+v", id, cmd.ID, cmd.Args)
	return Argument{}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
