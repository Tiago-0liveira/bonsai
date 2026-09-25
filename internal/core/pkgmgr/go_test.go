package pkgmgr

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestGoProviderCatalog(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/app\n\ngo 1.26\n")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 1 || project.Providers[0].ID != "go" {
		t.Fatalf("providers = %+v, want go", project.Providers)
	}
	for _, id := range []string{"build", "clean", "fmt", "generate", "mod-download", "mod-tidy", "run", "test", "vet"} {
		commandByID(t, project, "go:builtin:"+id)
	}

	testCmd := commandByID(t, project, "go:builtin:test")
	if testCmd.Invocation.Program != "go" || !reflect.DeepEqual(testCmd.Invocation.Prefix, []string{"test", "./..."}) {
		t.Fatalf("go test invocation = %+v", testCmd.Invocation)
	}
	run := commandByID(t, project, "go:builtin:run")
	if !reflect.DeepEqual(run.Invocation.Prefix, []string{"run", "."}) {
		t.Fatalf("go run invocation = %+v", run.Invocation)
	}
}

func TestGoProviderWorkspaceAndFingerprintInputs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.work", "go 1.26\nuse ./services/api\n")
	app := filepath.Join(root, "services", "api")
	write(t, app, "go.mod", "module example.com/api\n\ngo 1.26\n")
	write(t, app, "go.sum", "example checksum\n")
	write(t, filepath.Join(app, "cmd"), ".keep", "")

	project, err := Discover(filepath.Join(app, "cmd"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Location.ProjectRoot != app || project.Location.WorkspaceRoot != root {
		t.Fatalf("location = %+v, want project=%s workspace=%s", project.Location, app, root)
	}
	if project.Providers[0].WorkspaceRoot != root {
		t.Fatalf("provider workspace = %q, want %q", project.Providers[0].WorkspaceRoot, root)
	}
}

func TestGoProviderFoundBelowWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "services", "api")
	write(t, app, "go.mod", "module example.com/api\n\ngo 1.26\n")
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "go:builtin:build")
	if cmd.Invocation.WorkingDir != app {
		t.Fatalf("go command dir = %q, want %q", cmd.Invocation.WorkingDir, app)
	}
}


func TestGoWorkspaceRootDiscoversModuleNotVirtualRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.work", "go 1.26\nuse ./services/api\n")
	app := filepath.Join(root, "services", "api")
	write(t, app, "go.mod", "module example.com/api\n\ngo 1.26\n")
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 1 || project.Providers[0].ID != "go" {
		t.Fatalf("providers = %+v, want one Go module provider", project.Providers)
	}
	if project.Providers[0].Root != app || project.Providers[0].WorkspaceRoot != root {
		t.Fatalf("provider = %+v, want root=%q workspace=%q", project.Providers[0], app, root)
	}
	for _, cmd := range project.Commands {
		if cmd.Provider == "go" && cmd.Invocation.WorkingDir != app {
			t.Fatalf("Go command %q runs in %q, want module root %q", cmd.ID, cmd.Invocation.WorkingDir, app)
		}
	}
}

func TestGoWorkAloneDoesNotCreateInvalidCommands(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.work", "go 1.26\n")

	project, err := Discover(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range project.Providers {
		if provider.ID == "go" {
			t.Fatalf("go.work without an in-depth go.mod should not create a provider: %+v", provider)
		}
	}
}
