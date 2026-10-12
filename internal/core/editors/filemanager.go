package editors

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileManager is the system file browser that reveals a folder: Explorer on
// Windows and WSL, Finder on macOS, the desktop's default handler on Linux.
type FileManager struct {
	Label string `json:"label"`
}

func (d Discovery) FileManager() FileManager {
	switch {
	case d.OS == "windows" || d.WSL:
		return FileManager{Label: "Explorer"}
	case d.OS == "darwin":
		return FileManager{Label: "Finder"}
	default:
		return FileManager{Label: "file manager"}
	}
}

// ErrNoFileManager is returned when no file manager command can be found.
var ErrNoFileManager = errors.New("no file manager was found")

type revealCommand struct {
	path string
	args []string
	dir  string
	// windows runs with the Windows interop environment (WSL only).
	windows bool
	// anyExit treats every exit status as success. explorer.exe hands the
	// folder to the running shell and exits 1 even when the window opened.
	anyExit bool
}

// RevealFolder opens directory in the file manager. Commands use an argument
// vector, never a shell; later candidates run only when an earlier one fails.
func (d Discovery) RevealFolder(directory string) error {
	commands, err := d.revealCommands(directory)
	if err != nil {
		return err
	}
	var failures []string
	for _, command := range commands {
		cmd := exec.Command(command.path, command.args...)
		cmd.Dir = command.dir
		if command.windows {
			cmd.Env = d.windowsLaunchEnv()
		} else {
			cmd.Env = d.nativeEnv()
		}
		err := waitBriefly(cmd, filepath.Base(command.path), command.anyExit)
		if err == nil {
			return nil
		}
		failures = append(failures, err.Error())
	}
	return fmt.Errorf("cannot open the %s: %s", d.FileManager().Label, strings.Join(failures, "; "))
}

func (d Discovery) revealCommands(directory string) ([]revealCommand, error) {
	switch {
	case d.OS == "windows":
		return []revealCommand{{path: "explorer.exe", args: []string{directory}, dir: directory, anyExit: true}}, nil
	case d.WSL:
		explorer := d.windowsExplorer()
		if explorer == "" {
			return nil, fmt.Errorf("%w: explorer.exe is not reachable from WSL; check that Windows interop is enabled", ErrNoFileManager)
		}
		target, err := d.windowsPath(directory)
		if err != nil {
			return nil, err
		}
		// Run from the Windows drive: a WSL working directory becomes a UNC path.
		return []revealCommand{{path: explorer, args: []string{target}, dir: filepath.Dir(filepath.Dir(explorer)), windows: true, anyExit: true}}, nil
	case d.OS == "darwin":
		return d.nativeCandidates(directory, "open")
	default:
		return d.nativeCandidates(directory, "xdg-open", "gio")
	}
}

func (d Discovery) nativeCandidates(directory string, names ...string) ([]revealCommand, error) {
	var commands []revealCommand
	for _, name := range names {
		path := d.nativeLookup(name)
		if path == "" {
			continue
		}
		args := []string{directory}
		if name == "gio" {
			args = []string{"open", directory}
		}
		commands = append(commands, revealCommand{path: path, args: args, dir: directory})
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("%w: install xdg-utils or set a default file manager", ErrNoFileManager)
	}
	return commands, nil
}

// nativeLookup finds a system tool such as xdg-open. Unlike editor discovery
// it accepts any local executable: these are trusted OS utilities, often
// shell scripts, that never forward to Windows.
func (d Discovery) nativeLookup(name string) string {
	for _, dir := range d.nativeDirectories() {
		path := filepath.Join(dir, name)
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || d.foreignPath(resolved) {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

// windowsExplorer finds explorer.exe on a Windows drive, then on the Windows PATH.
func (d Discovery) windowsExplorer() string {
	mounts := d.WindowsMounts
	if len(mounts) == 0 {
		mounts, _ = filepath.Glob("/mnt/[a-z]")
	}
	for _, mount := range mounts {
		for _, name := range []string{"Windows", "WINDOWS"} {
			path := filepath.Join(mount, name, "explorer.exe")
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path
			}
		}
	}
	for _, dir := range filepath.SplitList(d.Path) {
		if !filepath.IsAbs(dir) || !d.foreignPath(dir) {
			continue
		}
		path := filepath.Join(dir, "explorer.exe")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// windowsPath converts a WSL path for Windows: wslpath when available,
// otherwise a /mnt/<drive> path becomes <DRIVE>:\ and any other path is
// reached through the distribution's \\wsl.localhost share.
func (d Discovery) windowsPath(path string) (string, error) {
	convert := d.convertPath
	if convert == nil {
		convert = wslpath
	}
	if converted, err := convert(path); err == nil && converted != "" {
		return converted, nil
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(clean, "/mnt/") && len(clean) >= 6 && clean[5] >= 'a' && clean[5] <= 'z' && (len(clean) == 6 || clean[6] == '/') {
		rest := strings.TrimPrefix(clean[6:], "/")
		return strings.ToUpper(clean[5:6]) + `:\` + strings.ReplaceAll(rest, "/", `\`), nil
	}
	if d.Distro == "" {
		return "", fmt.Errorf("cannot convert %s for Windows: wslpath failed and WSL_DISTRO_NAME is unset", path)
	}
	return `\\wsl.localhost\` + d.Distro + strings.ReplaceAll(clean, "/", `\`), nil
}

func wslpath(path string) (string, error) {
	output, err := exec.Command("wslpath", "-w", path).Output()
	return strings.TrimSpace(string(output)), err
}
