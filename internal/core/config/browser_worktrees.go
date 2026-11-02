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
		resolved, err := resolveWorktreeBase(ctx, settings, main)
		if err != nil {
			return err
		}
		root, err := prepareBrowserDirectory(settings.Roots, resolved.root)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return create(root)
	})
}

// worktreeBase is where a repository's worktrees go before any directory exists.
type worktreeBase struct {
	root  string // as configured, not yet checked or created
	owner ProjectRoot
	cfg   *Config
	trees []coregit.Worktree
}

// resolveWorktreeBase finds the project root that owns main and the configured
// worktree root. Creating and previewing placements share it so they cannot
// disagree about ownership.
func resolveWorktreeBase(ctx context.Context, settings ProjectRoots, main string) (worktreeBase, error) {
	trees, err := coregit.ListWorktreesContext(ctx, main)
	if err != nil {
		return worktreeBase{}, err
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
		return worktreeBase{}, fmt.Errorf("repository has no configured project root; add its directory in Settings")
	}
	root := filepath.Join(owner.Path, ".bonsai", "worktrees", ProjectID(main))
	cfg, err := Load(main)
	if err != nil {
		return worktreeBase{}, err
	}
	if cfg.Worktree.Root != "" {
		root = cfg.Worktree.Root
		if !filepath.IsAbs(root) {
			root = filepath.Join(main, root)
		}
	}
	return worktreeBase{root: root, owner: owner, cfg: cfg, trees: trees}, nil
}

// Check existing ancestors before mkdir, then check the resolved destination.
// Never follow an existing link outside a configured root to create directories.
func prepareBrowserDirectory(roots []ProjectRoot, path string) (string, error) {
	owner, destination, err := checkBrowserDirectory(roots, path)
	if err != nil {
		return "", err
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

// checkBrowserDirectory resolves where path would land through its existing
// ancestors and checks that a configured root owns it, creating nothing.
func checkBrowserDirectory(roots []ProjectRoot, path string) (ProjectRoot, string, error) {
	path = filepath.Clean(path)
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return ProjectRoot{}, "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return ProjectRoot{}, "", err
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return ProjectRoot{}, "", err
	}
	suffix, err := filepath.Rel(ancestor, path)
	if err != nil {
		return ProjectRoot{}, "", err
	}
	destination := filepath.Join(resolved, suffix)
	owner, ok := OwningRoot(roots, destination)
	if !ok {
		return ProjectRoot{}, "", fmt.Errorf("worktree.root must resolve within a configured project root; update .bonsai.yaml or add that directory in Settings")
	}
	canonical, err := CanonicalDirectory(owner.Path)
	if err != nil {
		return ProjectRoot{}, "", fmt.Errorf("project root unavailable: %w", err)
	}
	if canonical != owner.Path {
		return ProjectRoot{}, "", fmt.Errorf("project root now resolves to a different directory; remove and add it again in Settings")
	}
	return owner, destination, nil
}
