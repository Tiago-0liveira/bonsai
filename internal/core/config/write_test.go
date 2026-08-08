package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const annotatedFixture = `# bonsai project config
upstream: origin/main # base for metrics

notifications:
  # desktop notifications for processes
  process: true
  ci: false
`

// writeFixture writes content to a fresh .bonsai.yaml and returns its path.
func writeFixture(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), ".bonsai.yaml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSetPreservesComments(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "notifications.ci", "true"); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# bonsai project config", "# base for metrics", "# desktop notifications for processes"} {
		if !strings.Contains(s, want) {
			t.Errorf("comment lost after Set: %q\nfile:\n%s", want, s)
		}
	}

	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.CI {
		t.Error("notifications.ci not persisted as true")
	}
	if !cfg.Notifications.Process {
		t.Error("notifications.process changed unexpectedly")
	}
}

func TestSetCreatesNestedKeys(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "worktree.root", "../worktrees"); err != nil {
		t.Fatal(err)
	}
	if err := Set(f, "worktree.path_template", "{repo}-{branch}"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.Root != "../worktrees" || cfg.Worktree.PathTemplate != "{repo}-{branch}" {
		t.Errorf("worktree config = %+v", cfg.Worktree)
	}
}

func TestSetBoolRoundTrip(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "confirm_destructive", "false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfirmDestructive {
		t.Error("confirm_destructive not persisted as false")
	}
}

func TestSetCreatesMissingFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".bonsai.yaml")
	if err := Set(f, "upstream", "origin/develop"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != "origin/develop" {
		t.Errorf("upstream = %q", cfg.Upstream)
	}
}

func TestSetRejectsScalarMidPath(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "upstream.deeper", "x"); err == nil {
		t.Error("expected an error extending a scalar key")
	}
}

func TestUnsetRemovesKey(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "theme.preset", "nord"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(f, "theme.preset"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Preset != "" {
		t.Errorf("theme.preset still %q after Unset", cfg.Theme.Preset)
	}

	// Unsetting a missing key (and a missing path prefix) is a no-op.
	if err := Unset(f, "theme.preset"); err != nil {
		t.Errorf("repeat Unset: %v", err)
	}
	if err := Unset(f, "no.such.key"); err != nil {
		t.Errorf("Unset missing path: %v", err)
	}
}

func TestListSetHooks(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := ListSet(f, "hooks.on_worktree_create", []string{"npm ci", "cp ../.env ."}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.CreateHooks()
	if len(got) != 2 || got[0] != "npm ci" || got[1] != "cp ../.env ." {
		t.Errorf("create hooks = %v", got)
	}

	// Replace shrinks the list.
	if err := ListSet(f, "hooks.on_worktree_create", []string{"pnpm i"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CreateHooks(); len(got) != 1 || got[0] != "pnpm i" {
		t.Errorf("replaced hooks = %v", got)
	}
}

func TestSetMapEntry(t *testing.T) {
	f := writeFixture(t, annotatedFixture)
	if err := Set(f, "theme.overrides.accent", "#ff0000"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Overrides["accent"] != "#ff0000" {
		t.Errorf("overrides = %v", cfg.Theme.Overrides)
	}
}
