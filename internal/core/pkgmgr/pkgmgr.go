// Package pkgmgr parses project package manifests and extracts runnable scripts.
package pkgmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// PackageManager exposes the scripts defined by a project manifest.
type PackageManager interface {
	GetScripts() []string
}

// packageJSON models the fields of package.json that we care about.
type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// NPM is a package.json-backed PackageManager.
type NPM struct {
	manifest packageJSON
}

// GetScripts returns the script names sorted alphabetically.
func (n *NPM) GetScripts() []string {
	names := make([]string, 0, len(n.manifest.Scripts))
	for name := range n.manifest.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Command returns the shell command for a named script (empty if absent).
func (n *NPM) Command(name string) string { return n.manifest.Scripts[name] }

// Detect returns a PackageManager for dir, or (nil, nil) if none is recognized.
func Detect(dir string) (PackageManager, error) {
	pj := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pj)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m packageJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &NPM{manifest: m}, nil
}
