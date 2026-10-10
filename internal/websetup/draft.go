// Package websetup is the model behind `bonsai web setup`: what the user is
// choosing (a Draft), what applying it changes (a Plan) and the words used to
// explain each choice. It has no terminal code; internal/ui/websetup renders
// it and internal/cli applies it.
package websetup

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type Repo struct {
	ID, Name, Path string
}

// Folder is one row of the Projects screen: a configured project root, or a
// suggestion that becomes one when checked.
type Folder struct {
	Path     string
	RootID   string // set when the folder is already a project root
	Checked  bool
	ThisRepo bool // the repository bonsai web was started from
	Scanned  bool
	Repos    []Repo
	Err      string
	// Touched is set once the user toggles the row, so a scan finishing later
	// never overrides their choice.
	Touched bool
}

// Existing reports whether the folder is already a project root.
func (f Folder) Existing() bool { return f.RootID != "" }

// Draft is everything the setup screens edit. Nothing in it is written until
// the user confirms on the Review screen.
type Draft struct {
	Config  config.WebConfig
	Folders []Folder
	// RotateSecret asks Apply for a new live-updates webhook secret.
	RotateSecret bool
}

// Clone returns a deep copy, so the initial draft survives edits.
func (d Draft) Clone() Draft {
	out := d
	out.Config.Updates.Live.Command = append([]string{}, d.Config.Updates.Live.Command...)
	out.Config.Updates.Live.Repositories = append([]string{}, d.Config.Updates.Live.Repositories...)
	out.Folders = make([]Folder, len(d.Folders))
	for i, f := range d.Folders {
		f.Repos = append([]Repo{}, f.Repos...)
		out.Folders[i] = f
	}
	return out
}

// NewDraft builds the starting point of the setup screens.
//
// Configured roots are listed checked. Suggestions that no root already
// covers follow; on a first run with no roots they start checked (a scan
// that finds no repositories unchecks them again, see SetScan), so accepting
// the defaults gives a working project list. thisRepo is the main worktree of
// the repository bonsai web was started from, or "".
//
// A brand-new user (no web.json yet) gets the recommended "this computer
// only"; anyone else keeps their saved interfaces.
func NewDraft(cfg config.WebConfig, exists bool, roots []config.ProjectRoot, suggestions []string, thisRepo string) Draft {
	d := Draft{Config: cfg}
	if !exists {
		d.Config.Interfaces = config.WebInterfaces{Local: true, Hosted: false}
	}
	covered := func(path string) bool {
		for _, f := range d.Folders {
			if f.Path == path || (f.Existing() && config.ContainsPath(f.Path, path)) {
				return true
			}
		}
		return false
	}
	for _, root := range roots {
		d.Folders = append(d.Folders, Folder{Path: root.Path, RootID: root.ID, Checked: true})
	}
	preselect := len(roots) == 0
	if thisRepo != "" && !covered(thisRepo) {
		d.Folders = append(d.Folders, Folder{Path: thisRepo, ThisRepo: true, Checked: preselect})
	}
	for _, path := range suggestions {
		if !covered(path) {
			d.Folders = append(d.Folders, Folder{Path: path, Checked: preselect})
		}
	}
	return d
}

// SetScan records what discovery found in folder i. An untouched suggestion
// with no repositories is unchecked: it would only add an empty root.
func (d *Draft) SetScan(i int, repos []Repo, err string) {
	if i < 0 || i >= len(d.Folders) {
		return
	}
	f := &d.Folders[i]
	f.Scanned, f.Repos, f.Err = true, repos, err
	if !f.Existing() && !f.Touched && !f.ThisRepo && (len(repos) == 0 || err != "") {
		f.Checked = false
	}
}

// Toggle flips folder i.
func (d *Draft) Toggle(i int) {
	if i < 0 || i >= len(d.Folders) {
		return
	}
	d.Folders[i].Checked = !d.Folders[i].Checked
	d.Folders[i].Touched = true
}

// AddFolder appends a folder the user typed (already canonical) and checks
// it. It returns the folder's index; an existing row is checked instead.
func (d *Draft) AddFolder(path string) int {
	for i, f := range d.Folders {
		if f.Path == path {
			d.Folders[i].Checked, d.Folders[i].Touched = true, true
			return i
		}
	}
	d.Folders = append(d.Folders, Folder{Path: path, Checked: true, Touched: true})
	return len(d.Folders) - 1
}

// AfterApply is the draft as it is on disk once a plan built from it was
// applied: checked folders are project roots now, unchecked ones are not.
// The next plan is computed against it, so nothing is applied twice.
func (d Draft) AfterApply() Draft {
	out := d.Clone()
	for i, f := range out.Folders {
		if f.Checked {
			if f.RootID == "" {
				out.Folders[i].RootID = config.PathID("root", f.Path)
			}
		} else {
			out.Folders[i].RootID = ""
		}
		out.Folders[i].Touched = false
	}
	out.RotateSecret = false
	return out
}

// Pending lists checked folders whose scan has not finished yet.
func (d Draft) Pending() []Folder {
	var out []Folder
	for _, f := range d.Folders {
		if f.Checked && !f.Scanned {
			out = append(out, f)
		}
	}
	return out
}

// CheckedFolders lists the folders that will be project roots.
func (d Draft) CheckedFolders() []Folder {
	var out []Folder
	for _, f := range d.Folders {
		if f.Checked {
			out = append(out, f)
		}
	}
	return out
}

// RepoCount counts the distinct repositories under the checked folders, and
// whether every checked folder has been scanned.
func (d Draft) RepoCount() (int, bool) {
	seen := map[string]bool{}
	complete := true
	for _, f := range d.CheckedFolders() {
		if !f.Scanned {
			complete = false
		}
		for _, r := range f.Repos {
			seen[r.ID] = true
		}
	}
	return len(seen), complete
}

// TildePath shortens a path under home to ~/..., for display only.
func TildePath(home, path string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

// ProjectsSummary is the one-line description of the checked folders.
func ProjectsSummary(d Draft, home string) string {
	folders := d.CheckedFolders()
	if len(folders) == 0 {
		return "no folders yet"
	}
	names := make([]string, 0, len(folders))
	for _, f := range folders {
		names = append(names, TildePath(home, f.Path))
	}
	sort.Strings(names)
	count, complete := d.RepoCount()
	repos := strconv.Itoa(count) + " repos"
	if count == 1 {
		repos = "1 repo"
	}
	if !complete {
		repos = "counting repos…"
	}
	return strings.Join(names, ", ") + " · " + repos
}
