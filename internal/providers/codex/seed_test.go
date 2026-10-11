package codex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const hostConfig = `
model = "gpt-6.1-sol"
model_reasoning_effort = "high"
model_provider = "deepseek"
cli_auth_credentials_store = "keyring"
forced_login_method = "chatgpt"
forced_chatgpt_workspace_id = "ws-host"
profile = "fast"

[tui]
theme = "dark"

[features]
web_search = true

[mcp_servers.docs]
command = "docs-mcp"

[profiles.fast]
model = "gpt-5.5"

[projects."/home/me/secret-repo"]
trust_level = "trusted"

[model_providers.deepseek]
base_url = "https://api.deepseek.invalid"
env_key = "DEEPSEEK_API_KEY"

[model_providers.bearer]
base_url = "https://bearer.invalid"
experimental_bearer_token = "sk-bearer-secret"

[model_providers.local]
base_url = "http://localhost:11434/v1"
`

// hostCodexHome builds a realistic ~/.codex with everything Bonsai must not copy.
func hostCodexHome(t *testing.T, dir string) {
	t.Helper()
	for name, content := range map[string]string{
		"config.toml":                       hostConfig,
		"AGENTS.md":                         "be nice",
		"auth.json":                         `{"OPENAI_API_KEY":"sk-host-secret"}`,
		"auth.json.bak":                     "old",
		"history.jsonl":                     "{}",
		"session_index.jsonl":               "{}",
		"state_5.sqlite":                    "db",
		"logs_2.sqlite-wal":                 "db",
		"installation_id":                   "id",
		"models_cache.json":                 "{}",
		"deepseek.config.toml":              "key",
		"deepseek.key.gpg":                  "gpg",
		".secrets":                          "s",
		"sessions/2026/10/rollout-1.json":   "{}",
		"cache/x":                           "c",
		"plugins/p/plugin.json":             "{}",
		"packages/x":                        "p",
		"shell_snapshots/x":                 "s",
		"thread-writer-locks/x":             "l",
		"app-server-control/sock":           "s",
		"log/codex.log":                     "l",
		"prompts/review.md":                 "review",
		"skills/my-skill/SKILL.md":          "skill",
		"skills/.system/builtin/SKILL.md":   "managed by codex",
		"skills/my-skill/api.key":           "k",
		"skills/my-skill/cert.pem":          "k",
		"skills/my-skill/other.config.toml": "k",
		"rules/default.rules":               "rules",
	} {
		writeFile(t, filepath.Join(dir, name), content, 0o644)
	}
	writeFile(t, filepath.Join(dir, "skills/my-skill/run.sh"), "#!/bin/sh\n", 0o755)
	if err := os.Symlink(filepath.Join(dir, "auth.json"), filepath.Join(dir, "skills", "my-skill", "linked.json")); err != nil {
		t.Fatal(err)
	}
}

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	return files
}

func TestSeedCopiesOnlyTheAllowlist(t *testing.T) {
	e := newTestEnv(t)
	hostCodexHome(t, filepath.Join(e.home, ".codex"))
	before := treeHash(t, e.home)
	account := e.addLogin("work")
	home := e.profileHome(account)

	want := []string{"AGENTS.md", "auth.json", "config.toml", "prompts/review.md", "rules/default.rules", "skills/my-skill/SKILL.md", "skills/my-skill/run.sh"}
	if got := listTree(t, home); !slices.Equal(got, want) { // auth.json is the fake login's own
		t.Fatalf("profile files = %v\nwant %v", got, want)
	}
	if data, _ := os.ReadFile(authPath(home)); strings.Contains(string(data), "sk-host-secret") {
		t.Fatal("host auth.json was copied")
	}
	for name, mode := range map[string]os.FileMode{"AGENTS.md": 0o600, "skills/my-skill/run.sh": 0o700, "config.toml": 0o600} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %v, %v; want %v", name, info.Mode().Perm(), err, mode)
		}
	}
	report := e.out.String()
	for _, want := range []string{"seeded profile from " + filepath.Join(e.home, ".codex"), "skills/ (2 files)", "skipped skills/my-skill/linked.json (symlink)", "skipped skills/my-skill/api.key (credential file)", "skipped config.toml projects"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report lacks %q:\n%s", want, report)
		}
	}
	if settings, _ := ParseSettings(account); settings.SeededFrom != filepath.Join(e.home, ".codex") {
		t.Fatalf("seeded_from = %q", settings.SeededFrom)
	}
	if treeHash(t, e.home) != before {
		t.Fatal("the host ~/.codex changed")
	}
}

