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
