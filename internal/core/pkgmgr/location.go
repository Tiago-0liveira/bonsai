package pkgmgr

import (
	"fmt"
	"os"
	"path/filepath"
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
	best := ""
	for _, root := range roots {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, input)
		if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
			continue
		}
		if best == "" || len(filepath.Clean(root)) > len(filepath.Clean(best)) {
			best = root
		}
	}
	return best
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

// findProjectFile first preserves the historical upward lookup. If nothing is
// found, it performs a deterministic breadth-first scan below start, bounded by
// maxDepth. Symlinked directories are not followed.
func findProjectFile(start string, maxDepth int, names ...string) (string, string) {
	if root, path := findUp(start, names...); root != "" {
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
