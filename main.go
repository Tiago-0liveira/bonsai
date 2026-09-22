// Command bonsai is a git worktree manager TUI. main wires the core layer
// (config, state, git discovery) into the Bubble Tea presentation layer.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/cli"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui"
)

func main() {
	args := os.Args[1:]

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
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
