// Package cli implements bonsai's non-interactive subcommands. When invoked with
// no arguments the caller launches the TUI instead; any argument routes here.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/updater"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

// Run dispatches a subcommand. args excludes the program name. out/errOut are the
// destination streams (stdout/stderr in production).
func Run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("no subcommand")
	}

	// Global commands must work outside a Git repository.
	switch args[0] {
	case "version", "-v", "--version":
		if len(args) != 1 {
			return fmt.Errorf("usage: bonsai version")
		}
		fmt.Fprintln(out, "bonsai", version.String())
		return nil
	case "update":
		return cmdUpdate(args[1:], out, errOut)
	case "help", "-h", "--help":
		printUsage(out)
		return nil
	case "shell-init":
		return cmdShellInit(out)
	}

	// Anchor to the main worktree regardless of the current directory, so copy
	// sources and worktree paths resolve consistently.
	repoDir, err := git.MainRoot(".")
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	switch args[0] {
	case "list", "ls":
		return cmdList(repoDir, out)
	case "create", "new":
		return cmdCreate(repoDir, args[1:], out, errOut)
	case "copy", "cp":
		return cmdCopy(repoDir, args[1:], out)
	case "path", "cd":
		return cmdPath(repoDir, args[1:], out)
	case "x", "run":
		return cmdX(repoDir, args[1:], out, errOut)
	case "alias", "aliases":
		return cmdAlias(repoDir, args[1:], out, errOut)
	default:
		printUsage(errOut)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// cmdList prints every worktree as "branch<TAB>path".
func cmdList(repoDir string, out io.Writer) error {
	trees, err := git.ListWorktrees(repoDir)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	for _, t := range trees {
		branch := t.Branch
		if t.IsMain {
			branch += " (main)"
		}
		fmt.Fprintf(tw, "%s\t%s\n", branch, t.Path)
	}
	return tw.Flush()
}

// cmdCreate creates a worktree from a new branch, an existing branch, or a PR,
// runs create hooks, and prints the resulting path.
func cmdCreate(repoDir string, args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(errOut)
	existing := fs.Bool("existing", false, "check out an existing branch")
	prNum := fs.Int("pr", 0, "create from a GitHub pull request number")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFor(repoDir)
	if err != nil {
		return err
	}

	var (
		path   string
		branch string
	)
	switch {
	case *prNum > 0:
		path = cfg.WorktreePath(repoDir, fmt.Sprintf("pr-%d", *prNum))
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			branch, err = git.CreateWorktreeFromPR(repoDir, path, *prNum)
		}
	case *existing:
		branch = fs.Arg(0)
		if branch == "" {
			return fmt.Errorf("create --existing requires a branch name")
		}
		path = cfg.WorktreePath(repoDir, branch)
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = git.AddWorktreeExisting(repoDir, path, branch)
		}
	default:
		branch = fs.Arg(0)
		if branch == "" {
			return fmt.Errorf("create requires a branch name")
		}
		path = cfg.WorktreePath(repoDir, branch)
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = git.AddWorktreeNewBranch(repoDir, path, branch)
		}
	}
	if err != nil {
		return err
	}

	vars := config.HookVars(repoDir, path, branch, cfg.Upstream, *prNum)
	if err := coreexec.RunHooks(path, cfg.CreateHooks(), vars); err != nil {
		return fmt.Errorf("create hook: %w", err)
	}

	fmt.Fprintln(out, path)
	return nil
}

// cmdCopy copies a main-repo file into a target worktree, recording the copy.
func cmdCopy(repoDir string, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: bonsai copy <relpath> <worktree>")
	}
	rel, target := args[0], args[1]

	wtPath, err := resolveWorktree(repoDir, target)
	if err != nil {
		return err
	}

	src := filepath.Join(repoDir, rel)
	dst := filepath.Join(wtPath, rel)
	if err := fs.Copy(src, dst); err != nil {
		return err
	}

	if st, err := config.LoadState(); err == nil {
		_ = st.RecordCopy(rel)
	}

	fmt.Fprintf(out, "copied %s -> %s\n", src, dst)
	return nil
}

