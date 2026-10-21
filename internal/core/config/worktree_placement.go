package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrPlacementCollision means the worktree destination is already taken: a
	// file, directory, symlink (even a dangling one) or registered worktree.
	ErrPlacementCollision = errors.New("worktree destination already exists")
	// ErrPlacementUnsafe means the destination escapes the authorized roots or
	// cannot be proven to stay inside them.
	ErrPlacementUnsafe = errors.New("worktree placement is unsafe")
)

// WorktreePlacement is where a branch's worktree would be created.
type WorktreePlacement struct {
	// Path is the worktree directory, with existing ancestors symlink-resolved.
	Path string
	// Root is the resolved directory worktrees of this repository live under.
	Root string
	// Revision is the project-roots revision the placement was resolved against.
	Revision uint64
	// ConfigHash digests the resolved root, template, upstream and lifecycle
	// hooks, so a changed .bonsai.yaml is visible without storing hook commands.
	ConfigHash string
}

// PreviewWorktreePlacement resolves where a new worktree for branch would go. It
// creates no directory and writes no configuration, and refuses a destination
// that is already taken. Arguments after the context: the project-roots file,
// the main repository root and the branch.
func PreviewWorktreePlacement(ctx context.Context, settingsPath, mainRoot, branch string) (WorktreePlacement, error) {
	settings, err := ReadProjectRoots(settingsPath)
	if err != nil {
		return WorktreePlacement{}, err
	}
	pl, _, err := resolvePlacement(ctx, settings, mainRoot, branch, placementPreview)
	return pl, err
}

// InspectWorktreePlacement resolves a placement the same way but accepts an
// existing destination. Recovery uses it to compare an interrupted creation's
// recorded placement with the current configuration.
func InspectWorktreePlacement(ctx context.Context, settingsPath, mainRoot, branch string) (WorktreePlacement, error) {
	settings, err := ReadProjectRoots(settingsPath)
	if err != nil {
		return WorktreePlacement{}, err
	}
	pl, _, err := resolvePlacement(ctx, settings, mainRoot, branch, placementInspect)
	return pl, err
}

// PlacementSnapshot is a placement together with the delete hooks of the very
// configuration it was resolved from, so what is checked and what is executed
// cannot differ.
type PlacementSnapshot struct {
	WorktreePlacement
	DeleteHooks []string
}

// SnapshotWorktreePlacement resolves an existing worktree's placement like
// InspectWorktreePlacement and returns the delete hooks from the same read of the
// configuration. Callers hold the project-roots lock when the pair must also be
// stable against root edits.
func SnapshotWorktreePlacement(ctx context.Context, settingsPath, mainRoot, branch string) (PlacementSnapshot, error) {
	settings, err := ReadProjectRoots(settingsPath)
	if err != nil {
		return PlacementSnapshot{}, err
	}
	pl, cfg, err := resolvePlacement(ctx, settings, mainRoot, branch, placementInspect)
	if err != nil {
		return PlacementSnapshot{}, err
	}
	return PlacementSnapshot{WorktreePlacement: pl, DeleteHooks: append([]string(nil), cfg.DeleteHooks()...)}, nil
}

// WithWorktreePlacement re-resolves the placement under the project-roots lock,
// creates its parent directories safely and calls fn with the lock still held,
// so the configuration cannot change between the check and the creation. The
// caller compares the placement with the one it previewed.
func WithWorktreePlacement(ctx context.Context, settingsPath, mainRoot, branch string, fn func(WorktreePlacement) error) error {
	return WithProjectRootsContext(ctx, settingsPath, func(settings ProjectRoots) error {
		placement, _, err := resolvePlacement(ctx, settings, mainRoot, branch, placementCreate)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(placement)
	})
}

type placementMode int

const (
	placementPreview placementMode = iota
	placementInspect
	placementCreate
)

