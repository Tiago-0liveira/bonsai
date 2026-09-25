package pkgmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Location describes where discovery started and the roots that were found.
type Location struct {
	InputDir       string `json:"input_dir"`
	ProjectRoot    string `json:"project_root"`
	WorkspaceRoot  string `json:"workspace_root,omitempty"`
	RepositoryRoot string `json:"repository_root,omitempty"`
}

func resolveLocation(dir string) (Location, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return Location{}, err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Location{}, err
	}
	abs = filepath.Clean(abs)
	st, err := os.Stat(abs)
	if err != nil {
		return Location{}, err
	}
	if !st.IsDir() {
		return Location{}, fmt.Errorf("pkgmgr: %s is not a directory", abs)
	}

	loc := Location{InputDir: abs, ProjectRoot: abs}
	for cur := abs; ; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			loc.RepositoryRoot = cur
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return loc, nil
}

func nearestRoot(input string, roots ...string) string {
	input = filepath.Clean(input)
	best := ""
	bestDistance := int(^uint(0) >> 1)
	for _, root := range roots {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)

		distance, ok := pathDistance(input, root)
		if !ok {
			continue
		}
		if distance < bestDistance || (distance == bestDistance && (best == "" || root < best)) {
			best = root
			bestDistance = distance
		}
	}
	return best
}

func pathDistance(a, b string) (int, bool) {
	if rel, err := filepath.Rel(a, b); err == nil && pathIsWithin(rel) {
		return pathDepth(rel), true
	}
	if rel, err := filepath.Rel(b, a); err == nil && pathIsWithin(rel) {
		return pathDepth(rel), true
	}
	return 0, false
}

func pathIsWithin(rel string) bool {
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func pathDepth(rel string) int {
	rel = filepath.Clean(rel)
	if rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func findUp(start string, names ...string) (string, string) {
	for cur := start; ; cur = filepath.Dir(cur) {
		for _, name := range names {
			path := filepath.Join(cur, name)
			if _, err := os.Stat(path); err == nil {
				return cur, path
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ""
		}
	}
}


var skippedProjectDirs = map[string]bool{
	".git":        true,
	"node_modules": true,
	"target":      true,
	"vendor":      true,
	".venv":       true,
	"venv":        true,
	"__pycache__": true,
}

func findUpTo(start, boundary string, names ...string) (string, string) {
	boundary = filepath.Clean(boundary)
	for cur := filepath.Clean(start); ; cur = filepath.Dir(cur) {
		for _, name := range names {
			path := filepath.Join(cur, name)
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				return cur, path
			}
		}
		if boundary != "" && cur == boundary {
			return "", ""
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", ""
		}
	}
}

// findProjectFileWithin searches upward first, stopping at boundary when one is
// known, then performs a deterministic breadth-first scan below start bounded
// by maxDepth. Symlinked directories are not followed.
func findProjectFileWithin(start, boundary string, maxDepth int, names ...string) (string, string) {
	if root, path := findUpTo(start, boundary, names...); root != "" {
		return root, path
	}
	if maxDepth <= 0 {
		return "", ""
	}

	type queuedDir struct {
		path  string
		depth int
	}
	queue := []queuedDir{{path: start, depth: 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= maxDepth {
			continue
		}
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || skippedProjectDirs[entry.Name()] {
				continue
			}
			child := filepath.Join(current.path, entry.Name())
			for _, name := range names {
				path := filepath.Join(child, name)
				if st, err := os.Stat(path); err == nil && !st.IsDir() {
					return child, path
				}
			}
			queue = append(queue, queuedDir{path: child, depth: current.depth + 1})
		}
	}
	return "", ""
}


// findProjectFile is retained for focused tests/helpers outside a resolved Git
// repository; provider detection should use findProjectFileWithin.
func findProjectFile(start string, maxDepth int, names ...string) (string, string) {
	return findProjectFileWithin(start, "", maxDepth, names...)
}


// projectSearchDirs returns start plus descendant directories up to maxDepth in
// deterministic breadth-first order. Dependency/build directories and symlinked
// directories are skipped.
func projectSearchDirs(start string, maxDepth int) []string {
	start = filepath.Clean(start)
	out := []string{start}
	if maxDepth <= 0 {
		return out
	}
	type queuedDir struct {
		path  string
		depth int
	}
	queue := []queuedDir{{path: start, depth: 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= maxDepth {
			continue
		}
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || skippedProjectDirs[entry.Name()] {
				continue
			}
			child := filepath.Join(current.path, entry.Name())
			out = append(out, child)
			queue = append(queue, queuedDir{path: child, depth: current.depth + 1})
		}
	}
	return out
}