// cmdPath prints the filesystem path of a worktree so a shell wrapper can cd to
// it. The name "main" (or "-") resolves to the main worktree.
func cmdPath(repoDir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bonsai path <branch|main>")
	}
	wtPath, err := resolveWorktree(repoDir, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(out, wtPath)
	return nil
}

// cmdX runs a named alias. Usage:
//
//	bonsai x <alias> [worktree]   run alias in worktree (default: current dir)
//	bonsai x --list               list available aliases
//
// The alias command inherits the terminal's stdio and its exit code propagates.
func cmdX(repoDir string, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bonsai x <alias> [worktree]  (or: bonsai x --list)")
	}
	switch args[0] {
	case "--list", "-l", "list":
		return printAliases(repoDir, out)
	}

	name := args[0]
	command, ok := resolveAlias(repoDir, name)
	if !ok {
		return fmt.Errorf("no alias named %q (see: bonsai x --list)", name)
	}

	// Default to the current directory; an optional second arg names a worktree.
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if len(args) > 1 {
		dir, err = resolveWorktree(repoDir, args[1])
		if err != nil {
			return err
		}
	}

	// Expand {vars} in the alias command using the target worktree's context.
	upstream := "origin/main"
	if cfg, err := config.LoadFor(repoDir); err == nil {
		upstream = cfg.Upstream
	}
	vars := config.HookVars(repoDir, dir, branchForPath(repoDir, dir), upstream, 0)
	command = coreexec.ExpandVars(command, vars)

	cmd := coreexec.Command(dir, command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, errOut
	return cmd.Run()
}

// pathEqual compares two filesystem paths, case-insensitively on Windows.
func pathEqual(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// branchForPath returns the branch checked out in the worktree at path, or "".
func branchForPath(repoDir, path string) string {
	trees, err := git.ListWorktrees(repoDir)
	if err != nil {
		return ""
	}
	for _, t := range trees {
		if pathEqual(t.Path, path) {
			return t.Branch
		}
	}
	return ""
}

// cmdAlias manages persisted aliases. Usage:
//
//	bonsai alias list
//	bonsai alias add <name> <command...>
//	bonsai alias rm <name>
func cmdAlias(repoDir string, args []string, out, errOut io.Writer) error {
	action := "list"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "list", "ls":
		return printAliases(repoDir, out)

	case "add", "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: bonsai alias add <name> <command...>")
		}
		name := args[1]
		command := strings.Join(args[2:], " ")
		st, err := config.LoadState()
		if err != nil {
			return err
		}
		if err := st.AddAlias(config.Alias{Name: name, Command: command}); err != nil {
			return err
		}
		fmt.Fprintf(out, "added alias %q -> %s\n", name, command)
		return nil

	case "rm", "remove", "delete":
		if len(args) != 2 {
			return fmt.Errorf("usage: bonsai alias rm <name>")
		}
		st, err := config.LoadState()
		if err != nil {
			return err
		}
		removed, err := st.RemoveAlias(args[1])
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("no user alias named %q (config-file aliases are read-only)", args[1])
		}
		fmt.Fprintf(out, "removed alias %q\n", args[1])
		return nil

	default:
		return fmt.Errorf("unknown alias action %q (list|add|rm)", action)
	}
}

// mergedAliases returns config-file aliases followed by user (state) aliases.
func mergedAliases(repoDir string) []config.Alias {
	var all []config.Alias
	if cfg, err := config.LoadFor(repoDir); err == nil {
		all = append(all, cfg.Aliases...)
	}
	if st, err := config.LoadState(); err == nil {
		all = append(all, st.SortedAliases()...)
	}
	return all
}

// resolveAlias finds an alias command by name (user aliases shadow config ones).
func resolveAlias(repoDir, name string) (string, bool) {
	var configAliases []config.Alias
	if cfg, err := config.LoadFor(repoDir); err == nil {
		configAliases = cfg.Aliases
	}
	var stateAliases []config.Alias
	if st, err := config.LoadState(); err == nil {
		stateAliases = st.SortedAliases()
	}
	return config.ResolveAlias(name, configAliases, stateAliases)
}

