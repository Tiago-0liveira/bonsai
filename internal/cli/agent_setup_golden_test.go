package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

var goldenAccountID = regexp.MustCompile(`acct_[0-9a-f]{32}`)

// TestAccountAddOutputGolden pins what `bonsai agent account add` prints for
// every auth mode, the seed report and the duplicate-login warning. Setup IO
// moved from construction-time writers to per-request IO; the output must not
// change by a byte.
func TestAccountAddOutputGolden(t *testing.T) {
	codexCLIEnv(t)
	claudeCLIEnv(t) // shares HOME with the codex env; both fakes are on PATH
	home := os.Getenv("HOME")

	// Host setups to seed from, each with one item the seeder skips.
	writeGoldenFile(t, filepath.Join(home, ".codex", "AGENTS.md"), "codex rules\n")
	writeGoldenFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5.5\"\n[projects.\"/x\"]\ntrust_level = \"trusted\"\n")
	writeGoldenFile(t, filepath.Join(home, ".codex", "prompts", "review.md"), "review\n")
	if err := os.Symlink(filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(home, ".codex", "prompts", "link.md")); err != nil {
		t.Fatal(err)
	}
	writeGoldenFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "claude rules\n")
	writeGoldenFile(t, filepath.Join(home, ".claude", "settings.json"), "{\"theme\":\"dark\"}\n")
	writeGoldenFile(t, filepath.Join(home, ".claude", "commands", "ship.md"), "ship\n")
	if err := os.Symlink(filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(home, ".claude", "commands", "link.md")); err != nil {
		t.Fatal(err)
	}
	emptySeed := filepath.Join(home, "empty-seed")
	if err := os.MkdirAll(emptySeed, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FAKE_CODEX_EMAIL", "same@example.com")
	t.Setenv("FAKE_CLAUDE_EMAIL", "same@example.com")
	steps := []struct {
		stdin string
		args  []string
		env   map[string]string // set from this step on
	}{
		{"", []string{"codex", "browser"}, nil},
		{"", []string{"codex", "device", "--auth", "device", "--no-seed"}, nil},
		{"", []string{"codex", "again", "--auth", "chatgpt", "--seed-from", emptySeed}, nil},
		{cliTestAPIKey + "\n", []string{"codex", "key", "--token-stdin"}, nil},
		{cliTestAPIKey, []string{"codex", "key2", "--auth", "api-key", "--token-stdin", "--seed-from", filepath.Join(home, "missing")}, nil},
		{"", []string{"claude", "login"}, nil},
		{"", []string{"claude", "login2", "--no-seed"}, nil},
		{cliTestToken + "\n", []string{"claude", "token", "--token-stdin", "--seed-from", emptySeed}, nil},
		{cliTestToken, []string{"claude", "token2", "--auth", "token", "--token-stdin", "--no-seed"}, nil},
		{"", []string{"codex", "browser", "--no-seed"}, nil},
		{"", []string{"claude", "bad name"}, nil},
		{"", []string{"codex", "nohome"}, map[string]string{"CODEX_HOME": filepath.Join(home, "no-codex")}},
		{"", []string{"claude", "nohome"}, map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(home, "no-claude")}},
	}
	var got strings.Builder
	for _, step := range steps {
		for key, value := range step.env {
			t.Setenv(key, value)
		}
		out, errOut, err := runAgent(t, step.stdin, append([]string{"account", "add"}, step.args...)...)
		fmt.Fprintf(&got, "$ bonsai agent account add %s\n", strings.Join(step.args, " "))
		fmt.Fprintf(&got, "stdout:\n%s", out)
		fmt.Fprintf(&got, "stderr:\n%s", errOut)
		if err != nil {
			fmt.Fprintf(&got, "error: %v\n", err)
		}
		got.WriteString("\n")
	}
	text := strings.ReplaceAll(got.String(), home, "$HOME")
	text = goldenAccountID.ReplaceAllString(text, "acct_ID")
	for _, secret := range []string{cliTestAPIKey, cliTestToken} {
		if strings.Contains(text, secret) {
			t.Fatalf("output contains a secret:\n%s", text)
		}
	}

	path := filepath.Join("testdata", "account-add.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with core.autocrlf rewrites the golden with CRLF.
	if text != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("account add output differs from %s:\n%s", path, text)
	}
}

func writeGoldenFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
