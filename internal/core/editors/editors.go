// Package editors discovers GUI editor CLIs in the environment running Bonsai.
// It never searches another operating system's installation directories.
package editors

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	args    []string
}

type Discovery struct {
	Environment Environment
	OS          string
	Path        string
	WSL         bool
	// WindowsMounts includes nonstandard WSL drive mount locations.
	WindowsMounts []string
}

func Current() Discovery {
	host, _ := os.Hostname()
	d := Discovery{OS: runtime.GOOS, Path: os.Getenv("PATH")}
	d.Environment = Environment{ID: runtime.GOOS + ":" + host, Label: map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}[runtime.GOOS]}
	if d.Environment.Label == "" {
		d.Environment.Label = runtime.GOOS
	}
	if runtime.GOOS == "linux" {
		distro := os.Getenv("WSL_DISTRO_NAME")
		kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
		if distro != "" || strings.Contains(strings.ToLower(string(kernel)), "microsoft") {
			d.WSL = true
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
	for _, dir := range filepath.SplitList(d.Path) {
		// Skip inherited host paths and current/project-relative search paths.
		if !filepath.IsAbs(dir) || d.foreignPath(dir) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil && !d.foreignPath(resolved) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func (d Discovery) Editors() []Editor {
	candidates := []struct {
		id, name string
		commands []string
		args     []string
	}{
		{"vscode", "Visual Studio Code", []string{"code"}, []string{"--goto"}},
		{"vscode-insiders", "Visual Studio Code Insiders", []string{"code-insiders"}, []string{"--goto"}},
		{"cursor", "Cursor", []string{"cursor"}, []string{"--goto"}},
		{"zed", "Zed", []string{"zed", "zeditor"}, nil},
		{"sublime", "Sublime Text", []string{"subl"}, nil},
		{"kate", "Kate", []string{"kate"}, nil},
		{"gedit", "Gedit", []string{"gedit"}, nil},
		{"gnome-text-editor", "GNOME Text Editor", []string{"gnome-text-editor"}, nil},
	}
	result := []Editor{}
	dirs := d.nativeDirectories()
	for _, candidate := range candidates {
		found := ""
		for _, command := range candidate.commands {
			for _, dir := range dirs {
				name := command
				if d.OS == "windows" {
					name += ".exe"
				}
				path := filepath.Join(dir, name)
				if d.nativeCommand(path) {
					found = path
					break
				}
			}
			if found != "" {
				break
			}
		}
		if found != "" {
			result = append(result, Editor{ID: candidate.id, Name: candidate.name, Command: found, args: candidate.args})
		}
	}
	return result
}

// Launch uses an argument vector, never a shell or a system file handler. The
// editor outlives the request; Wait reaps it even if its GUI stays open.
func (d Discovery) Launch(editor Editor, directory, file string) error {
	args := append(append([]string{}, editor.args...), file)
	cmd := exec.Command(editor.Command, args...)
	cmd.Dir = directory
	// Native launcher scripts must also resolve helpers in this environment.
	cmd.Env = []string{}
	for _, value := range os.Environ() {
		if key, _, ok := strings.Cut(value, "="); ok && !strings.EqualFold(key, "PATH") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+strings.Join(d.nativeDirectories(), string(os.PathListSeparator)))
	var stderr limitedOutput
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot launch %s: %w", editor.Name, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("%s exited unsuccessfully: %w. %s", editor.Name, err, strings.TrimSpace(stderr.String()))
			// A GUI may stay running past the initial response. Preserve later
			// failures in the local API log instead of silently discarding them.
			log.Printf("editor launch: %v", err)
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