// printAliases writes every alias as "name<TAB>command".
func printAliases(repoDir string, out io.Writer) error {
	aliases := mergedAliases(repoDir)
	if len(aliases) == 0 {
		fmt.Fprintln(out, "(no aliases defined)")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	for _, a := range aliases {
		fmt.Fprintf(tw, "%s\t%s\n", a.Name, a.Command)
	}
	return tw.Flush()
}

// cmdShellInit prints shell integration helpers enabling `bcd <branch>` to change dirs.
func cmdShellInit(out io.Writer) error {
	fmt.Fprint(out, `# bonsai shell integration
#
# Bash / Zsh — add to ~/.zshrc or ~/.bashrc:
#   eval "$(bonsai shell-init)"
bcd() { cd "$(bonsai path "${1:-main}")" || return; }

# PowerShell — add to $PROFILE:
#   function bcd { param($target = "main") $p = (bonsai path $target); if ($LASTEXITCODE -eq 0 -and $p) { Set-Location $p } }

# Windows CMD — create bcd.bat on your PATH:
#   @echo off
#   for /f "delims=" %%i in ('bonsai path %*') do cd /d "%%i"
`)
	return nil
}

// resolveWorktree maps a user-supplied name to a worktree path. It accepts
// "main"/"-" for the main worktree, an exact branch name, or an exact path.
func resolveWorktree(repoDir, name string) (string, error) {
	trees, err := git.ListWorktrees(repoDir)
	if err != nil {
		return "", err
	}
	if name == "main" || name == "-" {
		for _, t := range trees {
			if t.IsMain {
				return t.Path, nil
			}
		}
	}
	for _, t := range trees {
		if t.Branch == name || pathEqual(t.Path, name) || (runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(t.Path), name)) || filepath.Base(t.Path) == name {
			return t.Path, nil
		}
	}
	return "", fmt.Errorf("no worktree matching %q", name)
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, strings.TrimLeft(`
bonsai — git worktree manager

Usage:
  bonsai                          launch the interactive TUI
  bonsai --config <file>          launch the TUI with a specific .bonsai.yaml
  bonsai list                     list worktrees (branch, path)
  bonsai create <branch>          create a worktree on a new branch
  bonsai create --existing <br>   create a worktree on an existing branch
  bonsai create --pr <number>     create a worktree from a GitHub PR
  bonsai copy <file> <worktree>   copy a main-repo file into a worktree
  bonsai path <branch|main>       print a worktree path (for cd)
  bonsai x <alias> [worktree]     run an alias (default: current dir)
  bonsai x --list                 list available aliases
  bonsai alias list               list aliases
  bonsai alias add <name> <cmd…>  add a user alias
  bonsai alias rm <name>          remove a user alias
  bonsai shell-init               print a shell 'bcd' cd helper
  bonsai version                  print the installed version
  bonsai update [--check]          install or check the latest release
  bonsai help                     show this help
`, "\n"))
}

func cmdUpdate(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(errOut)
	check := fs.Bool("check", false, "check without installing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: bonsai update [--check]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := updater.New()
	r, err := c.Latest(ctx)
	if err != nil {
		return err
	}
	if version.Version == "dev" || !updater.Newer(r.Tag, version.String()) {
		if version.Version == "dev" {
			fmt.Fprintf(out, "Current: dev; latest: %s. Install a release binary to enable updates.\n", r.Tag)
		} else {
			fmt.Fprintf(out, "bonsai %s is up to date.\n", version.String())
		}
		return nil
	}
	fmt.Fprintf(out, "Bonsai %s is available. Current: %s\n", r.Tag, version.String())
	if *check {
		return nil
	}
	if err = c.Install(ctx, r); err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated to %s. Restart Bonsai to use it.\n", r.Tag)
	return nil
}
