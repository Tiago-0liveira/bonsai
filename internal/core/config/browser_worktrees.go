package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
)

// WithBrowserWorktreeRoot resolves placement for each creation. Holding the root
// configuration lock makes removal and the start of creation serializable.
func WithBrowserWorktreeRoot(ctx context.Context, settingsPath, main string, create func(string) error) error {
	canonical, err := CanonicalDirectory(main)
	if err != nil {
		return err
	}
	main = canonical
	return WithProjectRootsContext(ctx, settingsPath, func(settings ProjectRoots) error {
		trees, err := coregit.ListWorktreesContext(ctx, main)
		if err != nil {
			return err
		}
		paths := []string{}
		for _, tree := range trees {
			for _, root := range settings.Roots {
				if CanDiscoverDirectory(root.Path, tree.Path) {
					paths = append(paths, tree.Path)
					break
				}
			}
		}
		owner, ok := OwningRoot(settings.Roots, main, paths...)
		if !ok {
			return fmt.Errorf("repository has no configured project root; add its directory in Settings")
		}
		root := filepath.Join(owner.Path, ".bonsai", "worktrees", PathID("project", main))
		cfg, err := Load(main)
		if err != nil {
			return err
		}
		if cfg.Worktree.Root != "" {
			root = cfg.Worktree.Root
			if !filepath.IsAbs(root) {
				root = filepath.Join(main, root)
			}
		}
		root, err = prepareBrowserDirectory(settings.Roots, root)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return create(root)
	})
}

// Check existing ancestors before mkdir, then check the resolved destination.
// Never follow an existing link outside a configured root to create directories.
func prepareBrowserDirectory(roots []ProjectRoot, path string) (string, error) {
	path = filepath.Clean(path)
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	suffix, err := filepath.Rel(ancestor, path)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(resolved, suffix)
	owner, ok := OwningRoot(roots, destination)
	if !ok {
		return "", fmt.Errorf("worktree.root must resolve within a configured project root; update .bonsai.yaml or add that directory in Settings")
	}
	canonical, err := CanonicalDirectory(owner.Path)
	if err != nil {
		return "", fmt.Errorf("project root unavailable: %w", err)
	}
	if canonical != owner.Path {
		return "", fmt.Errorf("project root now resolves to a different directory; remove and add it again in Settings")
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return "", err
	}
	final, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return "", err
	}
	if !ContainsPath(owner.Path, final) {
		return "", fmt.Errorf("worktree destination escaped its configured project root")
	}
	return final, nil
}
