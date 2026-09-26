package pkgmgr

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

type runnerCall struct {
	dir     string
	program string
	args    []string
}

type fakeRunner struct {
	output []byte
	err    error
	calls  []runnerCall
}

func (f *fakeRunner) Run(_ context.Context, dir, program string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, runnerCall{dir: dir, program: program, args: append([]string(nil), args...)})
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.output...), nil
}

const cargoMetadataFixture = `{
  "packages": [
    {
      "id": "api 0.1.0 (path+file:///repo/api)",
      "name": "api",
      "features": {"default": [], "server": [], "tls": []},
      "targets": [
        {"name": "server", "kind": ["bin"]},
        {"name": "worker", "kind": ["bin"]},
        {"name": "api_tests", "kind": ["test"]},
        {"name": "demo", "kind": ["example"]},
        {"name": "throughput", "kind": ["bench"]}
      ]
    },
    {
      "id": "lib 0.1.0 (path+file:///repo/lib)",
      "name": "lib",
      "features": {"serde": []},
      "targets": [{"name": "lib", "kind": ["lib"]}]
    }
  ],
  "workspace_members": [
    "api 0.1.0 (path+file:///repo/api)",
    "lib 0.1.0 (path+file:///repo/lib)"
  ]
}`

func TestCargoDetectionAndWorkspace(t *testing.T) {
	t.Run("single crate", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
		project, err := Discover(dir, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(project.Providers) != 1 || project.Providers[0].ID != "cargo" {
			t.Fatalf("providers = %+v", project.Providers)
		}
		if project.Location.ProjectRoot != dir || project.Location.WorkspaceRoot != "" {
			t.Fatalf("location = %+v", project.Location)
		}
	})

	t.Run("virtual workspace nested crate", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/app\"]\n")
		app := filepath.Join(root, "crates", "app")
		write(t, app, "Cargo.toml", "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")
		write(t, filepath.Join(app, "src"), "lib.rs", "")

		project, err := Discover(filepath.Join(app, "src"), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if project.Location.ProjectRoot != app || project.Location.WorkspaceRoot != root {
			t.Fatalf("location = %+v, want project=%s workspace=%s", project.Location, app, root)
		}
	})

	t.Run("nearest nested workspace", func(t *testing.T) {
		outer := t.TempDir()
		write(t, outer, "Cargo.toml", "[workspace]\nmembers = [\"groups/team\"]\n")
		inner := filepath.Join(outer, "groups", "team")
		write(t, inner, "Cargo.toml", "[workspace]\nmembers = [\"app\"]\n")
		app := filepath.Join(inner, "app")
		write(t, app, "Cargo.toml", "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")
		write(t, filepath.Join(app, "src"), "main.rs", "")

		project, err := Discover(filepath.Join(app, "src"), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if project.Location.WorkspaceRoot != inner {
			t.Fatalf("WorkspaceRoot = %q, want nearest %q", project.Location.WorkspaceRoot, inner)
		}
	})
}

func TestCargoBuiltinCatalogAndSchemas(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	wantBuiltins := []string{
		"build", "check", "clean", "doc", "fetch", "fix", "metadata", "package",
		"publish", "run", "test", "tree", "update", "vendor", "version",
	}
	for _, name := range wantBuiltins {
		cmd := commandByID(t, project, "cargo:builtin:"+name)
		if cmd.Kind != CommandBuiltin || cmd.Provider != "cargo" || cmd.Confidence != ConfidenceExact {
			t.Fatalf("%s metadata = %+v", name, cmd)
		}
	}

	build := commandByID(t, project, "cargo:builtin:build")
	pkg := argByID(t, build, "package")
	if pkg.Type != ValueString || !reflect.DeepEqual(pkg.Flags, []string{"-p", "--package"}) {
		t.Fatalf("package arg = %+v", pkg)
	}
	if release := argByID(t, build, "release"); release.Type != ValueBool {
		t.Fatalf("release arg = %+v", release)
	}
	if jobs := argByID(t, build, "jobs"); jobs.Type != ValueInt {
		t.Fatalf("jobs arg = %+v", jobs)
	}

	run := commandByID(t, project, "cargo:builtin:run")
	if bin := argByID(t, run, "bin"); bin.Type != ValueString || !reflect.DeepEqual(bin.Flags, []string{"--bin"}) {
		t.Fatalf("bin arg = %+v", bin)
	}
	if pass := argByID(t, run, "args"); pass.Kind != ArgumentPassThrough || !pass.Variadic {
		t.Fatalf("run passthrough = %+v", pass)
	}
	if run.Invocation.PassThrough != PassThroughDoubleDash {
		t.Fatalf("run pass-through mode = %q", run.Invocation.PassThrough)
	}

	version := commandByID(t, project, "cargo:builtin:version")
	if len(version.Args) != 0 {
		t.Fatalf("cargo version args = %+v, want none", version.Args)
	}
}

