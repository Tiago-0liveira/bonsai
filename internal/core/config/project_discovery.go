package config

import (
	"os"
	"path/filepath"
	"strings"
)

const ProjectDiscoveryDepth = 3

// SkipProjectDirectory is shared by discovery and daemon-side ownership so a
// linked worktree hidden behind a skipped directory cannot change placement.
func SkipProjectDirectory(name string) bool {
	switch name {
	case ".git", ".bonsai", "node_modules", "vendor", "dist", "build", "target", ".cache", ".venv", "venv", "__pycache__", ".next", ".turbo":
		return true
	default:
		return false
	}
}

// CanDiscoverDirectory checks whether the bounded scanner can reach this known
// worktree path. It does not enumerate the directory or invoke Git.
func CanDiscoverDirectory(root, path string) bool {
	if !ContainsPath(root, path) {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		info, err := os.Stat(path)
		return err == nil && info.IsDir()
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > ProjectDiscoveryDepth {
		return false
	}
	parent := root
	for _, part := range parts {
		if SkipProjectDirectory(part) {
			return false
		}
		if _, err := os.Lstat(filepath.Join(parent, ".git")); err == nil {
			return false
		}
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
