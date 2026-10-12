// Package editors discovers GUI editors for the environment running Bonsai:
// commands on PATH and a few well-known user directories on Linux, PATH plus
// standard application bundles on macOS, and PATH plus the standard install
// folders on Windows. On WSL it also finds the WSL-aware wrapper scripts that
// Windows editors ship, so Windows VS Code, Cursor and Zed can open files that
// live inside the distribution.
package editors

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Environment struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Editor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	// Folders reports whether the editor can open a worktree directory.
	Folders bool `json:"folders"`
	args    []string
	// windows marks a Windows editor reached from WSL through its own
	// WSL-aware wrapper script. It runs with the unmodified environment.
	windows bool
}

type Discovery struct {
	Environment Environment
	OS          string
	Path        string
	WSL         bool
	// Distro is the WSL distribution name, passed to Windows editor wrappers.
	Distro string
	// ExtraDirs are searched after PATH for commands: user and package manager
	// bin directories a background service may not inherit.
	ExtraDirs []string
	// AppRoots hold application installs that may not be on PATH: Windows
	// "Programs"/"Program Files", macOS "Applications", Linux /opt and friends.
	AppRoots []string
	// WindowsMounts includes nonstandard WSL drive mount locations.
	WindowsMounts []string
	// convertPath turns a WSL path into a Windows path; nil runs wslpath.
	convertPath func(string) (string, error)
}

func Current() Discovery {
	host, _ := os.Hostname()
	d := Discovery{OS: runtime.GOOS, Path: os.Getenv("PATH")}
	d.ExtraDirs, d.AppRoots = defaultSearchDirs(runtime.GOOS, os.Getenv, userHome())
	d.Environment = Environment{ID: runtime.GOOS + ":" + host, Label: map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}[runtime.GOOS]}
	if d.Environment.Label == "" {
		d.Environment.Label = runtime.GOOS
	}
	if runtime.GOOS == "linux" {
		distro := os.Getenv("WSL_DISTRO_NAME")
		kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
		if distro != "" || strings.Contains(strings.ToLower(string(kernel)), "microsoft") {
			d.WSL = true
			d.Distro = distro
			d.Environment = Environment{ID: "wsl:" + host + ":" + distro, Label: "WSL"}
			if distro != "" {
				d.Environment.Label += " (" + distro + ")"
			}
			mounts, _ := os.ReadFile("/proc/self/mountinfo")
			d.WindowsMounts = windowsMounts(string(mounts))
		}
	}
	return d
}

func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}

// defaultSearchDirs lists the per-OS places editors normally install to.
func defaultSearchDirs(goos string, getenv func(string) string, home string) (extra, apps []string) {
	under := func(base string, elems ...string) string {
		if base == "" {
			return ""
		}
		return filepath.Join(append([]string{base}, elems...)...)
	}
	keep := func(values ...string) []string {
		var out []string
		for _, value := range values {
			if value != "" {
				out = append(out, value)
			}
		}
		return out
	}
	switch goos {
	case "windows":
		apps = keep(under(getenv("LOCALAPPDATA"), "Programs"), getenv("ProgramFiles"), getenv("ProgramFiles(x86)"))
	case "darwin":
		extra = keep("/opt/homebrew/bin", "/usr/local/bin", under(home, ".local", "bin"))
		apps = keep("/Applications", under(home, "Applications"))
	default:
		extra = keep(under(home, ".local", "bin"), "/usr/local/bin", "/snap/bin")
		apps = keep("/usr/share", "/opt", under(home, ".local"))
	}
	return extra, apps
}

// mountinfo escapes spaces, tabs, newlines and backslashes in mount paths.
func windowsMounts(mountinfo string) []string {
	var mounts []string
	for _, line := range strings.Split(mountinfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 6 && (strings.Contains(line, " - drvfs ") || strings.Contains(line, "aname=drvfs")) {
			mounts = append(mounts, strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(fields[4]))
		}
	}
	return mounts
}