func TestCargoMetadataRunnerAndEnrichment(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"api\"\nversion = \"0.1.0\"\n")
	runner := &fakeRunner{output: []byte(cargoMetadataFixture)}

	project, err := Discover(dir, Options{AllowProviderCLI: true, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.dir != dir || call.program != "cargo" || !reflect.DeepEqual(call.args, []string{"metadata", "--format-version", "1", "--no-deps"}) {
		t.Fatalf("runner call = %+v", call)
	}

	run := commandByID(t, project, "cargo:builtin:run")
	pkg := argByID(t, run, "package")
	if pkg.Type != ValueEnum || !reflect.DeepEqual(pkg.Choices, []string{"api", "lib"}) {
		t.Fatalf("package choices = %+v", pkg)
	}
	bin := argByID(t, run, "bin")
	if bin.Type != ValueEnum || !reflect.DeepEqual(bin.Choices, []string{"server", "worker"}) {
		t.Fatalf("bin choices = %+v", bin)
	}
	if bin.Source.Kind != "cargo metadata" || bin.Confidence != ConfidenceExact {
		t.Fatalf("bin provenance = source=%+v confidence=%q", bin.Source, bin.Confidence)
	}
	test := argByID(t, commandByID(t, project, "cargo:builtin:test"), "test")
	if test.Type != ValueEnum || !reflect.DeepEqual(test.Choices, []string{"api_tests"}) {
		t.Fatalf("test choices = %+v", test)
	}

	meta, err := readCargoMetadata(&fakeRunner{output: []byte(cargoMetadataFixture)}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta.Examples, []string{"demo"}) || !reflect.DeepEqual(meta.Benches, []string{"throughput"}) {
		t.Fatalf("metadata examples/benches = %+v", meta)
	}
	if !reflect.DeepEqual(meta.Features, []string{"default", "serde", "server", "tls"}) {
		t.Fatalf("features = %v", meta.Features)
	}
}

func TestCargoMetadataFailuresDegradeToStaticDiscovery(t *testing.T) {
	tests := []struct {
		name   string
		runner *fakeRunner
	}{
		{name: "cargo missing", runner: &fakeRunner{err: errors.New("executable not found")}},
		{name: "timeout", runner: &fakeRunner{err: context.DeadlineExceeded}},
		{name: "non-zero", runner: &fakeRunner{err: errors.New("exit status 1")}},
		{name: "malformed json", runner: &fakeRunner{output: []byte("{")}},
		{name: "oversized output", runner: &fakeRunner{output: bytes.Repeat([]byte("x"), providerStdoutLimit+1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
			project, err := Discover(dir, Options{AllowProviderCLI: true, Runner: tc.runner})
			if err != nil {
				t.Fatalf("Discover should degrade gracefully: %v", err)
			}
			run := commandByID(t, project, "cargo:builtin:run")
			if bin := argByID(t, run, "bin"); bin.Type != ValueString || len(bin.Choices) != 0 {
				t.Fatalf("static bin schema after metadata failure = %+v", bin)
			}
		})
	}
}

func TestCargoProviderCLIDisabledDoesNotCallRunner(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\nversion = \"0.1.0\"\n")
	runner := &fakeRunner{err: errors.New("must not be called")}
	project, err := Discover(dir, Options{AllowProviderCLI: false, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	commandByID(t, project, "cargo:builtin:build")
	if len(runner.calls) != 0 {
		t.Fatalf("runner was called during passive discovery: %+v", runner.calls)
	}
}

func TestRustCargoProviderFoundAtDepthTwo(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "crates", "api")
	write(t, app, "Cargo.toml", "[package]\nname = \"api\"\nversion = \"0.1.0\"\n")
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 1 || project.Providers[0].ID != "cargo" {
		t.Fatalf("providers = %+v, want cargo/Rust provider", project.Providers)
	}
	cmd := commandByID(t, project, "cargo:builtin:test")
	if cmd.Invocation.WorkingDir != app {
		t.Fatalf("cargo command dir = %q, want %q", cmd.Invocation.WorkingDir, app)
	}
}

func TestCargoWorkspaceRootDoesNotDuplicateMemberCatalog(t *testing.T) {
	root := t.TempDir()
	write(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/app\"]\n")
	app := filepath.Join(root, "crates", "app")
	write(t, app, "Cargo.toml", "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")
	depth2 := 2

	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	var cargoProviders int
	for _, provider := range project.Providers {
		if provider.ID == "cargo" {
			cargoProviders++
		}
	}
	if cargoProviders != 1 {
		t.Fatalf("cargo providers = %d, want 1: %+v", cargoProviders, project.Providers)
	}
	if got := commandByID(t, project, "cargo:builtin:test").Invocation.WorkingDir; got != root {
		t.Fatalf("cargo workspace command dir = %q, want %q", got, root)
	}
}