func TestSeededConfigIsFiltered(t *testing.T) {
	e := newTestEnv(t)
	hostCodexHome(t, filepath.Join(e.home, ".codex"))
	account := e.addLogin("work")
	data, err := os.ReadFile(configPath(e.profileHome(account)))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"forced_login_method", "forced_chatgpt_workspace_id", "profile", "profiles", "projects", "model_provider"} {
		if _, ok := config[key]; ok {
			t.Errorf("%s survived", key)
		}
	}
	if config["cli_auth_credentials_store"] != "file" {
		t.Errorf("store = %v", config["cli_auth_credentials_store"])
	}
	if config["model"] != "gpt-6.1-sol" || config["model_reasoning_effort"] != "high" {
		t.Errorf("model settings lost: %v", config)
	}
	for _, key := range []string{"tui", "features", "mcp_servers"} {
		if _, ok := config[key].(map[string]any); !ok {
			t.Errorf("[%s] lost", key)
		}
	}
	providers, _ := config["model_providers"].(map[string]any)
	if _, ok := providers["local"]; !ok || len(providers) != 1 {
		t.Errorf("model_providers = %v, want only local", providers)
	}
	for _, leak := range []string{"sk-bearer-secret", "DEEPSEEK_API_KEY", "secret-repo", "ws-host"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("config.toml leaks %q", leak)
		}
	}
}

func TestFilterConfigKeepsSelectedProviderWithoutCredentials(t *testing.T) {
	out, dropped, err := filterConfig([]byte("model_provider = \"local\"\n[model_providers.local]\nbase_url = \"http://localhost\"\n"))
	if err != nil || len(dropped) != 0 || !strings.Contains(string(out), "model_provider = 'local'") {
		t.Fatalf("out = %s, dropped = %v, err = %v", out, dropped, err)
	}
	if _, _, err := filterConfig([]byte("not = = toml")); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}

func TestInvalidHostConfigIsSkipped(t *testing.T) {
	e := newTestEnv(t)
	writeFile(t, filepath.Join(e.home, ".codex", "config.toml"), "not = = toml", 0o644)
	account := e.addLogin("work")
	if !strings.Contains(e.out.String(), "skipped config.toml (invalid TOML)") {
		t.Fatalf("report = %q", e.out.String())
	}
	data, _ := os.ReadFile(configPath(e.profileHome(account)))
	if strings.TrimSpace(string(data)) != "cli_auth_credentials_store = 'file'" {
		t.Fatalf("config.toml = %q", data)
	}
}

func TestNoSeed(t *testing.T) {
	e := newTestEnv(t)
	hostCodexHome(t, filepath.Join(e.home, ".codex"))
	no := false
	account := e.add("work", agents.SetupOptions{Seed: &no})
	if got := listTree(t, e.profileHome(account)); !slices.Equal(got, []string{"auth.json", "config.toml"}) {
		t.Fatalf("profile files = %v", got)
	}
	if strings.Contains(e.out.String(), "seed") {
		t.Fatalf("report = %q", e.out.String())
	}
}

func TestSeedFromHostCodexHomeAndExplicitDir(t *testing.T) {
	e := newTestEnv(t)
	host := t.TempDir()
	writeFile(t, filepath.Join(host, "AGENTS.md"), "host home", 0o644)
	t.Setenv("CODEX_HOME", host)
	account := e.addLogin("work")
	if data, _ := os.ReadFile(filepath.Join(e.profileHome(account), "AGENTS.md")); string(data) != "host home" {
		t.Fatalf("AGENTS.md = %q", data)
	}

	explicit := t.TempDir()
	writeFile(t, filepath.Join(explicit, "AGENTS.md"), "explicit", 0o644)
	other := e.add("other", agents.SetupOptions{SeedFrom: explicit})
	if data, _ := os.ReadFile(filepath.Join(e.profileHome(other), "AGENTS.md")); string(data) != "explicit" {
		t.Fatalf("AGENTS.md = %q", data)
	}

	if _, err := e.service.Setup(t.Context(), ProviderID, "missing", agents.SetupOptions{SeedFrom: filepath.Join(explicit, "nope")}); err == nil {
		t.Fatal("a missing explicit seed source must fail")
	}
}

func TestMissingDefaultSourceIsNotAnError(t *testing.T) {
	e := newTestEnv(t)
	e.addLogin("work")
	if !strings.Contains(e.out.String(), "nothing to seed") {
		t.Fatalf("report = %q", e.out.String())
	}
}

func TestSeedSizeCap(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "AGENTS.md"), strings.Repeat("a", 40), 0o644)
	writeFile(t, filepath.Join(src, "prompts", "big.md"), strings.Repeat("b", 100), 0o644)
	report, err := seedProfile(src, dst, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.copied, []string{"AGENTS.md"}) || len(report.skipped) != 1 || !strings.Contains(report.skipped[0], "size limit") {
		t.Fatalf("report = %+v", report)
	}
}
