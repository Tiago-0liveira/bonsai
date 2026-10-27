// Command bonsai is a git worktree manager TUI. main wires the core layer
// (config, state, git discovery) into the Bubble Tea presentation layer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/agentsbackend"
	"github.com/Tiago-0liveira/bonsai/internal/cli"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/shellinit"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/server"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/server/localapi"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	"github.com/Tiago-0liveira/bonsai/internal/ui"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

func main() {
	args := os.Args[1:]

	// Hidden: `bonsai __daemon --repo <root>` runs the per-repo background daemon,
	// `bonsai __daemon --home <dir>` the user-level one behind `bonsai web`.
	// Clients auto-start it detached; users never invoke it directly.
	if len(args) >= 1 && args[0] == "__daemon" {
		root, home := "", ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--repo" && i+1 < len(args) {
				root = args[i+1]
			}
			if args[i] == "--home" && i+1 < len(args) {
				home = args[i+1]
			}
		}
		if (root == "") == (home == "") {
			fmt.Fprintln(os.Stderr, "bonsai: __daemon requires exactly one of --repo <root> or --home <dir>")
			os.Exit(1)
		}
		serve := server.Serve
		if home != "" {
			root, serve = home, server.ServeUserHome
		}
		if err := serve(root); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai daemon:", err)
			os.Exit(1)
		}
		return
	}

	// Hidden: the daemon supervises the loopback-only local HTTP API.
	if len(args) >= 1 && args[0] == "__serve-api" {
		if err := runServeInternal(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai:", err)
			os.Exit(1)
		}
		return
	}

	// Hidden: `bonsai __agent-host` holds one agent session (its PTY, terminal
	// stream and lifecycle) detached from the API that launched it, so the
	// session survives API restarts and `bonsai web stop`.
	if len(args) >= 1 && args[0] == "__agent-host" {
		if err := runAgentHost(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai agent host:", err)
			os.Exit(1)
		}
		return
	}

	// Hidden: `bonsai __agent-hook <provider> <event>` is what an agent CLI runs
	// for each lifecycle hook. It must stay fast and never fail, so it comes
	// before any config or flag handling.
	if len(args) >= 1 && args[0] == "__agent-hook" {
		os.Exit(agentruntime.RunHook(args[1:], os.Stdin, os.Stdout))
	}

	// Hidden: in-process cold sync benchmark.
	if len(args) >= 1 && args[0] == "__sync-bench" {
		if err := runSyncBench(args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai:", err)
			os.Exit(1)
		}
		return
	}

	// Hidden: development-only verify/normalize/SSE webhook relay.
	if len(args) >= 1 && args[0] == "__serve-webhook" {
		if err := runDevWebhookInternal(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai:", err)
			os.Exit(1)
		}
		return
	}

	// Flags like --version / -v and --update / -u are checked early
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version", "--update", "-u", "update":
			if err := cli.Run(args, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "bonsai:", err)
				os.Exit(1)
			}
			return
		}
	}

	// --config <file> (or --config=<file>) overrides config discovery for the
	// TUI. It must come before any subcommand.
	var cfgPath string
	switch {
	case len(args) >= 2 && (args[0] == "--config" || args[0] == "-c"):
		cfgPath, args = args[1], args[2:]
	case len(args) >= 1 && strings.HasPrefix(args[0], "--config="):
		cfgPath, args = strings.TrimPrefix(args[0], "--config="), args[1:]
	}
	if cfgPath != "" {
		if strings.HasPrefix(cfgPath, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				if cfgPath == "~" {
					cfgPath = home
				} else if strings.HasPrefix(cfgPath, "~/") || strings.HasPrefix(cfgPath, "~\\") {
					cfgPath = filepath.Join(home, cfgPath[2:])
				}
			}
		}
		if abs, err := filepath.Abs(cfgPath); err == nil {
			cfgPath = filepath.Clean(abs)
		} else {
			cfgPath = filepath.Clean(cfgPath)
		}
	}

	// Any remaining argument selects a non-interactive subcommand; bare
	// invocation runs the TUI.
	if len(args) > 0 {
		if cfgPath != "" {
			fmt.Fprintln(os.Stderr, "bonsai: --config applies to the TUI only, not subcommands")
			os.Exit(1)
		}
		if err := cli.Run(args, os.Stdout, os.Stderr); err != nil {
			exitWith(err)
		}
		return
	}

	if err := run(cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "bonsai:", err)
		os.Exit(1)
	}
}

