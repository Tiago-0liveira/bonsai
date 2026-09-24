// Package config loads the .bonsai.yaml project config via viper and persists
// mutable state (file-copy frequency, aliases) to the user config directory.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// Alias is a user-defined command runnable against a worktree.
type Alias struct {
	Name    string `mapstructure:"name"`
	Command string `mapstructure:"command"`
}

// ResolveAlias finds an alias command by name, scanning config-file aliases
// first and then user (state) aliases; the last match wins, so user aliases
// shadow config-file aliases of the same name.
func ResolveAlias(name string, configAliases, stateAliases []Alias) (string, bool) {
	var command string
	found := false
	for _, a := range configAliases {
		if a.Name == name {
			command, found = a.Command, true
		}
	}
	for _, a := range stateAliases {
		if a.Name == name {
			command, found = a.Command, true
		}
	}
	return command, found
}

// Hooks holds shell commands fired on worktree lifecycle events.
type Hooks struct {
	OnWorktreeCreate []string `mapstructure:"on_worktree_create"`
	OnWorktreeDelete []string `mapstructure:"on_worktree_delete"`
}

// Worktree controls where new worktrees are created on disk.
type Worktree struct {
	// Root is the base directory new worktrees are created under. Empty defaults
	// to the parent of the main repo. Relative paths resolve against the repo.
	Root string `mapstructure:"root"`
	// PathTemplate names the worktree directory. Supports {repo} and {branch}
	// (branch slashes flattened to '-'). Empty defaults to "{repo}-{branch}".
	PathTemplate string `mapstructure:"path_template"`
}

// Theme selects and customizes the UI color palette.
type Theme struct {
	// Preset names a built-in palette (bonsai / sakura / dracula / nord / mono).
	// Empty defaults to bonsai.
	Preset string `mapstructure:"preset"`
	// Overrides maps a theme role (accent, danger, …) to a lipgloss color string.
	Overrides map[string]string `mapstructure:"overrides"`
}

// Notifications toggles desktop notifications per event.
type Notifications struct {
	Process bool `mapstructure:"process"`
	CI      bool `mapstructure:"ci"`
}

// ProcessPolicy sets the daemon's restart behavior for background processes
// whose label or command matches Match (exact or prefix). Restart is one of
// "no", "on-failure", "always".
type ProcessPolicy struct {
	Match       string `mapstructure:"match"`
	Restart     string `mapstructure:"restart"`
	MaxRestarts int    `mapstructure:"max_restarts"`
}

// Config is the parsed .bonsai.yaml.
type Config struct {
	// Upstream is the ref ahead/behind metrics compare against, e.g. origin/main.
	Upstream string `mapstructure:"upstream"`
	// Editor is the command run for the "open editor" action ("e"), e.g.
	// "code -w" or "nvim". Empty falls back to $VISUAL/$EDITOR, then vi. A
	// personal override in preferences (state.json) wins over this.
	Editor   string   `mapstructure:"editor"`
	Hooks    Hooks    `mapstructure:"hooks"`
	Aliases  []Alias  `mapstructure:"aliases"`
	Worktree Worktree `mapstructure:"worktree"`
	// ConfirmDestructive gates a yes/no prompt before merge/close/update/bulk-prune.
	ConfirmDestructive bool `mapstructure:"confirm_destructive"`
	// Keys maps an action name to an override key (see internal/ui keys.go).
	Keys map[string]string `mapstructure:"keys"`
	// Theme selects the color palette.
	Theme Theme `mapstructure:"theme"`
	// Notifications toggles desktop notifications.
	Notifications Notifications `mapstructure:"notifications"`
	// Processes sets per-process restart policies for the background daemon.
	Processes []ProcessPolicy `mapstructure:"processes"`
}

// PolicyFor resolves the restart policy for a process, matching label first then
// command against each configured entry (exact match or prefix). It returns
// procstore.DefaultPolicy when nothing matches.
func (c *Config) PolicyFor(label, command string) procstore.Policy {
	match := func(s, pat string) bool {
		return pat != "" && (s == pat || strings.HasPrefix(s, pat))
	}
	for _, key := range []string{label, command} {
		for _, p := range c.Processes {
			if !match(key, p.Match) {
				continue
			}
			mode := p.Restart
			if !procstore.ValidMode(mode) {
				mode = procstore.DefaultPolicy().Mode
			}
			max := p.MaxRestarts
			if max <= 0 {
				max = procstore.DefaultPolicy().MaxRestarts
			}
			return procstore.Policy{Mode: mode, MaxRestarts: max}
		}
	}
	return procstore.DefaultPolicy()
}

// WorktreePath resolves the filesystem path for a branch's worktree from the
// configured template and root. With defaults it matches git.WorktreePath: a
// sibling of the repo named "<repo>-<branch>".
func (c *Config) WorktreePath(repoDir, branch string) string {
	tmpl := c.Worktree.PathTemplate
	if tmpl == "" {
		tmpl = "{repo}-{branch}"
	}
	safe := strings.ReplaceAll(branch, "/", "-")
	name := strings.ReplaceAll(tmpl, "{repo}", filepath.Base(repoDir))
	name = strings.ReplaceAll(name, "{branch}", safe)

	if isAbsOrRooted(name) {
		return filepath.Clean(name)
	}

	root := c.Worktree.Root
	if root == "" {
		root = filepath.Dir(repoDir)
	} else if !isAbsOrRooted(root) {
		root = filepath.Join(repoDir, root)
	}
	return filepath.Join(root, name)
}

func isAbsOrRooted(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return false
}

// RemoteOf returns the remote portion of an upstream ref: "origin" for
// "origin/main". Falls back to "origin" when no remote is present.
func RemoteOf(upstream string) string {
	if i := strings.Index(upstream, "/"); i > 0 {
		return upstream[:i]
	}
	return "origin"
}

// Load reads .bonsai.yaml from dir. A missing file yields defaults, not an error.
func Load(dir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(".bonsai")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)
	v.SetDefault("upstream", "origin/main")
	v.SetDefault("confirm_destructive", true)
	v.SetDefault("notifications.process", true)
	v.SetDefault("notifications.ci", false)

	if err := v.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.Upstream == "" {
		cfg.Upstream = "origin/main"
	}
	return cfg, nil
}

// LoadFor loads the config for a repo anchored at mainRoot. If the worktree the
// process runs in has its own .bonsai.yaml, that file wins; otherwise the main
// worktree's is used (a missing file yields defaults, not an error).
func LoadFor(mainRoot string) (*Config, error) {
	dir := mainRoot
	if cwd, err := os.Getwd(); err == nil {
		if root, err := git.RepoRoot(cwd); err == nil {
			if _, statErr := os.Stat(filepath.Join(root, ".bonsai.yaml")); statErr == nil {
				dir = root
			}
		}
	}
	return Load(dir)
}

// LoadFile reads config from an explicit file path. Unlike Load, a missing or
// unreadable file is an error, since the user asked for it by name.
func LoadFile(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("upstream", "origin/main")
	v.SetDefault("confirm_destructive", true)
	v.SetDefault("notifications.process", true)
	v.SetDefault("notifications.ci", false)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Upstream == "" {
		cfg.Upstream = "origin/main"
	}
	return cfg, nil
}

// CreateHooks returns the commands to run after a worktree is created.
func (c *Config) CreateHooks() []string { return c.Hooks.OnWorktreeCreate }

// DeleteHooks returns the commands to run before a worktree is deleted.
func (c *Config) DeleteHooks() []string { return c.Hooks.OnWorktreeDelete }
