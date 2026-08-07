// Package config loads the .bonsai.yaml project config via viper and persists
// mutable state (file-copy frequency, aliases) to the user config directory.
package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Alias is a user-defined command runnable against a worktree.
type Alias struct {
	Name    string `mapstructure:"name"`
	Command string `mapstructure:"command"`
}

// Hooks holds shell commands fired on worktree lifecycle events.
type Hooks struct {
	OnWorktreeCreate []string `mapstructure:"on_worktree_create"`
	OnWorktreeDelete []string `mapstructure:"on_worktree_delete"`
}

// Config is the parsed .bonsai.yaml.
type Config struct {
	// Upstream is the ref ahead/behind metrics compare against, e.g. origin/main.
	Upstream string  `mapstructure:"upstream"`
	Hooks    Hooks   `mapstructure:"hooks"`
	Aliases  []Alias `mapstructure:"aliases"`
}

// Load reads .bonsai.yaml from dir. A missing file yields defaults, not an error.
func Load(dir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(".bonsai")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)
	v.SetDefault("upstream", "origin/main")

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

// CreateHooks returns the commands to run after a worktree is created.
func (c *Config) CreateHooks() []string { return c.Hooks.OnWorktreeCreate }

// DeleteHooks returns the commands to run before a worktree is deleted.
func (c *Config) DeleteHooks() []string { return c.Hooks.OnWorktreeDelete }
