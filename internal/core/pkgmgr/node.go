package pkgmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type nodeProvider struct{}

func (nodeProvider) ID() string { return "node" }

type nodeManifest struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
	Workspaces     json.RawMessage   `json:"workspaces"`
}

type nodeDetection struct {
	Manifest      nodeManifest
	ManifestPath  string
	Manager       string
	LockfilePath  string
	WorkspaceRoot string
}

func (nodeProvider) Detect(ctx Context) (Detection, error) {
	root, manifestPath := findProjectFile(ctx.Location.InputDir, ctx.Options.searchDepth(), "package.json")
	if root == "" {
		return Detection{}, nil
	}
	manifest, err := readNodeManifest(manifestPath)
	if err != nil {
		return Detection{}, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	workspaceRoot := findNodeWorkspaceRoot(root)
	manager, lock := detectNodeManager(root, workspaceRoot, manifest)
	return Detection{
		Applicable:    true,
		ID:            "node:" + manager,
		Name:          manager,
		Root:          root,
		WorkspaceRoot: workspaceRoot,
		Data: nodeDetection{
			Manifest:      manifest,
			ManifestPath:  manifestPath,
			Manager:       manager,
			LockfilePath:  lock,
			WorkspaceRoot: workspaceRoot,
		},
	}, nil
}

func (nodeProvider) Commands(_ Context, detection Detection) ([]Command, error) {
	d, ok := detection.Data.(nodeDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid node detection data")
	}
	names := make([]string, 0, len(d.Manifest.Scripts))
	for name := range d.Manifest.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	commands := make([]Command, 0, len(names))
	for _, name := range names {
		id := "node:script:" + name
		if d.WorkspaceRoot != "" && filepath.Clean(d.WorkspaceRoot) != filepath.Clean(detection.Root) {
			if rel, err := filepath.Rel(d.WorkspaceRoot, detection.Root); err == nil && rel != "." {
				id = "node:" + filepath.ToSlash(rel) + ":script:" + name
			}
		}
		prefix := []string{"run", name}
		pass := PassThroughDoubleDash
		if d.Manager == "yarn" {
			prefix = []string{name}
			pass = PassThroughAppend
		}
		commands = append(commands, Command{
			ID:       id,
			Name:     name,
			Provider: detection.ID,
			Kind:     CommandProject,
			Args: []Argument{{
				ID:         "args",
				Name:       "arguments",
				Kind:       ArgumentPassThrough,
				Type:       ValueUnknown,
				Variadic:   true,
				Source:     Source{Kind: "package.json", File: d.ManifestPath, Pointer: "scripts." + name},
				Confidence: ConfidenceExact,
			}},
			Invocation: InvocationSpec{Program: d.Manager, Prefix: prefix, WorkingDir: detection.Root, PassThrough: pass},
			Source:     Source{Kind: "package.json", File: d.ManifestPath, Pointer: "scripts." + name},
			Confidence: ConfidenceExact,
			Raw:        d.Manifest.Scripts[name],
		})
	}
	return commands, nil
}

func (nodeProvider) FingerprintInputs(_ Context, detection Detection) ([]FingerprintInput, error) {
	d, ok := detection.Data.(nodeDetection)
	if !ok {
		return nil, fmt.Errorf("pkgmgr: invalid node detection data")
	}
	var paths []string
	paths = append(paths, d.ManifestPath)
	if d.LockfilePath != "" {
		paths = append(paths, d.LockfilePath)
	}
	if d.WorkspaceRoot != "" {
		workspaceManifest := filepath.Join(d.WorkspaceRoot, "package.json")
		if filepath.Clean(workspaceManifest) != filepath.Clean(d.ManifestPath) {
			if _, err := os.Stat(workspaceManifest); err == nil {
				paths = append(paths, workspaceManifest)
			}
		}
		pnpm := filepath.Join(d.WorkspaceRoot, "pnpm-workspace.yaml")
		if _, err := os.Stat(pnpm); err == nil {
			paths = append(paths, pnpm)
		}
	}
	return fileInputs("node", detection.WorkspaceRoot, detection.Root, paths)
}

func readNodeManifest(path string) (nodeManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nodeManifest{}, err
	}
	var manifest nodeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nodeManifest{}, err
	}
	if manifest.Scripts == nil {
		manifest.Scripts = map[string]string{}
	}
	return manifest, nil
}

func detectNodeManager(root, workspaceRoot string, manifest nodeManifest) (string, string) {
	if manager := managerFromPackageManager(manifest.PackageManager); manager != "" {
		return manager, findManagerLock(root, workspaceRoot, manager)
	}
	if workspaceRoot != "" && filepath.Clean(workspaceRoot) != filepath.Clean(root) {
		if workspaceManifest, err := readNodeManifest(filepath.Join(workspaceRoot, "package.json")); err == nil {
			if manager := managerFromPackageManager(workspaceManifest.PackageManager); manager != "" {
				return manager, findManagerLock(root, workspaceRoot, manager)
			}
		}
	}
	for _, candidate := range []struct {
		manager string
		files   []string
	}{
		{"pnpm", []string{"pnpm-lock.yaml"}},
		{"yarn", []string{"yarn.lock"}},
		{"bun", []string{"bun.lock", "bun.lockb"}},
		{"npm", []string{"package-lock.json"}},
	} {
		for _, base := range uniqueDirs(root, workspaceRoot) {
			for _, name := range candidate.files {
				path := filepath.Join(base, name)
				if _, err := os.Stat(path); err == nil {
					return candidate.manager, path
				}
			}
		}
	}
	return "npm", ""
}

func managerFromPackageManager(value string) string {
	value = strings.TrimSpace(value)
	for _, manager := range []string{"npm", "pnpm", "yarn", "bun"} {
		if value == manager || strings.HasPrefix(value, manager+"@") {
			return manager
		}
	}
	return ""
}

func findManagerLock(root, workspaceRoot, manager string) string {
	var names []string
	switch manager {
	case "pnpm":
		names = []string{"pnpm-lock.yaml"}
	case "yarn":
		names = []string{"yarn.lock"}
	case "bun":
		names = []string{"bun.lock", "bun.lockb"}
	case "npm":
		names = []string{"package-lock.json"}
	}
	for _, base := range uniqueDirs(root, workspaceRoot) {
		for _, name := range names {
			path := filepath.Join(base, name)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

func uniqueDirs(dirs ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

func findNodeWorkspaceRoot(projectRoot string) string {
	for cur := projectRoot; ; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, "pnpm-workspace.yaml")); err == nil {
			return cur
		}
		manifestPath := filepath.Join(cur, "package.json")
		if manifest, err := readNodeManifest(manifestPath); err == nil && hasWorkspaces(manifest.Workspaces) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return ""
}

func hasWorkspaces(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return len(list) > 0
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	return json.Unmarshal(raw, &obj) == nil && len(obj.Packages) > 0
}
