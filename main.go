// Command bonsai is a git worktree manager TUI. main wires the core layer
// (config, state, git discovery) into the Bubble Tea presentation layer.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/cli"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/server"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	serverruntime "github.com/Tiago-0liveira/bonsai/internal/server/runtime"
	"github.com/Tiago-0liveira/bonsai/internal/ui"
)

func main() {
	args := os.Args[1:]

	// Hidden: `bonsai __daemon --repo <root>` runs the per-repo background daemon.
	// Clients auto-start it detached; users never invoke it directly.
	if len(args) >= 1 && args[0] == "__daemon" {
		root := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--repo" && i+1 < len(args) {
				root = args[i+1]
			}
		}
		if root == "" {
			fmt.Fprintln(os.Stderr, "bonsai: __daemon requires --repo <root>")
			os.Exit(1)
		}
		if err := server.Serve(root); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai daemon:", err)
			os.Exit(1)
		}
		return
	}

	// Hidden: the daemon supervises these split local HTTP services.
	if len(args) >= 1 && (args[0] == "__serve-api" || args[0] == "__serve-webhook") {
		if err := runServeInternal(args[0], args[1:]); err != nil {
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
			fmt.Fprintln(os.Stderr, "bonsai:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "bonsai:", err)
		os.Exit(1)
	}
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

	model := ui.New(repoDir, cfg, state)
	// Mouse tracking is on so the wheel scrolls the pane under the pointer.
	// Without it, terminals translate the wheel into arrow keys, which the panes
	// read as "move the selection". The cost is that dragging to select text now
	// needs shift held (or the mouse turned off from the command palette).
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func runServeInternal(kind string, args []string) error {
	fs := flag.NewFlagSet(kind, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "server config path")
	port := fs.Int("port", 0, "loopback listen port")
	apiPort := fs.Int("api-port", 0, "main API port")
	browserOrigin := fs.String("browser-origin", "", "local browser origin")
	sessionSecretFile := fs.String("session-secret-file", "", "serve session secret file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *port <= 0 {
		return fmt.Errorf("%s requires --config and --port", kind)
	}
	address := fmt.Sprintf("127.0.0.1:%d", *port)
	switch kind {
	case "__serve-api":
		if *browserOrigin == "" {
			return fmt.Errorf("__serve-api requires --browser-origin")
		}
		return serverruntime.RunAPI(*configPath, address, *browserOrigin, *sessionSecretFile)
	case "__serve-webhook":
		if *apiPort <= 0 {
			return fmt.Errorf("__serve-webhook requires --api-port")
		}
		return serverruntime.RunWebhook(*configPath, address, fmt.Sprintf("127.0.0.1:%d", *apiPort), *sessionSecretFile)
	default:
		return fmt.Errorf("unknown internal serve command %q", kind)
	}
}
