//go:build linux || darwin

package localapi

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentsetup"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/server/webui"
)

// TestAgentsTabBrowserFixture serves the Agents tab over the production agent
// runtime with no profiles: the browser creates them through real setup jobs
// against the fake codex and claude binaries. HOME and the XDG roots are temp
// dirs, so nothing reads or writes the real ~/.codex, ~/.claude or profiles.
func TestAgentsTabBrowserFixture(t *testing.T) {
	if os.Getenv("BONSAI_AGENTS_TAB_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, name := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "WSL_DISTRO_NAME", "SSH_CONNECTION"} {
		unsetEnv(t, name)
	}
	bin := t.TempDir()
	// agy is not installed, so its provider lists as unavailable.
	codexTestdata, err := filepath.Abs(filepath.Join("..", "..", "providers", "codex", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	claudeTestdata, err := filepath.Abs(filepath.Join("..", "..", "providers", "claude", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"codex": filepath.Join(codexTestdata, "fakecodex.sh"), "claude": filepath.Join(claudeTestdata, "fakeclaude.sh")} {
		if err := os.Symlink(script, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv("FAKE_CODEX_FIXTURES", filepath.Join(codexTestdata, "phase0"))
	t.Setenv("FAKE_CODEX_DEVICE_CODE", "BONSAI-E2E1")
	// Interactive logins wait for a line typed into the job terminal.
	t.Setenv("FAKE_CODEX_LOGIN_PROMPT", "1")
	t.Setenv("FAKE_CODEX_EMAIL", "fullstack@example.com")
	t.Setenv("FAKE_CLAUDE_FIXTURES", claudeTestdata)
	// Model and usage lookups go to a closed local port: never the network.
	claude.ModelsEndpoint, claude.UsageEndpoint = "http://127.0.0.1:1/v1/models", "http://127.0.0.1:1/usage"

	s, _, _ := terminalTestServer(t)
	s.agents.manager.Close(false, 10*time.Second)
	runtime, err := agentruntime.New(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.agents = &agentAPI{runtime: runtime, registry: s.registry}
	s.agents.manager = testAgentManager(t, testAgentHostPaths(t), runtime.Accounts, runtime.Sessions, runtime.Registry, s.publishAgents)
	s.agents.setup = agentsetup.New(runtime.AccountService, s.publishAgentsChanged)
	t.Cleanup(s.Close)
	s.publishAgents("repo")
	s.expectedHost = "127.0.0.1:7012"
	s.browserOrigin = ""
	s.securityMode = BrowserSecurityProduction
	s.ui = webui.New(os.DirFS(filepath.Join("..", "..", "..", "web", "dist-e2e")), webui.Options{})
	// A configured root keeps the first-run folder picker out of the way.
	if err := os.WriteFile(s.rootsPath, []byte(`{"version":1,"revision":1,"roots":[{"id":"fixture","path":"/tmp"}]}`), 0600); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", s.expectedHost)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go s.stateSync.Run(ctx)
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	if err = server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

// unsetEnv clears a variable for the test and restores it afterwards.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}
