package pkgmgr

import (
	"path/filepath"
	"testing"
)

func TestBoundedManifestSearchDepth(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "apps", "web"), "package.json", `{"scripts":{"dev":"vite"}}`)

	depth1 := 1
	project, err := Discover(root, Options{SearchDepth: &depth1})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range project.Providers {
		if provider.ID == "node:npm" {
			t.Fatalf("depth=1 unexpectedly discovered depth-2 package: %+v", provider)
		}
	}

	depth2 := 2
	project, err = Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "node:script:dev")
	wantRoot := filepath.Join(root, "apps", "web")
	if cmd.Invocation.WorkingDir != wantRoot {
		t.Fatalf("WorkingDir = %q, want %q", cmd.Invocation.WorkingDir, wantRoot)
	}
	if project.Location.ProjectRoot != wantRoot {
		t.Fatalf("ProjectRoot = %q, want %q", project.Location.ProjectRoot, wantRoot)
	}
}

func TestBoundedManifestSearchDoesNotExceedDepth(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "one", "two", "three"), "package.json", `{"scripts":{"dev":"vite"}}`)
	depth2 := 2
	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range project.Providers {
		if provider.ID == "node:npm" {
			t.Fatal("depth=2 discovered package.json at depth 3")
		}
	}
}

func TestBoundedManifestSearchSkipsDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "node_modules", "dep"), "package.json", `{"scripts":{"evil":"echo nope"}}`)
	write(t, filepath.Join(root, "target", "nested"), "Cargo.toml", "[package]\nname=\"dep\"\nversion=\"0.1.0\"\n")
	depth2 := 2
	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 0 {
		t.Fatalf("dependency/build directories produced providers: %+v", project.Providers)
	}
}

func TestBoundedSearchPreservesUpwardPreference(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"scripts":{"root":"echo root"}}`)
	nested := filepath.Join(root, "apps", "web")
	write(t, nested, "package.json", `{"scripts":{"nested":"echo nested"}}`)
	write(t, filepath.Join(nested, "src"), ".keep", "")

	project, err := Discover(filepath.Join(nested, "src"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	commandByID(t, project, "node:script:nested")
	for _, cmd := range project.Commands {
		if cmd.ID == "node:script:root" {
			t.Fatal("upward lookup skipped nearer nested package")
		}
	}
}


func TestDiscoverRootAndNestedNodeProjects(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"scripts":{"root":"echo root"}}`)
	web := filepath.Join(root, "apps", "web")
	write(t, web, "package.json", `{"scripts":{"dev":"vite"}}`)
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	var nodeProviders int
	for _, provider := range project.Providers {
		if provider.ID == "node:npm" {
			nodeProviders++
		}
	}
	if nodeProviders != 2 {
		t.Fatalf("node providers = %d, want 2: %+v", nodeProviders, project.Providers)
	}

	rootCmd := commandByID(t, project, "node:script:root")
	if rootCmd.Invocation.WorkingDir != root {
		t.Fatalf("root command dir = %q, want %q", rootCmd.Invocation.WorkingDir, root)
	}
	webCmd := commandByID(t, project, "node:script:dev")
	if webCmd.Invocation.WorkingDir != web {
		t.Fatalf("web command dir = %q, want %q", webCmd.Invocation.WorkingDir, web)
	}
}

func TestDiscoverSiblingNodeProjects(t *testing.T) {
	root := t.TempDir()
	admin := filepath.Join(root, "apps", "admin")
	web := filepath.Join(root, "apps", "web")
	write(t, admin, "package.json", `{"scripts":{"dev":"vite --mode admin"}}`)
	write(t, web, "package.json", `{"scripts":{"dev":"vite"}}`)
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	adminCmd := commandByID(t, project, "node:script:dev@apps/admin")
	webCmd := commandByID(t, project, "node:script:dev@apps/web")
	if adminCmd.Invocation.WorkingDir != admin || webCmd.Invocation.WorkingDir != web {
		t.Fatalf("sibling dirs = admin %q web %q", adminCmd.Invocation.WorkingDir, webCmd.Invocation.WorkingDir)
	}
}


func TestDiscoverDifferentNodeManagersDoNotCollide(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"packageManager":"npm@11.0.0","scripts":{"dev":"echo root"}}`)
	web := filepath.Join(root, "apps", "web")
	write(t, web, "package.json", `{"packageManager":"pnpm@10.17.0","scripts":{"dev":"vite"}}`)
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	rootCmd := commandByID(t, project, "node:script:dev@.")
	webCmd := commandByID(t, project, "node:script:dev@apps/web")
	if rootCmd.Provider != "node:npm" || webCmd.Provider != "node:pnpm" {
		t.Fatalf("manager-scoped commands = root %+v web %+v", rootCmd, webCmd)
	}
}