func (d Discovery) foreignPath(path string) bool {
	if d.OS == "windows" {
		return false
	}
	path = filepath.Clean(path)
	// Skip WSL's default Windows drive mounts before inspecting any files.
	if d.WSL && strings.HasPrefix(path, "/mnt/") && len(path) >= 6 && path[5] >= 'a' && path[5] <= 'z' && (len(path) == 6 || path[6] == '/') {
		return true
	}
	for _, mount := range d.WindowsMounts {
		if path == mount || strings.HasPrefix(path, mount+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (d Discovery) nativeCommand(path string) bool {
	if d.foreignPath(path) {
		return false
	}
	// Do not inspect a known Windows target through a local command symlink.
	if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		if d.foreignPath(target) {
			return false
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || d.foreignPath(resolved) {
		return false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || (d.OS != "windows" && info.Mode()&0111 == 0) {
		return false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return false
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, 64*1024))
	if d.OS == "windows" {
		return bytes.HasPrefix(head, []byte("MZ"))
	}
	if bytes.HasPrefix(head, []byte("MZ")) {
		return false
	}
	if bytes.HasPrefix(head, []byte("#!")) {
		// Also reject local symlinks/copies of Windows forwarding wrappers.
		lower := strings.ToLower(string(head))
		return !strings.Contains(lower, ".exe") && !strings.Contains(lower, "powershell") && !strings.Contains(lower, "/mnt/c/")
	}
	if d.OS == "linux" {
		return bytes.HasPrefix(head, []byte("\x7fELF"))
	}
	// Mach-O, including universal executables, on macOS.
	return d.OS == "darwin" && len(head) >= 4 && (bytes.Equal(head[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.Equal(head[:4], []byte{0xfe, 0xed, 0xfa, 0xcf}) || bytes.Equal(head[:4], []byte{0xca, 0xfe, 0xba, 0xbe}) || bytes.Equal(head[:4], []byte{0xca, 0xfe, 0xba, 0xbf}))
}

func (d Discovery) nativeDirectories() []string {
	dirs := []string{}
	seen := map[string]bool{}
	for _, dir := range append(filepath.SplitList(d.Path), d.ExtraDirs...) {
		// Skip inherited host paths and current/project-relative search paths.
		if !filepath.IsAbs(dir) || d.foreignPath(dir) || seen[dir] {
			continue
		}
		seen[dir] = true
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil && !d.foreignPath(resolved) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

type candidate struct {
	id, name string
	commands []string
	// args precede a file path; they are not used when opening a folder.
	args    []string
	folders bool
	// shims maps a command whose PATH entry is a Windows .cmd shim to the GUI
	// executable it starts, relative to the shim's directory.
	shims map[string]string
	// apps are install locations relative to Discovery.AppRoots, per GOOS.
	apps map[string][]string
}

var candidates = []candidate{
	{id: "vscode", name: "Visual Studio Code", commands: []string{"code"}, args: []string{"--goto"}, folders: true,
		shims: map[string]string{"code": "../Code.exe"},
		apps: map[string][]string{
			"windows": {"Microsoft VS Code/Code.exe"},
			"darwin":  {"Visual Studio Code.app/Contents/Resources/app/bin/code"},
			"linux":   {"code/bin/code"},
		}},
	{id: "vscode-insiders", name: "Visual Studio Code Insiders", commands: []string{"code-insiders"}, args: []string{"--goto"}, folders: true,
		shims: map[string]string{"code-insiders": "../Code - Insiders.exe"},
		apps: map[string][]string{
			"windows": {"Microsoft VS Code Insiders/Code - Insiders.exe"},
			"darwin":  {"Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code-insiders"},
		}},
	{id: "cursor", name: "Cursor", commands: []string{"cursor"}, args: []string{"--goto"}, folders: true,
		shims: map[string]string{"cursor": "../../../Cursor.exe"},
		apps: map[string][]string{
			"windows": {"cursor/Cursor.exe"},
			"darwin":  {"Cursor.app/Contents/Resources/app/bin/cursor"},
		}},
	{id: "zed", name: "Zed", commands: []string{"zed", "zeditor"}, folders: true,
		apps: map[string][]string{
			"windows": {"Zed/bin/zed.exe", "Zed/Zed.exe"},
			"darwin":  {"Zed.app/Contents/MacOS/cli", "Zed Preview.app/Contents/MacOS/cli"},
			"linux":   {"zed.app/bin/zed", "zed-preview.app/bin/zed"},
		}},
	{id: "sublime", name: "Sublime Text", commands: []string{"subl"}, folders: true,
		apps: map[string][]string{
			"windows": {"Sublime Text/subl.exe"},
			"darwin":  {"Sublime Text.app/Contents/SharedSupport/bin/subl"},
		}},
	{id: "kate", name: "Kate", commands: []string{"kate"}},
	{id: "gedit", name: "Gedit", commands: []string{"gedit"}},
	{id: "gnome-text-editor", name: "GNOME Text Editor", commands: []string{"gnome-text-editor"}},
}

// find returns the first working command for a candidate: PATH and extra
// directories first, then known install locations.
func (d Discovery) find(c candidate, dirs []string) string {
	for _, command := range c.commands {
		for _, dir := range dirs {
			if d.OS != "windows" {
				if path := filepath.Join(dir, command); d.nativeCommand(path) {
					return path
				}
				continue
			}
			if path := filepath.Join(dir, command+".exe"); d.nativeCommand(path) {
				return path
			}
			// VS Code and Cursor put a .cmd shim on PATH. Run the GUI
			// executable it starts instead of going through cmd.exe, which
			// would interpret metacharacters in file names.
			if rel, ok := c.shims[command]; ok {
				if _, err := os.Stat(filepath.Join(dir, command+".cmd")); err == nil {
					if path := filepath.Clean(filepath.Join(dir, filepath.FromSlash(rel))); d.nativeCommand(path) {
						return path
					}
				}
			}
		}
	}
	for _, root := range d.AppRoots {
		for _, rel := range c.apps[d.OS] {
			if path := filepath.Join(root, filepath.FromSlash(rel)); d.nativeCommand(path) {
				return path
			}
		}
	}
	return ""
}

func (d Discovery) Editors() []Editor {
	result := []Editor{}
	dirs := d.nativeDirectories()
	for _, c := range candidates {
		if path := d.find(c, dirs); path != "" {
			result = append(result, Editor{ID: c.id, Name: c.name, Command: path, Folders: c.folders, args: c.args})
		}
	}
	return append(result, d.windowsEditors()...)
}

// windowsCandidates are Windows editors that ship a shell wrapper aware of WSL.
// The wrapper forwards to the Windows executable and tells it which
// distribution owns the file (Remote-WSL for VS Code, --wsl for Zed). Bare .exe
// files are deliberately not used: they would receive Linux paths unconverted.
var windowsCandidates = []struct {
	id, name, command string
	args              []string
	// installs are bin directories relative to a Windows "Programs" or
	// "Program Files" directory.
	installs []string
}{
	{"vscode-windows", "Visual Studio Code (Windows)", "code", []string{"--goto"}, []string{"Microsoft VS Code/bin"}},
	{"vscode-insiders-windows", "Visual Studio Code Insiders (Windows)", "code-insiders", []string{"--goto"}, []string{"Microsoft VS Code Insiders/bin"}},
	{"cursor-windows", "Cursor (Windows)", "cursor", []string{"--goto"}, []string{"cursor/resources/app/bin"}},
	{"zed-windows", "Zed (Windows)", "zed", nil, []string{"Zed/bin"}},
}

// windowsEditors finds Windows editors from WSL. PATH entries on a Windows drive
// win because they belong to the current Windows user; known install locations
// cover distributions that do not append the Windows PATH.
func (d Discovery) windowsEditors() []Editor {
	if !d.WSL || d.OS != "linux" {
		return nil
	}
	pathDirs := []string{}
	for _, dir := range filepath.SplitList(d.Path) {
		if filepath.IsAbs(dir) && d.foreignPath(dir) {
			pathDirs = append(pathDirs, dir)
		}
	}
	// Windows drives are slow to walk from WSL: list the roots once and probe
	// the candidates concurrently. Results keep the candidate order.
	roots := d.windowsInstallRoots()
	found := make([]string, len(windowsCandidates))
	var wg sync.WaitGroup
	for i, candidate := range windowsCandidates {
		dirs := append([]string{}, pathDirs...)
		for _, root := range roots {
			for _, install := range candidate.installs {
				dirs = append(dirs, filepath.Join(root, filepath.FromSlash(install)))
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, dir := range dirs {
				if path := filepath.Join(dir, candidate.command); d.windowsWrapper(path) {
					found[i] = path
					return
				}
			}
		}()
	}
	wg.Wait()
	var result []Editor
	for i, path := range found {
		if path != "" {
			candidate := windowsCandidates[i]
			result = append(result, Editor{ID: candidate.id, Name: candidate.name, Command: path, Folders: true, args: candidate.args, windows: true})
		}
	}
	return result
}

// windowsInstallRoots lists per-user and machine-wide Windows program folders.
func (d Discovery) windowsInstallRoots() []string {
	mounts := d.WindowsMounts
	if len(mounts) == 0 {
		mounts, _ = filepath.Glob("/mnt/[a-z]")
	}
	var roots []string
	for _, mount := range mounts {
		users, _ := filepath.Glob(filepath.Join(mount, "Users", "*", "AppData", "Local", "Programs"))
		roots = append(roots, users...)
		roots = append(roots, filepath.Join(mount, "Program Files"), filepath.Join(mount, "Program Files (x86)"))
	}
	return roots
}

// windowsWrapper accepts only a regular executable shell script on a Windows
// drive that mentions WSL_DISTRO_NAME, i.e. the vendor's WSL forwarding script.
func (d Discovery) windowsWrapper(path string) bool {
	// Most PATH entries lack the command; one cheap lookup avoids resolving
	// every path component over the Windows drive.
	if _, err := os.Lstat(path); err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !d.foreignPath(resolved) {
		return false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return false
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, 64*1024))
	return bytes.HasPrefix(head, []byte("#!")) && bytes.Contains(head, []byte("WSL_DISTRO_NAME"))
}

// Launch uses an argument vector, never a shell or a system file handler. The
// editor outlives the request; Wait reaps it even if its GUI stays open.
func (d Discovery) Launch(editor Editor, directory, file string) error {
	return d.start(editor, directory, append(append([]string{}, editor.args...), file))
}

// ErrFolderUnsupported is returned for editors that cannot open a directory.
var ErrFolderUnsupported = errors.New("this editor cannot open folders")

// LaunchFolder opens a directory, for example a worktree, in the editor.
func (d Discovery) LaunchFolder(editor Editor, directory string) error {
	if !editor.Folders {
		return ErrFolderUnsupported
	}
	return d.start(editor, directory, []string{directory})
}

func (d Discovery) start(editor Editor, directory string, args []string) error {
	cmd := exec.Command(editor.Command, args...)
	cmd.Dir = directory
	if editor.windows {
		cmd.Env = d.windowsLaunchEnv()
	} else {
		cmd.Env = d.nativeEnv()
	}
	return waitBriefly(cmd, editor.Name, false)
}

// nativeEnv keeps the environment but limits PATH to native directories, so
// launcher scripts resolve helpers in this environment, not on Windows.
func (d Discovery) nativeEnv() []string {
	env := []string{}
	for _, value := range os.Environ() {
		if key, _, ok := strings.Cut(value, "="); ok && !strings.EqualFold(key, "PATH") {
			env = append(env, value)
		}
	}
	return append(env, "PATH="+strings.Join(d.nativeDirectories(), string(os.PathListSeparator)))
}

// waitBriefly starts cmd and reports a failure within the first 300ms. The
// process outlives the request; Wait reaps it even if its GUI stays open.
// anyExit accepts every exit status, for launchers whose status means nothing.
func waitBriefly(cmd *exec.Cmd, name string, anyExit bool) error {
	var stderr limitedOutput
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot launch %s: %w", name, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		var exit *exec.ExitError
		if anyExit && errors.As(err, &exit) {
			err = nil
		}
		if err != nil {
			err = fmt.Errorf("%s exited unsuccessfully: %w. %s", name, err, strings.TrimSpace(stderr.String()))
			// A GUI may stay running past the initial response. Preserve later
			// failures in the local API log instead of silently discarding them.
			log.Printf("launch: %v", err)
		}
		done <- err
	}()
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return nil
	}
}

// windowsLaunchEnv keeps the full environment, including Windows PATH entries
// the wrappers rely on, and supplies the variables they read when the API was
// started without them: the distribution (WSL_DISTRO_NAME) and user (USER).
func (d Discovery) windowsLaunchEnv() []string {
	env := os.Environ()
	set := func(key, value string) {
		if value == "" {
			return
		}
		for _, entry := range env {
			if name, existing, ok := strings.Cut(entry, "="); ok && name == key && existing != "" {
				return
			}
		}
		env = append(env, key+"="+value)
	}
	set("WSL_DISTRO_NAME", d.Distro)
	if current, err := user.Current(); err == nil {
		set("USER", current.Username)
	}
	return env
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 4096 - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