func resolvePlacement(ctx context.Context, settings ProjectRoots, mainRoot, branch string, mode placementMode) (WorktreePlacement, *Config, error) {
	if strings.TrimSpace(branch) == "" || strings.ContainsRune(branch, 0) {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: invalid branch", ErrPlacementUnsafe)
	}
	main, err := CanonicalDirectory(mainRoot)
	if err != nil {
		return WorktreePlacement{}, nil, err
	}
	base, err := resolveWorktreeBase(ctx, settings, main)
	if err != nil {
		return WorktreePlacement{}, nil, err
	}
	var root string
	if mode == placementCreate {
		root, err = prepareBrowserDirectory(settings.Roots, base.root)
	} else {
		_, root, err = checkBrowserDirectory(settings.Roots, base.root)
	}
	if err != nil {
		return WorktreePlacement{}, nil, err
	}

	cfg := *base.cfg
	cfg.Worktree.Root = root
	template := cfg.Worktree.PathTemplate
	if template == "" {
		template = "{repo}-{branch}"
	}
	name := strings.ReplaceAll(template, "{repo}", filepath.Base(main))
	name = strings.ReplaceAll(name, "{branch}", strings.ReplaceAll(branch, "/", "-"))
	path := cfg.WorktreePath(main, branch)
	if !isAbsOrRooted(name) && !ContainsPath(root, path) {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: path_template leaves the worktree root", ErrPlacementUnsafe)
	}
	if path == root {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: destination is the worktree root", ErrPlacementUnsafe)
	}
	// An entry already at the destination, even a link that dangles, is a
	// collision for a new worktree and would defeat the link-following below.
	if mode != placementInspect {
		if _, err := os.Lstat(path); err == nil {
			return WorktreePlacement{}, nil, fmt.Errorf("%w: %s", ErrPlacementCollision, path)
		} else if !os.IsNotExist(err) {
			return WorktreePlacement{}, nil, fmt.Errorf("%w: destination unreadable: %w", ErrPlacementUnsafe, err)
		}
	}
	// Absolute templates are allowed only where a configured project root owns
	// the resolved destination; so are relative ones, once links are followed.
	_, dest, err := checkBrowserDirectory(settings.Roots, path)
	if err != nil {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: %w", ErrPlacementUnsafe, err)
	}
	if !isAbsOrRooted(name) && !ContainsPath(root, dest) {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: destination resolves outside the worktree root", ErrPlacementUnsafe)
	}
	if dest == root || sameDir(dest, main) {
		return WorktreePlacement{}, nil, fmt.Errorf("%w: destination is the root or the main worktree", ErrPlacementUnsafe)
	}
	if mode != placementInspect {
		if err := requireFreeDestination(dest, base); err != nil {
			return WorktreePlacement{}, nil, err
		}
	}
	if mode == placementCreate {
		if parent := filepath.Dir(dest); parent != root {
			if _, err := prepareBrowserDirectory(settings.Roots, parent); err != nil {
				return WorktreePlacement{}, nil, fmt.Errorf("%w: %w", ErrPlacementUnsafe, err)
			}
		}
	}
	return WorktreePlacement{Path: dest, Root: root, Revision: settings.Revision, ConfigHash: placementHash(root, template, base.cfg)}, base.cfg, nil
}

// requireFreeDestination refuses anything at dest, including a link that dangles,
// and a path an existing worktree already uses. A read error is not absence.
func requireFreeDestination(dest string, base worktreeBase) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%w: %s", ErrPlacementCollision, dest)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%w: destination unreadable: %w", ErrPlacementUnsafe, err)
	}
	for _, tree := range base.trees {
		if filepath.Clean(tree.Path) == dest || sameDir(tree.Path, dest) {
			return fmt.Errorf("%w: registered worktree", ErrPlacementCollision)
		}
	}
	return nil
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

func placementHash(root, template string, cfg *Config) string {
	raw, _ := json.Marshal(struct {
		Root     string
		Template string
		Upstream string
		Create   []string
		Delete   []string
	}{root, template, cfg.Upstream, cfg.CreateHooks(), cfg.DeleteHooks()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
