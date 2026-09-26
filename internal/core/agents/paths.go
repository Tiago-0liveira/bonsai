package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func DefaultAgentsDataRoot() (string, error) {
	var root string
	switch runtime.GOOS {
	case "windows":
		root = os.Getenv("LOCALAPPDATA")
		if root == "" {
			root = os.Getenv("APPDATA")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, "Library", "Application Support")
	default:
		root = os.Getenv("XDG_DATA_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, ".local", "share")
		}
	}
	if root == "" {
		return "", fmt.Errorf("unable to resolve user data directory")
	}
	return filepath.Join(root, "bonsai", "agents"), nil
}

func DefaultAgentsCacheRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", fmt.Errorf("unable to resolve user cache directory")
	}
	return filepath.Join(root, "bonsai", "agents"), nil
}

func DefaultSessionsRoot() (string, error) {
	root, err := DefaultAgentsCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "sessions"), nil
}

func DefaultUsageCacheRoot() (string, error) {
	root, err := DefaultAgentsCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "usage"), nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func isContained(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != ".." && rel != "." && !filepath.IsAbs(rel) && len(rel) > 0 &&
		rel[0] != '.' || pathAbs == rootAbs
}
