package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func TestSetScalarStylesAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, input, value, want string
	}{
		{"plain with comment", "key: old  # keep [a,b]\n\nother: keep\n", "new", "key: new  # keep [a,b]\n\nother: keep\n"},
		{"plain punctuation", "key: command {a,b} # keep\n\nother: keep\n", "next {c,d}", "key: next {c,d} # keep\n\nother: keep\n"},
		{"double quoted", "key: \"old\" # keep\n", "new", "key: \"new\" # keep\n"},
		{"escaped double quote", "key: \"o\\\"ld\" # keep\n", "new", "key: \"new\" # keep\n"},
		{"single quoted", "key: 'it''s old' # keep\n", "it's new", "key: 'it''s new' # keep\n"},
		{"needs quoting", "key: old\n\nother: keep\n", "value: with colon", "key: 'value: with colon'\n\nother: keep\n"},
		{"unicode column", "clé: old # keep\n", "new", "clé: new # keep\n"},
		{"CRLF", "key: old  # keep\r\n\r\nother: keep\r\n", "new", "key: new  # keep\r\n\r\nother: keep\r\n"},
		{"no final newline", "key: old # keep", "new", "key: new # keep"},
		{"empty quoted", "key: \"\"\n", "new", "key: \"new\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := writeFixture(t, tc.input)
			key := "key"
			if tc.name == "unicode column" {
				key = "clé"
			}
			if err := Set(f, key, tc.value); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
			var values map[string]string
			if err := yaml.Unmarshal(got, &values); err != nil || values[key] != tc.value {
				t.Fatalf("decoded value = %q, error = %v", values[key], err)
			}
		})
	}
}

func TestSetPreservesCommentedRepoLayout(t *testing.T) {
	original, err := os.ReadFile("testdata/commented-repo-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f := writeFixture(t, string(original))
	if err := Set(f, "theme.preset", "nord"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(original, []byte(`  preset: "bonsai"`), []byte(`  preset: "nord"`), 1)
	if bytes.Equal(want, original) {
		t.Fatal("fixture does not contain the expected theme line")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Set changed unrelated layout:\n%s", got)
	}
}

func TestWriteFallbackUsesTwoSpaceIndent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(string) error
		want  string
	}{
		{"new mapping", func(f string) error { return Set(f, "theme.preset", "nord") }, "theme:\n  preset: nord\n"},
		{"sequence", func(f string) error { return ListSet(f, "hooks.create", []string{"echo hi"}) }, "hooks:\n  create:\n    - echo hi\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := writeFixture(t, "")
			if err := tc.write(f); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetFlowMappingFallsBackSafely(t *testing.T) {
	f := writeFixture(t, "theme: {preset: old # comment\n}\n")
	if err := Set(f, "theme.preset", "a,b"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Theme struct{ Preset string }
	}
	if err := yaml.Unmarshal(data, &got); err != nil || got.Theme.Preset != "a,b" {
		t.Fatalf("flow value changed: %s, %v", data, err)
	}
}

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
