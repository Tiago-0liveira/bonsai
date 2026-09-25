package pkgmgr

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectAndGetScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","build":"vite build","test":"vitest"}}`)
	pm, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if pm == nil {
		t.Fatal("expected a PackageManager")
	}
	if got, want := pm.GetScripts(), []string{"build", "dev", "test"}; !reflect.DeepEqual(got, want) {
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

func TestNodeManagerPrecedence(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"dev":"vite"}}`)
	write(t, dir, "package-lock.json", "{}")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 1 || project.Providers[0].ID != "node:pnpm" {
		t.Fatalf("providers = %+v, want node:pnpm", project.Providers)
	}
	cmd := commandByID(t, project, "node:script:dev")
	if cmd.Invocation.Program != "pnpm" || !reflect.DeepEqual(cmd.Invocation.Prefix, []string{"run", "dev"}) {
		t.Fatalf("invocation = %+v", cmd.Invocation)
	}
	if cmd.Raw != "vite" {
		t.Fatalf("raw = %q, want vite", cmd.Raw)
	}
}

func TestDiscoverMultipleProviders(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"build":"vite build","dev":"vite"}}`)
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	write(t, dir, "Makefile", "bootstrap:\n\t@echo ok\ndeploy:\n\t@echo ok\n")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	gotProviders := make([]string, 0, len(project.Providers))
	for _, provider := range project.Providers {
		gotProviders = append(gotProviders, provider.ID)
	}
	wantProviders := []string{"node:npm", "cargo", "make"}
	if !reflect.DeepEqual(gotProviders, wantProviders) {
		t.Fatalf("providers = %v, want %v", gotProviders, wantProviders)
	}
	for _, id := range []string{"node:script:build", "cargo:builtin:build", "make:target:deploy"} {
		commandByID(t, project, id)
	}
}

func TestNodeWorkspaceLocationAndScopedID(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "package.json", `{"private":true,"packageManager":"pnpm@10.17.0","workspaces":["apps/*"]}`)
	write(t, repo, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	app := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(filepath.Join(app, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, app, "package.json", `{"scripts":{"dev":"vite"}}`)

	project, err := Discover(filepath.Join(app, "src"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Location.ProjectRoot != app {
		t.Fatalf("ProjectRoot = %q, want %q", project.Location.ProjectRoot, app)
	}
	if project.Location.WorkspaceRoot != repo {
		t.Fatalf("WorkspaceRoot = %q, want %q", project.Location.WorkspaceRoot, repo)
	}
	if project.Providers[0].ID != "node:pnpm" {
		t.Fatalf("provider = %q, want node:pnpm", project.Providers[0].ID)
	}
	commandByID(t, project, "node:apps/web:script:dev")
}

func TestCargoCatalogAndResolve(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build", "check", "clean", "doc", "fetch", "fix", "metadata", "package", "publish", "run", "test", "tree", "update", "vendor", "version"} {
		commandByID(t, project, "cargo:builtin:"+name)
	}
	cmd := commandByID(t, project, "cargo:builtin:run")
	inv, err := Resolve(cmd, ArgumentValues{
		"release": {"true"},
		"jobs":    {"4"},
		"args":    {"--port", "8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--release", "--jobs", "4", "--", "--port", "8080"}
	if inv.Program != "cargo" || inv.Dir != dir || !reflect.DeepEqual(inv.Args, want) {
		t.Fatalf("Resolve = %+v, want cargo %v in %s", inv, want, dir)
	}
}

func TestMakeStaticParserAndProvenance(t *testing.T) {
	dir := t.TempDir()
	mk := ".PHONY: build test\n" +
		"build:\n\tgo build .\n" +
		"test deploy-prod: build\n\tgo test ./...\n" +
		"%.o: %.c\n\tcc -c $<\n" +
		"VAR := x\n"
	write(t, dir, "Makefile", mk)
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := commandByID(t, project, "make:target:build"); got.Source.Line != 2 {
		t.Fatalf("build line = %d, want 2", got.Source.Line)
	}
	if got := commandByID(t, project, "make:target:test"); got.Source.Line != 4 {
		t.Fatalf("test line = %d, want 4", got.Source.Line)
	}
	commandByID(t, project, "make:target:deploy-prod")
	for _, cmd := range project.Commands {
		if strings.Contains(cmd.ID, "%.o") || cmd.Name == ".PHONY" {
			t.Fatalf("unexpected make command: %+v", cmd)
		}
	}
}

func TestFingerprintContentBasedAndDirtyAware(t *testing.T) {
	makeProject := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
		return dir
	}
	a, b := makeProject(t), makeProject(t)
	pa, err := Discover(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Discover(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if pa.Fingerprint != pb.Fingerprint {
		t.Fatalf("same content at different paths produced fingerprints %s and %s", pa.Fingerprint, pb.Fingerprint)
	}
	write(t, a, "package.json", `{"scripts":{"dev":"vite","test":"vitest"}}`)
	changed, err := Discover(a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fingerprint == pa.Fingerprint {
		t.Fatal("dirty manifest change did not invalidate fingerprint")
	}
}

func TestCacheRebasesWorkingDirectory(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	mk := func() string {
		d := t.TempDir()
		write(t, d, "package.json", `{"scripts":{"dev":"vite"}}`)
		return d
	}
	firstDir := mk()
	first, err := Discover(firstDir, Options{UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	secondDir := mk()
	second, err := Discover(secondDir, Options{UseCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("expected cache-compatible fingerprints")
	}
	cmd := commandByID(t, second, "node:script:dev")
	if cmd.Invocation.WorkingDir != secondDir {
		t.Fatalf("cached WorkingDir = %q, want %q", cmd.Invocation.WorkingDir, secondDir)
	}
}

func TestOverridesEnrichHideAndDefine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","test":"vitest"}}`)
	write(t, dir, ".bonsai.yaml", `pkgmgr:
  commands:
    - id: node:script:dev
      description: Start the local web app
      args:
        - id: port
          flags: ["--port"]
          type: int
    - id: node:script:test
      hide: true
    - id: project:seed
      name: seed
      command:
        program: pnpm
        args: ["db:seed"]
`)
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	dev := commandByID(t, project, "node:script:dev")
	if dev.Description != "Start the local web app" {
		t.Fatalf("description = %q", dev.Description)
	}
	var port *Argument
	for i := range dev.Args {
		if dev.Args[i].ID == "port" {
			port = &dev.Args[i]
		}
	}
	if port == nil || port.Type != ValueInt || !reflect.DeepEqual(port.Flags, []string{"--port"}) {
		t.Fatalf("port arg = %+v", port)
	}
	for _, cmd := range project.Commands {
		if cmd.ID == "node:script:test" {
			t.Fatal("hidden test command was retained")
		}
	}
	seed := commandByID(t, project, "project:seed")
	if seed.Provider != "override" || seed.Invocation.Program != "pnpm" || !reflect.DeepEqual(seed.Invocation.Prefix, []string{"db:seed"}) {
		t.Fatalf("seed = %+v", seed)
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
