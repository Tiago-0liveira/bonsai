// Command bonsai is a git worktree manager TUI. main wires the core layer
// (config, state, git discovery) into the Bubble Tea presentation layer.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/cli"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui"
)

func main() {
	// Any argument selects a non-interactive subcommand; bare invocation runs
	// the TUI.
	if len(os.Args) > 1 {
		if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "bonsai:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bonsai:", err)
		os.Exit(1)
	}
}

func run() error {
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

	cfg, err := config.Load(repoDir)
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
