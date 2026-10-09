package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// fillClaudeDir builds a fake ~/.claude with allowed and forbidden items.
func fillClaudeDir(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "settings.json"), `{"model":"opus","theme":"dark"}`, 0o644)
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "be nice", 0o644)
	writeFile(t, filepath.Join(dir, "keybindings.json"), "{}", 0o644)
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), "agent", 0o644)
	writeFile(t, filepath.Join(dir, "commands", "sub", "ship.md"), "command", 0o644)
	writeFile(t, filepath.Join(dir, "skills", "x", "run.sh"), "#!/bin/sh", 0o755)
	writeFile(t, filepath.Join(dir, "output-styles", "terse.md"), "style", 0o644)
	// never copied
	writeFile(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"SECRET"}}`, 0o600)
	writeFile(t, filepath.Join(dir, "projects", "p", "chat.jsonl"), "history", 0o644)
	writeFile(t, filepath.Join(dir, "history.jsonl"), "history", 0o644)
	writeFile(t, filepath.Join(dir, "sessions", "s.json"), "s", 0o644)
	writeFile(t, filepath.Join(dir, "todos", "t.json"), "t", 0o644)
	writeFile(t, filepath.Join(dir, "shell-snapshots", "s.sh"), "s", 0o644)
	writeFile(t, filepath.Join(dir, "statsig", "s"), "s", 0o644)
	writeFile(t, filepath.Join(dir, "backups", "b"), "b", 0o644)
	writeFile(t, filepath.Join(dir, "ide", "i"), "i", 0o644)
	writeFile(t, filepath.Join(dir, "plugins", "installed_plugins.json"), `{"/abs/path":1}`, 0o644)
}

const claudeJSON = `{"mcpServers":{"docs":{"command":"docs-mcp"}},"oauthAccount":{"emailAddress":"me@example.com"},"userID":"u","projects":{"/x":{"mcpServers":{"leak":{}}}}}`

func assertSeeded(t *testing.T, dst string) {
	t.Helper()
	for _, want := range []string{"settings.json", "CLAUDE.md", "keybindings.json", "agents/reviewer.md", "commands/sub/ship.md", "skills/x/run.sh", "output-styles/terse.md"} {
		if !exists(filepath.Join(dst, want)) {
			t.Errorf("%s not copied", want)
		}
	}
	for _, banned := range []string{".credentials.json", "projects", "history.jsonl", "sessions", "todos", "shell-snapshots", "statsig", "backups", "ide", "plugins"} {
		if exists(filepath.Join(dst, banned)) {
			t.Errorf("%s must not be copied", banned)
		}
	}
	data, err := os.ReadFile(filepath.Join(dst, ".claude.json"))
	if err != nil {
		t.Fatalf("mcp servers not seeded: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["mcpServers"] == nil || strings.Contains(string(data), "me@example.com") || strings.Contains(string(data), "leak") {
		t.Fatalf("only mcpServers may be copied from .claude.json: %s", data)
	}
	if info, _ := os.Stat(filepath.Join(dst, "settings.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("settings.json mode = %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Join(dst, "skills", "x", "run.sh")); info.Mode().Perm() != 0o700 {
		t.Errorf("skill script lost its owner exec bit: %v", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Join(dst, "agents")); info.Mode().Perm() != 0o700 {
		t.Errorf("agents dir mode = %v", info.Mode().Perm())
	}
}

func TestSeedFromDefaultHomeClaudeDir(t *testing.T) {
	e := newTestEnv(t)
	fillClaudeDir(t, filepath.Join(e.home, ".claude"))
	writeFile(t, filepath.Join(e.home, ".claude.json"), claudeJSON, 0o600)
	before := snapshotFiles(t, e.home)

	account := e.addSeeded("work", agents.SetupOptions{})
	dst := configDir(e.accounts, account)
	assertSeeded(t, dst)
	if !strings.Contains(e.out.String(), "seeded profile from") || !strings.Contains(e.out.String(), "settings.json") {
		t.Fatalf("seeding not reported: %q", e.out.String())
	}
	settings, _ := ParseSettings(account)
	if settings.SeededFrom != filepath.Join(e.home, ".claude") {
		t.Fatalf("seeded_from = %q", settings.SeededFrom)
	}
	if after := snapshotFiles(t, e.home); after != before {
		t.Fatalf("source was modified:\n%s\n--\n%s", before, after)
	}
}

func snapshotFiles(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			rel, _ := filepath.Rel(root, path)
			sb.WriteString(rel + "=" + string(data) + "\n")
		}
		return nil
	})
	return sb.String()
}

func TestSeedHonoursHostConfigDir(t *testing.T) {
	e := newTestEnv(t)
	custom := t.TempDir()
	fillClaudeDir(t, custom)
	writeFile(t, filepath.Join(custom, ".claude.json"), claudeJSON, 0o600)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	// a decoy in the default location must be ignored
	writeFile(t, filepath.Join(e.home, ".claude", "CLAUDE.md"), "decoy", 0o644)

	account := e.addSeeded("work", agents.SetupOptions{})
	assertSeeded(t, configDir(e.accounts, account))
	if data, _ := os.ReadFile(filepath.Join(configDir(e.accounts, account), "CLAUDE.md")); string(data) != "be nice" {
		t.Fatalf("CLAUDE.md = %q", data)
	}
}

func TestSeedFromExplicitDir(t *testing.T) {
	e := newTestEnv(t)
	src := t.TempDir()
	fillClaudeDir(t, src)
	writeFile(t, filepath.Join(src, ".claude.json"), claudeJSON, 0o600)
	account, err := e.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{SeedFrom: src, Secret: strings.NewReader(testToken)})
	if err != nil {
		t.Fatal(err)
	}
	assertSeeded(t, configDir(e.accounts, account))
	if _, err := e.service.Setup(t.Context(), ProviderID, "other", agents.SetupOptions{SeedFrom: filepath.Join(src, "missing"), Secret: strings.NewReader(testToken)}); err == nil {
		t.Fatal("a missing explicit seed source must fail")
	}
}

func TestNoSeedCopiesNothing(t *testing.T) {
	e := newTestEnv(t)
	fillClaudeDir(t, filepath.Join(e.home, ".claude"))
	no := false
	account, err := e.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{Seed: &no, Secret: strings.NewReader(testToken)})
	if err != nil {
		t.Fatal(err)
	}
	dst := configDir(e.accounts, account)
	for _, name := range []string{"settings.json", "CLAUDE.md", "agents", "commands", "skills"} {
		if exists(filepath.Join(dst, name)) {
			t.Errorf("%s seeded despite --no-seed", name)
		}
	}
	if settings, _ := ParseSettings(account); settings.SeededFrom != "" {
		t.Fatalf("seeded_from = %q", settings.SeededFrom)
	}
}

func TestSeedSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	src, dst := t.TempDir(), t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret", 0o600)
	writeFile(t, filepath.Join(src, "agents", "real.md"), "real", 0o644)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(src, "agents", "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(src, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	report, err := seedProfile(src, nil, dst, maxSeedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dst, "agents", "real.md")) {
		t.Fatal("regular file not copied")
	}
	for _, name := range []string{"agents/link.md", "skills", "CLAUDE.md"} {
		if exists(filepath.Join(dst, name)) {
			t.Errorf("symlink %s was followed", name)
		}
	}
	if len(report.skipped) != 3 {
		t.Fatalf("skipped = %v", report.skipped)
	}
}

func TestSeedSizeCap(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "CLAUDE.md"), strings.Repeat("a", 100), 0o644)
	writeFile(t, filepath.Join(src, "agents", "big.md"), strings.Repeat("b", 600), 0o644)
	writeFile(t, filepath.Join(src, "agents", "small.md"), "s", 0o644)
	report, err := seedProfile(src, nil, dst, 200)
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dst, "agents", "big.md")) {
		t.Fatal("file above the cap was copied")
	}
	var total int64
	_ = filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			total += info.Size()
		}
		return nil
	})
	if total > 200 {
		t.Fatalf("copied %d bytes, cap is 200", total)
	}
	if len(report.skipped) == 0 {
		t.Fatal("cap hit not reported")
	}
}

func TestSeedStripsCredentialSettings(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "settings.json"), `{
		"model":"opus",
		"apiKeyHelper":"/bin/get-key",
		"forceLoginMethod":"claudeai", "forceLoginOrgUUID":"org", "otelHeadersHelper":"/bin/h",
		"awsAuthRefresh":"x",
		"env":{"ANTHROPIC_API_KEY":"sk","ANTHROPIC_BASE_URL":"u","CLAUDE_CODE_USE_BEDROCK":"1","AWS_PROFILE":"p","EDITOR":"vim"}
	}`, 0o644)
	if _, err := seedProfile(src, nil, dst, maxSeedBytes); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "settings.json"))
	var got struct {
		Model  string            `json:"model"`
		Helper string            `json:"apiKeyHelper"`
		AWS    string            `json:"awsAuthRefresh"`
		Force  string            `json:"forceLoginMethod"`
		Org    string            `json:"forceLoginOrgUUID"`
		Otel   string            `json:"otelHeadersHelper"`
		Env    map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "opus" || got.Helper != "" || got.AWS != "" || got.Force != "" || got.Org != "" || got.Otel != "" || len(got.Env) != 1 || got.Env["EDITOR"] != "vim" {
		t.Fatalf("settings = %s", data)
	}
}

func TestSeedInvalidSettingsIsSkipped(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "settings.json"), `{not json`, 0o644)
	writeFile(t, filepath.Join(src, "CLAUDE.md"), "ok", 0o644)
	report, err := seedProfile(src, nil, dst, maxSeedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dst, "settings.json")) || !exists(filepath.Join(dst, "CLAUDE.md")) || len(report.skipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
}

// addSeeded sets a profile up in token mode: unlike a login, it leaves the
// seeded config dir untouched, so tests see exactly what seeding wrote.
func (e *testEnv) addSeeded(name string, options agents.SetupOptions) agents.Account {
	e.t.Helper()
	options.Secret = strings.NewReader(testToken)
	account, err := e.service.Setup(e.t.Context(), ProviderID, name, options)
	if err != nil {
		e.t.Fatal(err)
	}
	return account
}