// exitWith reports err and exits non-zero. A cli.ExitError has already told
// the user what went wrong, so only its exit code is used.
func exitWith(err error) {
	var exit *cli.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.Code)
	}
	fmt.Fprintln(os.Stderr, "bonsai:", err)
	os.Exit(1)
}

func run(cfgPath string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Anchor to the main worktree even when launched from inside a linked one,
	// so file copies always source from main.
	repoDir, err := git.MainRoot(cwd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	var cfg *config.Config
	if cfgPath != "" {
		cfg, err = config.LoadFile(cfgPath)
	} else {
		cfg, err = config.LoadFor(repoDir)
	}
	if err != nil {
		return err
	}

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	// The TUI always starts: without a backend it just has no Agents tab.
	var agentsAPI *agentsapi.Backend
	backend, backendErr := agentsbackend.New()
	if backendErr == nil {
		// Closing only ends the TUI's attachments; holders keep running.
		defer backend.Close()
		api := backend.API()
		agentsAPI = &api
	}
	model := newTUIModel(repoDir, cfg, state, agentsAPI)
	// Here, not in newTUIModel, so tests do not write shims to the real cache dir.
	go coreexec.PrepareShellIntegration()
	// Mouse tracking is on so the wheel scrolls the pane under the pointer.
	// Without it, terminals translate the wheel into arrow keys, which the panes
	// read as "move the selection". The cost is that dragging to select text now
	// needs shift held (or the mouse turned off from the command palette).
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	if backendErr != nil {
		// After the TUI has left the alternate screen, so the line stays visible.
		fmt.Fprintln(os.Stderr, "agents unavailable:", backendErr)
	}
	return err
}

// newTUIModel builds the TUI, with agent support when a backend is given.
func newTUIModel(repoDir string, cfg *config.Config, state *config.State, agents *agentsapi.Backend) ui.Model {
	opts := []ui.Option{ui.WithDepth(shellinit.Depth())}
	if agents != nil {
		opts = append(opts, ui.WithAgents(*agents))
	}
	return ui.New(repoDir, cfg, state, opts...)
}

func runServeInternal(args []string) error {
	fs := flag.NewFlagSet("__serve-api", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoDir := fs.String("repo", "", "launch repository root (omit for the user-level API that serves every configured project)")
	port := fs.Int("port", 0, "loopback listen port")
	browserOrigin := fs.String("browser-origin", "", "additional loopback browser origin (development only)")
	securityMode := fs.String("security-mode", string(localapi.BrowserSecurityProduction), "browser security mode")
	webhookPort := fs.Int("webhook-port", 0, "loopback port of the live-updates webhook receiver (user-level API only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port <= 0 {
		return fmt.Errorf("__serve-api requires --port")
	}
	cfg := localapi.Config{
		RepoDir:       *repoDir,
		Address:       fmt.Sprintf("127.0.0.1:%d", *port),
		BrowserOrigin: *browserOrigin,
		SecurityMode:  localapi.BrowserSecurityMode(*securityMode),
	}
	if *webhookPort > 0 {
		// The secret comes from its 0600 file, never from argv.
		path, err := config.WebWebhookSecretPath()
		if err != nil {
			return err
		}
		secret, err := config.ReadWebWebhookSecret(path)
		if err != nil {
			return fmt.Errorf("live updates webhook secret: %w", err)
		}
		cfg.WebhookAddress = fmt.Sprintf("127.0.0.1:%d", *webhookPort)
		cfg.WebhookSecret = secret
		// The live controller keeps the hooks of web.json's live
		// repositories pointed at the tunnel, and records them in
		// web-state.json.
		if cfg.WebConfigPath, err = config.WebConfigPath(); err != nil {
			return err
		}
		if cfg.WebStatePath, err = config.WebStatePath(); err != nil {
			return err
		}
		if cfg.WebHome, err = config.WebHome(); err != nil {
			return err
		}
	}
	return localapi.Run(cfg)
}

// runAgentHost serves the session whose spec arrives on stdin. SIGTERM stops
// the session; the holder exits once it has ended.
func runAgentHost(args []string) error {
	fs := flag.NewFlagSet("__agent-host", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	records := fs.String("records", "", "agent host record directory")
	sockets := fs.String("sockets", "", "agent host socket directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *records == "" || *sockets == "" {
		return fmt.Errorf("__agent-host requires --records and --sockets")
	}
	spec, err := agenthost.ReadSpec(os.Stdin)
	if err != nil {
		agenthost.Ready(fmt.Errorf("read agent spec: %w", err))
		return err
	}
	runtime, err := agentruntime.New(nil, nil, nil)
	if err != nil {
		agenthost.Ready(err)
		return err
	}
	signal.Ignore(syscall.SIGHUP, syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	paths := agenthost.Paths{Records: *records, Sockets: *sockets}
	return agenthost.Serve(ctx, spec, agenthost.Deps{Accounts: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry, Paths: paths, Notifications: notifyPrefs()}, agenthost.Ready)
}

// notifyPrefs reads the agent notification preference from web.json once. Any
// failure (no config dir, unreadable or invalid file) means no notifications:
// a session never fails for them.
func notifyPrefs() agenthost.NotifyPrefs {
	path, err := config.WebConfigPath()
	if err != nil {
		return agenthost.NotifyPrefs{}
	}
	cfg, _, err := config.ReadWebConfig(path)
	if err != nil {
		return agenthost.NotifyPrefs{}
	}
	n := cfg.Agents.Notifications
	return agenthost.NotifyPrefs{Enabled: n.Enabled, NeedsInput: n.NeedsInput, TurnComplete: n.TurnComplete}
}

func runDevWebhookInternal(args []string) error {
	fs := flag.NewFlagSet("__serve-webhook", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "loopback listen port")
	browserOrigin := fs.String("browser-origin", "", "authorized development browser origin")
	secretFile := fs.String("secret-file", "", "development webhook HMAC secret file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port <= 0 || *browserOrigin == "" || *secretFile == "" {
		return fmt.Errorf("__serve-webhook requires --port, --browser-origin, and --secret-file")
	}
	return webhooks.RunDevRelay(webhooks.DevRelayConfig{
		Address:       fmt.Sprintf("127.0.0.1:%d", *port),
		BrowserOrigin: *browserOrigin,
		SecretFile:    *secretFile,
	})
}

func runSyncBench(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("__sync-bench", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	projects := fs.Int("projects", 0, "limit to the first N projects (0 = all)")
	root := fs.String("root", "", "benchmark every repository under this directory instead of the configured project roots")
	repo := fs.String("repo", "", "only benchmark the project at this main worktree path")
	// Sync state is memory-only, so every run is cold. The flag is accepted so
	// scripts written now keep working once warm-start persistence lands.
	_ = fs.Bool("cold", false, "start from empty caches (always true today)")
	timeout := fs.Duration("timeout", 2*time.Minute, "give up after this long")
	warm := fs.Bool("warm", false, "after the cold pass, force one provider refresh per project and report its GitHub requests")
	noPrewarm := fs.Bool("no-prewarm", false, "skip the GitHub token read and connection the API server prepares at startup (measures a cold process)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := localapi.SyncBench(ctx, localapi.SyncBenchOptions{Root: *root, Repo: *repo, Projects: *projects, Timeout: *timeout, Warm: *warm, NoPrewarm: *noPrewarm})
	if err != nil {
		return err
	}
	ms := func(d time.Duration) string {
		if d == 0 {
			return "-"
		}
		return fmt.Sprintf("%.0fms", float64(d.Microseconds())/1000)
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tWORKTREES\tINVENTORY\tLOCAL READY\tPR CATALOG\tALL CHECKS\tNOTE")
	for _, row := range append(result.Rows, result.Total) {
		note := row.ProviderErr
		if row.Incomplete {
			note = "timed out"
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\n", row.Name, row.Worktrees, ms(row.Inventory), ms(row.LocalReady), ms(row.PRCatalog), ms(row.AllChecks), note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "\nspawns: git=%d gh=%d (whole pass)\ngithub http: requests=%d not_modified=%d\n", result.GitSpawns, result.GHSpawns, result.HTTPRequests, result.NotModified); err != nil {
		return err
	}
	if warm := result.Warm; warm != nil {
		if _, err := fmt.Fprintf(out, "warm refresh: %s gh=%d requests=%d not_modified=%d\n", ms(warm.Duration), warm.GHSpawns, warm.HTTPRequests, warm.NotModified); err != nil {
			return err
		}
	}
	return nil
}
