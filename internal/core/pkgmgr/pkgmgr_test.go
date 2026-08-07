package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectAndGetScripts(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm == nil {
		t.Fatal("expected a PackageManager")
	}

	got := pm.GetScripts()
	want := []string{"build", "dev", "test"} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetScripts = %v, want %v", got, want)
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

func TestDetectNodeManagers(t *testing.T) {
	cases := []struct {
		lockfile string
		name     string
		runDev   string
	}{
		{"package-lock.json", "npm", "npm run dev"},
		{"pnpm-lock.yaml", "pnpm", "pnpm run dev"},
		{"yarn.lock", "yarn", "yarn dev"},
		{"bun.lockb", "bun", "bun run dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
			if c.lockfile != "" {
				write(t, dir, c.lockfile, "")
			}
			pm, err := Detect(dir)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if pm.Name() != c.name {
				t.Fatalf("Name = %q, want %q", pm.Name(), c.name)
			}
			if got := pm.RunCommand("dev"); got != c.runDev {
				t.Fatalf("RunCommand = %q, want %q", got, c.runDev)
			}
		})
	}
}

func TestDetectCargo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\n")
	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm.Name() != "cargo" {
		t.Fatalf("Name = %q, want cargo", pm.Name())
	}
	if got := pm.RunCommand("build"); got != "cargo build" {
		t.Fatalf("RunCommand = %q, want cargo build", got)
	}
}

func TestDetectMakeTargets(t *testing.T) {
	dir := t.TempDir()
	mk := "" +
		".PHONY: build test\n" +
		"build:\n\tgo build .\n" +
		"test: build\n\tgo test ./...\n" +
		"%.o: %.c\n\tcc -c $<\n" +
		"VAR := x\n"
	write(t, dir, "Makefile", mk)
	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm.Name() != "make" {
		t.Fatalf("Name = %q, want make", pm.Name())
	}
	got := pm.GetScripts()
	want := []string{"build", "test"} // .PHONY, pattern rule, and VAR assignment excluded
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetScripts = %v, want %v", got, want)
	}
	if cmd := pm.RunCommand("test"); cmd != "make test" {
		t.Fatalf("RunCommand = %q, want make test", cmd)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
