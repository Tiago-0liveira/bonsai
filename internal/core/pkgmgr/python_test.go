package pkgmgr

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPythonUVProviderAndProjectScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pyproject.toml", `[project]
name = "demo"
version = "0.1.0"

[project.scripts]
serve = "demo.cli:main"
`)
	write(t, dir, "uv.lock", "version = 1\n")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Providers) != 1 || project.Providers[0].ID != "python:uv" {
		t.Fatalf("providers = %+v, want python:uv", project.Providers)
	}
	serve := commandByID(t, project, "python:script:serve")
	if serve.Invocation.Program != "uv" || !reflect.DeepEqual(serve.Invocation.Prefix, []string{"run", "serve"}) {
		t.Fatalf("serve invocation = %+v", serve.Invocation)
	}
	if serve.Raw != "demo.cli:main" || serve.Source.Kind != "pyproject.toml" {
		t.Fatalf("serve metadata = %+v", serve)
	}
	test := commandByID(t, project, "python:builtin:test")
	if test.Invocation.Program != "uv" || !reflect.DeepEqual(test.Invocation.Prefix, []string{"run", "python", "-m", "pytest"}) {
		t.Fatalf("python test invocation = %+v", test.Invocation)
	}
}

func TestPythonPoetryScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pyproject.toml", `[tool.poetry]
name = "demo"
version = "0.1.0"

[tool.poetry.scripts]
demo = "demo.cli:main"
`)
	write(t, dir, "poetry.lock", "")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Providers[0].ID != "python:poetry" {
		t.Fatalf("provider = %q, want python:poetry", project.Providers[0].ID)
	}
	demo := commandByID(t, project, "python:script:demo")
	if demo.Invocation.Program != "poetry" || !reflect.DeepEqual(demo.Invocation.Prefix, []string{"run", "demo"}) {
		t.Fatalf("poetry script invocation = %+v", demo.Invocation)
	}
}

func TestPythonPlainRequirementsInstall(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "requirements.txt", "pytest\n")

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Providers[0].ID != "python:python" {
		t.Fatalf("provider = %q, want python:python", project.Providers[0].ID)
	}
	install := commandByID(t, project, "python:builtin:install")
	if install.Invocation.Program != "python" ||
		!reflect.DeepEqual(install.Invocation.Prefix, []string{"-m", "pip", "install", "-r", "requirements.txt"}) {
		t.Fatalf("install invocation = %+v", install.Invocation)
	}
}

func TestPythonProviderFoundAtDepthTwo(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "services", "worker")
	write(t, app, "pyproject.toml", `[project]
name = "worker"
version = "0.1.0"
`)
	depth2 := 2
	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "python:builtin:test")
	if cmd.Invocation.WorkingDir != app {
		t.Fatalf("python command dir = %q, want %q", cmd.Invocation.WorkingDir, app)
	}
}
