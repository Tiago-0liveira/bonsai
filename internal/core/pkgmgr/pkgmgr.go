// Package pkgmgr detects a project's package manager / build tool and exposes its
// runnable scripts or targets. Supported: npm, pnpm, yarn, bun (package.json),
// cargo (Cargo.toml), and make (Makefile).
package pkgmgr

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// PackageManager exposes the runnable scripts/targets of a project.
type PackageManager interface {
	// Name is the manager's short name, e.g. "npm", "cargo", "make".
	Name() string
	// GetScripts returns the selectable script/target names.
	GetScripts() []string
	// RunCommand returns the shell command that runs the named script/target.
	RunCommand(name string) string
}

// packageJSON models the fields of package.json that we care about.
type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// nodeManager backs the JavaScript ecosystem managers, which all read
// package.json scripts but differ in their run prefix.
type nodeManager struct {
	name    string // npm / pnpm / yarn / bun
	runVerb string // "run" for npm/pnpm/bun; "" for yarn (yarn <script>)
	scripts map[string]string
}

func (n *nodeManager) Name() string { return n.name }

// GetScripts returns the script names sorted alphabetically.
func (n *nodeManager) GetScripts() []string {
	names := make([]string, 0, len(n.scripts))
	for name := range n.scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (n *nodeManager) RunCommand(name string) string {
	if n.runVerb == "" {
		return n.name + " " + name
	}
	return n.name + " " + n.runVerb + " " + name
}

// cargoManager backs Rust projects. Cargo has no user scripts, so a fixed set of
// common subcommands is exposed.
type cargoManager struct{}

func (cargoManager) Name() string { return "cargo" }
func (cargoManager) GetScripts() []string {
	return []string{"build", "check", "clippy", "fmt", "run", "test"}
}
func (cargoManager) RunCommand(n string) string { return "cargo " + n }

// makeManager backs Makefile projects. Scripts are the parsed targets.
type makeManager struct{ targets []string }

func (m makeManager) Name() string               { return "make" }
func (m makeManager) GetScripts() []string       { return m.targets }
func (m makeManager) RunCommand(n string) string { return "make " + n }

// Detect returns a PackageManager for dir, or (nil, nil) if none is recognized.
// Node ecosystem (by lockfile) takes priority, then cargo, then make.
func Detect(dir string) (PackageManager, error) {
	pj, ok, err := readPackageJSON(dir)
	if err != nil {
		return nil, err
	}
	if ok {
		name, verb := "npm", "run"
		switch {
		case exists(dir, "pnpm-lock.yaml"):
			name, verb = "pnpm", "run"
		case exists(dir, "yarn.lock"):
			name, verb = "yarn", ""
		case exists(dir, "bun.lockb"), exists(dir, "bun.lock"):
			name, verb = "bun", "run"
		}
		return &nodeManager{name: name, runVerb: verb, scripts: pj.Scripts}, nil
	}

	if exists(dir, "Cargo.toml") {
		return cargoManager{}, nil
	}

	if mk := makefilePath(dir); mk != "" {
		targets, err := parseMakeTargets(mk)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 {
			return makeManager{targets: targets}, nil
		}
	}

	return nil, nil
}

// readPackageJSON parses dir/package.json. ok is false when the file is absent.
func readPackageJSON(dir string) (packageJSON, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return packageJSON{}, false, nil
		}
		return packageJSON{}, false, err
	}
	var m packageJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return packageJSON{}, false, err
	}
	return m, true, nil
}

// exists reports whether dir/name is present.
func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// makefilePath returns the path of the Makefile in dir, or "" if none.
func makefilePath(dir string) string {
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// targetRe matches a Makefile target definition, capturing the target name.
var targetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*:(?:[^=]|$)`)

// parseMakeTargets extracts real targets from a Makefile: skips dot-prefixed
// special targets (.PHONY etc.), pattern rules (containing '%'), and duplicates.
func parseMakeTargets(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		out  []string
		seen = map[string]bool{}
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			continue // recipe line
		}
		m := targetRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if strings.Contains(name, "%") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
