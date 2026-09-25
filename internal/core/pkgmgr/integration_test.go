package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestMixedProviderRepositoryPreservesNameCollisions(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"dev":"vite","test":"vitest"}}`)
	write(t, dir, "pnpm-lock.yaml", "")
	write(t, dir, "Cargo.toml", "[package]\nname=\"mixed\"\nversion=\"0.1.0\"\n")
	write(t, dir, "Makefile", "deploy:\n\t@echo deploy\ntest:\n\t@echo test\n")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var providers []string
	for _, provider := range project.Providers {
		providers = append(providers, provider.ID)
	}
	if want := []string{"node:pnpm", "cargo", "make"}; !reflect.DeepEqual(providers, want) {
		t.Fatalf("providers = %v, want %v", providers, want)
	}
	for _, id := range []string{
		"node:script:dev",
		"node:script:test",
		"cargo:builtin:build",
		"cargo:builtin:test",
		"make:target:deploy",
		"make:target:test",
	} {
		commandByID(t, project, id)
	}

	var tests []string
	for _, cmd := range project.Commands {
		if cmd.Name == "test" {
			tests = append(tests, cmd.ID)
		}
	}
	if want := []string{"cargo:builtin:test", "make:target:test", "node:script:test"}; !reflect.DeepEqual(tests, want) {
		t.Fatalf("test collisions = %v, want %v", tests, want)
	}
}

func TestPassiveDiscoveryNeverExecutesNodeScripts(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "SHOULD_NOT_EXIST")
	write(t, dir, "package.json", `{"scripts":{"evil":"touch SHOULD_NOT_EXIST"}}`)

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	commandByID(t, project, "node:script:evil")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("node script executed during discovery: %v", err)
	}
}

func TestPassiveDiscoveryDoesNotProbeArbitraryProjectBinary(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "SHOULD_NOT_EXIST")
	toolName := "project-tool"
	toolPath := filepath.Join(dir, toolName)
	script := "#!/bin/sh\necho probed > " + marker + "\n"
	if runtime.GOOS == "windows" {
		toolName = "project-tool.bat"
		toolPath = filepath.Join(dir, toolName)
		script = "@echo probed>" + marker + "\r\n"
	}
	if err := os.WriteFile(toolPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	write(t, dir, "package.json", `{"scripts":{"tool":"project-tool --help"}}`)

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "node:script:tool")
	if cmd.Raw != "project-tool --help" {
		t.Fatalf("raw script = %q", cmd.Raw)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("arbitrary project binary was probed: %v", err)
	}
}

func TestShellMetacharactersRemainData(t *testing.T) {
	dir := t.TempDir()
	name := "evil;touch SHOULD_NOT_EXIST"
	write(t, dir, "package.json", `{"scripts":{"evil;touch SHOULD_NOT_EXIST":"echo ok"}}`)
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "node:script:"+name)
	inv, err := Resolve(cmd, ArgumentValues{"args": {"&&", "echo", "still-data"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", name, "--", "&&", "echo", "still-data"}
	if inv.Program != "npm" || !reflect.DeepEqual(inv.Args, want) {
		t.Fatalf("invocation = %#v, want argv %#v", inv, want)
	}
	if strings.Contains(inv.Program, ";") {
		t.Fatalf("metacharacter escaped into program: %q", inv.Program)
	}
}
