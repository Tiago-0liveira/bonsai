package pkgmgr

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNodePackageManagerField(t *testing.T) {
	for _, tc := range []struct {
		field string
		want  string
	}{
		{"npm@11.0.0", "node:npm"},
		{"pnpm@10.17.0", "node:pnpm"},
		{"yarn@4.9.0", "node:yarn"},
		{"bun@1.2.0", "node:bun"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", `{"packageManager":"`+tc.field+`","scripts":{"dev":"vite"}}`)
			write(t, dir, "package-lock.json", "{}")
			project, err := Discover(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(project.Providers) != 1 || project.Providers[0].ID != tc.want {
				t.Fatalf("providers = %+v, want %s", project.Providers, tc.want)
			}
		})
	}
}

func TestNodeLockfileSelection(t *testing.T) {
	for _, tc := range []struct {
		lock string
		want string
	}{
		{"package-lock.json", "node:npm"},
		{"pnpm-lock.yaml", "node:pnpm"},
		{"yarn.lock", "node:yarn"},
		{"bun.lock", "node:bun"},
		{"bun.lockb", "node:bun"},
	} {
		t.Run(tc.lock, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
			write(t, dir, tc.lock, "")
			project, err := Discover(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if project.Providers[0].ID != tc.want {
				t.Fatalf("provider = %s, want %s", project.Providers[0].ID, tc.want)
			}
		})
	}
}

func TestNodeLockfilePriorityAndFallback(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
	write(t, dir, "package-lock.json", "")
	write(t, dir, "yarn.lock", "")
	write(t, dir, "pnpm-lock.yaml", "")
	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Providers[0].ID != "node:pnpm" {
		t.Fatalf("provider = %s, want pnpm priority", project.Providers[0].ID)
	}

	fallback := t.TempDir()
	write(t, fallback, "package.json", `{"scripts":{"dev":"vite"}}`)
	project, err = Discover(fallback, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Providers[0].ID != "node:npm" {
		t.Fatalf("fallback provider = %s, want npm", project.Providers[0].ID)
	}
}

func TestNodeMalformedManifest(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":`)
	_, err := Discover(dir, Options{})
	if err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("error = %v, want package.json parse error", err)
	}
}

func TestNodeScriptsPreserveRawAndProvenance(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{
  "scripts": {
    "build": "vite build",
    "dev": "PORT=3000 vite --host && echo \"ready\"",
    "lint:fix": "eslint . --fix || true; echo done",
    "subshell": "echo $(pwd)",
    "test": "vitest"
  }
}`)

	project, err := Discover(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"node:script:build",
		"node:script:dev",
		"node:script:lint:fix",
		"node:script:subshell",
		"node:script:test",
	}
	var gotIDs []string
	for _, cmd := range project.Commands {
		if strings.HasPrefix(cmd.ID, "node:") {
			gotIDs = append(gotIDs, cmd.ID)
			if cmd.Source.Kind != "package.json" || cmd.Source.File != filepath.Join(dir, "package.json") {
				t.Fatalf("source = %+v", cmd.Source)
			}
			if cmd.Confidence != ConfidenceExact {
				t.Fatalf("confidence = %q", cmd.Confidence)
			}
			args := argByID(t, cmd, "args")
			if args.Kind != ArgumentPassThrough || !args.Variadic || args.Type != ValueUnknown {
				t.Fatalf("pass-through arg = %+v", args)
			}
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("IDs = %v, want %v", gotIDs, wantIDs)
	}
	dev := commandByID(t, project, "node:script:dev")
	if dev.Raw != `PORT=3000 vite --host && echo "ready"` {
		t.Fatalf("raw script = %q", dev.Raw)
	}
}

func TestNodeWorkspaces(t *testing.T) {
	tests := []struct {
		name       string
		rootJSON   string
		extraFile  string
		wantManage string
	}{
		{name: "npm", rootJSON: `{"workspaces":["apps/*"]}`, wantManage: "node:npm"},
		{name: "pnpm", rootJSON: `{"packageManager":"pnpm@10.17.0"}`, extraFile: "pnpm-workspace.yaml", wantManage: "node:pnpm"},
		{name: "yarn", rootJSON: `{"packageManager":"yarn@4.9.0","workspaces":{"packages":["apps/*"]}}`, wantManage: "node:yarn"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "package.json", tc.rootJSON)
			if tc.extraFile != "" {
				write(t, root, tc.extraFile, "packages:\n  - apps/*\n")
			}
			app := filepath.Join(root, "apps", "web")
			write(t, app, "package.json", `{"scripts":{"dev":"vite"}}`)
			nested := filepath.Join(app, "src")
			write(t, nested, ".keep", "")

			project, err := Discover(nested, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if project.Location.ProjectRoot != app || project.Location.WorkspaceRoot != root {
				t.Fatalf("location = %+v, want project=%s workspace=%s", project.Location, app, root)
			}
			if project.Providers[0].ID != tc.wantManage {
				t.Fatalf("provider = %s, want %s", project.Providers[0].ID, tc.wantManage)
			}
			commandByID(t, project, "node:apps/web:script:dev")
			if len(project.Commands) != 1 {
				t.Fatalf("workspace discovery aggregated packages: %+v", project.Commands)
			}
		})
	}
}

func TestNodeNestedWorkspaceUsesNearestRoot(t *testing.T) {
	outer := t.TempDir()
	write(t, outer, "package.json", `{"workspaces":["packages/*"]}`)
	inner := filepath.Join(outer, "packages", "team")
	write(t, inner, "package.json", `{"packageManager":"yarn@4.9.0","workspaces":["apps/*"]}`)
	app := filepath.Join(inner, "apps", "web")
	write(t, app, "package.json", `{"scripts":{"dev":"vite"}}`)
	write(t, filepath.Join(app, "src"), ".keep", "")

	project, err := Discover(filepath.Join(app, "src"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Location.WorkspaceRoot != inner {
		t.Fatalf("WorkspaceRoot = %q, want nearest %q", project.Location.WorkspaceRoot, inner)
	}
	if project.Providers[0].ID != "node:yarn" {
		t.Fatalf("provider = %s, want node:yarn", project.Providers[0].ID)
	}
	commandByID(t, project, "node:apps/web:script:dev")
}
