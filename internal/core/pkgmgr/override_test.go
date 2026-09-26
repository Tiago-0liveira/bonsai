package pkgmgr

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOverridesEnrichHideDefineAndReplace(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","test":"vitest","build":"vite build"}}`)
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
    - id: node:script:build
      command:
        program: custom-build
        args: ["--project", "web"]
        working_dir: tools
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
	port := argByID(t, dev, "port")
	if port.Type != ValueInt || port.Kind != ArgumentFlag || !reflect.DeepEqual(port.Flags, []string{"--port"}) {
		t.Fatalf("port arg = %+v", port)
	}
	if dev.Source.Kind != "override" || dev.Source.File != filepath.Join(dir, ".bonsai.yaml") || dev.Confidence != ConfidenceExact {
		t.Fatalf("override provenance = source=%+v confidence=%q", dev.Source, dev.Confidence)
	}

	for _, cmd := range project.Commands {
		if cmd.ID == "node:script:test" {
			t.Fatal("hidden command was retained")
		}
	}

	build := commandByID(t, project, "node:script:build")
	if build.Invocation.Program != "custom-build" ||
		!reflect.DeepEqual(build.Invocation.Prefix, []string{"--project", "web"}) ||
		build.Invocation.WorkingDir != filepath.Join(dir, "tools") {
		t.Fatalf("replacement invocation = %+v", build.Invocation)
	}

	seed := commandByID(t, project, "project:seed")
	if seed.Provider != "override" || seed.Kind != CommandOverride {
		t.Fatalf("custom command metadata = %+v", seed)
	}
	if seed.Invocation.Program != "pnpm" || !reflect.DeepEqual(seed.Invocation.Prefix, []string{"db:seed"}) {
		t.Fatalf("custom invocation = %+v", seed.Invocation)
	}
}

func TestOverrideValidation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "invalid command id",
			yaml: `pkgmgr:
  commands:
    - id: "bad id"
      command:
        program: echo
`,
			want: "invalid override command id",
		},
		{
			name: "duplicate argument ids",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: value
          type: string
        - id: value
          type: int
`,
			want: "duplicate argument id",
		},
		{
			name: "invalid enum",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: mode
          type: enum
`,
			want: "requires choices",
		},
		{
			name: "invalid enum default",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: mode
          type: enum
          choices: [safe, fast]
          default: turbo
`,
			want: "is not a choice",
		},
		{
			name: "duplicate positional order",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: first
          kind: positional
          position: 0
        - id: second
          kind: positional
          position: 0
`,
			want: "share position",
		},
		{
			name: "invalid kind",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: value
          kind: magical
`,
			want: "invalid kind",
		},
		{
			name: "invalid type",
			yaml: `pkgmgr:
  commands:
    - id: project:x
      command:
        program: echo
      args:
        - id: value
          type: object
`,
			want: "invalid type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
			write(t, dir, ".bonsai.yaml", tc.yaml)
			_, err := Discover(dir, Options{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestOverridePrecedenceOverDiscoveredMetadata(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
	write(t, dir, ".bonsai.yaml", `pkgmgr:
  commands:
    - id: node:script:dev
      description: overridden
      args:
        - id: args
          description: custom passthrough
          kind: passthrough
          type: unknown
`)
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cmd := commandByID(t, project, "node:script:dev")
	if cmd.Description != "overridden" || cmd.Source.Kind != "override" {
		t.Fatalf("command override did not win: %+v", cmd)
	}
	args := argByID(t, cmd, "args")
	if args.Description != "custom passthrough" || args.Source.Kind != "override" {
		t.Fatalf("argument override did not win: %+v", args)
	}
}

func TestOverrideWorkingDirUsesConfigDirectoryInMultiProjectRepo(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "apps", "admin"), "package.json", `{"scripts":{"dev":"vite"}}`)
	write(t, filepath.Join(root, "apps", "web"), "package.json", `{"scripts":{"test":"vitest"}}`)
	write(t, root, ".bonsai.yaml", `pkgmgr:
  search_depth: 2
  commands:
    - id: project:seed
      command:
        program: pnpm
        args: ["db:seed"]
        working_dir: tools
`)
	depth2 := 2
	project, err := Discover(root, Options{SearchDepth: &depth2})
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(project.Location.ProjectRoot, root) {
		t.Fatalf("ProjectRoot = %q, want equivalent to %q", project.Location.ProjectRoot, root)
	}
	seed := commandByID(t, project, "project:seed")
	if !samePath(seed.ProjectRoot, root) {
		t.Fatalf("override ProjectRoot = %q, want equivalent to %q", seed.ProjectRoot, root)
	}
	want := filepath.Join(root, "tools")
	if seed.Invocation.WorkingDir != want {
		t.Fatalf("override working dir = %q, want config-rooted %q", seed.Invocation.WorkingDir, want)
	}
}
