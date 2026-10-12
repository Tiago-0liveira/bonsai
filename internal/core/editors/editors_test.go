package editors

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempDir is t.TempDir with symlinks resolved. macOS temp folders live under
// /var, a symlink to /private/var, which changes $PWD and path comparisons.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func executable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoveryKeepsNativeAndWindowsSeparate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixtures")
	}
	foreign, native := t.TempDir(), t.TempDir()
	executable(t, foreign, "zed", "#!/bin/sh\nexec zed.exe --wsl Ubuntu \"$@\"\n")
	nativeZed := executable(t, native, "zeditor", "#!/bin/sh\nexit 0\n")
	executable(t, foreign, "cursor", "MZfake Windows executable")
	executable(t, native, "code", "#!/bin/sh\nexit 0\n")
	if err := os.Symlink(filepath.Join(foreign, "zed"), filepath.Join(native, "zed")); err != nil {
		t.Fatal(err)
	}
	d := Discovery{OS: "linux", WSL: true, Path: strings.Join([]string{foreign, ".", native}, string(os.PathListSeparator)), WindowsMounts: []string{foreign}}
	got := d.Editors()
	// The wrapper does not mention WSL_DISTRO_NAME and cursor is a PE binary, so
	// only native commands are offered; the symlinked Windows zed is not native.
	if len(got) != 2 || got[0].ID != "vscode" || got[1].ID != "zed" || got[1].Command != nativeZed || got[1].windows {
		t.Fatalf("native editors = %+v", got)
	}
	if !d.foreignPath("/mnt/c/Users/person/Zed/bin") || !d.foreignPath("/mnt/d/bin") || d.foreignPath("/mnt/code") {
		t.Fatal("default WSL mount filtering")
	}
	// Rescan after uninstall.
	if err := os.Remove(nativeZed); err != nil {
		t.Fatal(err)
	}
	if got := d.Editors(); len(got) != 1 || got[0].ID != "vscode" {
		t.Fatalf("uninstalled editor still available: %+v", got)
	}
}

func TestWindowsEditorsOnWSL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixtures")
	}
	const wrapper = "#!/usr/bin/env sh\nif [ -n \"$WSL_DISTRO_NAME\" ]; then exec \"$0.exe\" --wsl \"$@\"; fi\n"
	mount, native := tempDir(t), tempDir(t)
	programs := filepath.Join(mount, "Users", "me", "AppData", "Local", "Programs")
	mkdir := func(parts ...string) string {
		dir := filepath.Join(parts...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	// Zed is found through the Windows PATH, VS Code only through its install
	// folder, and Cursor's .exe-only install has no WSL wrapper to use.
	zedBin := mkdir(programs, "Zed", "bin")
	zed := executable(t, zedBin, "zed", wrapper)
	code := executable(t, mkdir(programs, "Microsoft VS Code", "bin"), "code", wrapper)
	executable(t, mkdir(programs, "cursor", "resources", "app", "bin"), "cursor", "MZfake Windows executable")
	// A native editor with the same command keeps its own ID.
	nativeCode := executable(t, native, "code", "#!/bin/sh\nexit 0\n")
	// Wrappers that never mention WSL cannot convert paths, so they are skipped.
	executable(t, mkdir(programs, "Microsoft VS Code Insiders", "bin"), "code-insiders", "#!/bin/sh\nexec Code.exe \"$@\"\n")

	d := Discovery{OS: "linux", WSL: true, Distro: "Ubuntu", Path: strings.Join([]string{zedBin, native}, string(os.PathListSeparator)), WindowsMounts: []string{mount}}
	got := d.Editors()
	want := []struct{ id, command string }{{"vscode", nativeCode}, {"vscode-windows", code}, {"zed-windows", zed}}
	if len(got) != len(want) {
		t.Fatalf("editors = %+v", got)
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Command != w.command || got[i].windows != strings.HasSuffix(w.id, "-windows") {
			t.Fatalf("editor %d = %+v, want %+v", i, got[i], w)
		}
	}
	if !strings.HasSuffix(got[1].Name, "(Windows)") {
		t.Fatalf("Windows editor not labelled: %q", got[1].Name)
	}
	// Outside WSL nothing from a Windows drive is considered.
	d.WSL = false
	for _, editor := range d.Editors() {
		if editor.windows {
			t.Fatalf("Windows editor offered outside WSL: %+v", editor)
		}
	}
}

func TestWindowsWrapperRejectsOutsideAndNonScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixtures")
	}
	mount, other := tempDir(t), tempDir(t)
	d := Discovery{OS: "linux", WSL: true, WindowsMounts: []string{mount}}
	inside := executable(t, mount, "zed", "#!/bin/sh\n# WSL_DISTRO_NAME\n")
	if !d.windowsWrapper(inside) {
		t.Fatal("WSL-aware wrapper rejected")
	}
	if d.windowsWrapper(executable(t, other, "zed", "#!/bin/sh\n# WSL_DISTRO_NAME\n")) {
		t.Fatal("script outside a Windows drive accepted")
	}
	if err := os.Symlink(filepath.Join(other, "zed"), filepath.Join(mount, "linked")); err != nil {
		t.Fatal(err)
	}
	if d.windowsWrapper(filepath.Join(mount, "linked")) {
		t.Fatal("symlink escaping the Windows drive accepted")
	}
	if d.windowsWrapper(executable(t, mount, "binary", "MZ WSL_DISTRO_NAME")) {
		t.Fatal("PE binary accepted")
	}
	if err := os.WriteFile(filepath.Join(mount, "plain"), []byte("#!/bin/sh\n# WSL_DISTRO_NAME\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if d.windowsWrapper(filepath.Join(mount, "plain")) {
		t.Fatal("non-executable file accepted")
	}
}

func TestWindowsLaunchKeepsWindowsPathAndWSLVariables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "env")
	t.Setenv("BONSAI_EDITOR_TEST_OUTPUT", out)
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("USER", "")
	t.Setenv("PATH", "/mnt/c/Windows:"+os.Getenv("PATH"))
	command := executable(t, dir, "zed", "#!/bin/sh\nprintf '%s\\n' \"$WSL_DISTRO_NAME\" \"$USER\" \"$PATH\" \"$@\" > \"$BONSAI_EDITOR_TEST_OUTPUT\"\n")
	d := Discovery{OS: "linux", WSL: true, Distro: "Debian", Path: "/mnt/c/Windows"}
	file := filepath.Join(dir, "main.go")
	if err := d.Launch(Editor{ID: "zed-windows", Name: "Zed", Command: command, windows: true}, dir, file); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if err != nil || len(lines) != 4 || lines[0] != "Debian" || lines[1] == "" || !strings.HasPrefix(lines[2], "/mnt/c/Windows:") || lines[3] != file {
		t.Fatalf("environment = %q (%v)", raw, err)
	}
}

func TestWindowsMounts(t *testing.T) {
	got := windowsMounts("1 2 0:0 / /windows\\040drive rw - 9p C: rw,aname=drvfs;path=C:\\\n3 4 0:1 / /other rw - drvfs D: rw\n5 6 0:2 / /native rw - ext4 /dev/sda rw\n")
	if len(got) != 2 || got[0] != "/windows drive" || got[1] != "/other" {
		t.Fatalf("mounts = %q", got)
	}
}

func TestLaunchArgumentsAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	dir := tempDir(t)
	out := filepath.Join(dir, "arguments")
	t.Setenv("BONSAI_EDITOR_TEST_OUTPUT", out)
	command := executable(t, dir, "zed", "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" \"$PATH\" > \"$BONSAI_EDITOR_TEST_OUTPUT\"\n")
	d := Discovery{OS: runtime.GOOS, Path: strings.Join([]string{"/mnt/c/Windows", ".", dir}, string(os.PathListSeparator)), WSL: true}
	file := filepath.Join(dir, "file with spaces $(touch sentinel).ts")
	if err := d.Launch(Editor{ID: "zed", Name: "Zed", Command: command, args: []string{"--goto"}}, dir, file); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != dir+"\n--goto\n"+file+"\n"+dir+"\n" {
		t.Fatalf("argv/cwd = %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sentinel")); !os.IsNotExist(err) {
		t.Fatal("filename was evaluated by a shell")
	}
	executable(t, dir, "zed", "#!/bin/sh\necho 'cannot connect to display' >&2\nexit 1\n")
	if err := d.Launch(Editor{Name: "Zed", Command: command}, dir, file); err == nil || !strings.Contains(err.Error(), "cannot connect to display") {
		t.Fatalf("launch failure = %v", err)
	}
	if err := d.Launch(Editor{Name: "Zed", Command: filepath.Join(dir, "missing")}, dir, file); err == nil {
		t.Fatal("missing command accepted")
	}
}

func tree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func ids(editors []Editor) string {
	out := make([]string, len(editors))
	for i, editor := range editors {
		out[i] = editor.ID
	}
	return strings.Join(out, ",")
}

func TestWindowsDiscovery(t *testing.T) {
	root := t.TempDir()
	programs, files := filepath.Join(root, "Local", "Programs"), filepath.Join(root, "Program Files")
	tree(t, root, map[string]string{
		// VS Code: only a .cmd shim is on PATH; Code.exe is the real launcher.
		"Local/Programs/Microsoft VS Code/Code.exe":     "MZ",
		"Local/Programs/Microsoft VS Code/bin/code.cmd": "@echo off",
		// Cursor installed machine-wide, shim on PATH.
		"Program Files/cursor/Cursor.exe":                   "MZ",
		"Program Files/cursor/resources/app/bin/cursor.cmd": "@echo off",
		// Zed is not on PATH and is found in the standard install folder.
		"Local/Programs/Zed/bin/zed.exe": "MZ",
		// Sublime ships a real subl.exe on PATH.
		"Sublime/subl.exe": "MZ",
		// A shim without its executable must not be reported.
		"Local/Programs/Microsoft VS Code Insiders/bin/code-insiders.cmd": "@echo off",
	})
	path := strings.Join([]string{
		filepath.Join(programs, "Microsoft VS Code", "bin"),
		filepath.Join(files, "cursor", "resources", "app", "bin"),
		filepath.Join(programs, "Microsoft VS Code Insiders", "bin"),
		filepath.Join(root, "Sublime"),
	}, string(os.PathListSeparator))
	d := Discovery{OS: "windows", Path: path, AppRoots: []string{programs, files}}
	got := d.Editors()
	if ids(got) != "vscode,cursor,zed,sublime" {
		t.Fatalf("editors = %+v", got)
	}
	if got[0].Command != filepath.Join(programs, "Microsoft VS Code", "Code.exe") || !got[0].Folders {
		t.Fatalf("vscode = %+v", got[0])
	}
	if got[1].Command != filepath.Join(files, "cursor", "Cursor.exe") {
		t.Fatalf("cursor = %+v", got[1])
	}
	if got[2].Command != filepath.Join(programs, "Zed", "bin", "zed.exe") {
		t.Fatalf("zed = %+v", got[2])
	}
}

func TestDarwinDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable bits")
	}
	root := t.TempDir()
	tree(t, root, map[string]string{
		"Applications/Visual Studio Code.app/Contents/Resources/app/bin/code": "#!/usr/bin/env bash\nexit 0\n",
		"Applications/Zed.app/Contents/MacOS/cli":                             "\xcf\xfa\xed\xfemachO",
		"Applications/Cursor.app/Contents/Resources/app/bin/cursor":           "#!/usr/bin/env bash\nexit 0\n",
		"brew/subl": "#!/bin/sh\nexit 0\n",
	})
	d := Discovery{OS: "darwin", ExtraDirs: []string{filepath.Join(root, "brew")}, AppRoots: []string{filepath.Join(root, "Applications")}}
	got := d.Editors()
	if ids(got) != "vscode,cursor,zed,sublime" {
		t.Fatalf("editors = %+v", got)
	}
}

func TestLinuxExtraDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable bits")
	}
	root := t.TempDir()
	tree(t, root, map[string]string{
		"home/.local/bin/zed": "\x7fELFbinary",
		"share/code/bin/code": "#!/bin/sh\nexit 0\n",
		"path/kate":           "#!/bin/sh\nexit 0\n",
	})
	d := Discovery{OS: "linux", Path: filepath.Join(root, "path"), ExtraDirs: []string{filepath.Join(root, "home", ".local", "bin")}, AppRoots: []string{filepath.Join(root, "share")}}
	got := d.Editors()
	if ids(got) != "vscode,zed,kate" {
		t.Fatalf("editors = %+v", got)
	}
	for _, editor := range got {
		if editor.ID == "kate" && editor.Folders {
			t.Fatal("kate cannot open folders")
		}
	}
}

func TestDefaultSearchDirs(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `/win/Local`, "ProgramFiles": `/win/PF`}
	_, apps := defaultSearchDirs("windows", func(k string) string { return env[k] }, "/home/u")
	if len(apps) != 2 || apps[0] != filepath.Join("/win/Local", "Programs") || apps[1] != "/win/PF" {
		t.Fatalf("windows roots = %q", apps)
	}
	extra, apps := defaultSearchDirs("darwin", func(string) string { return "" }, "/Users/u")
	if len(apps) != 2 || apps[0] != "/Applications" || apps[1] != filepath.Join("/Users/u", "Applications") || extra[0] != "/opt/homebrew/bin" {
		t.Fatalf("darwin dirs = %q %q", extra, apps)
	}
	extra, _ = defaultSearchDirs("linux", func(string) string { return "" }, "")
	if len(extra) != 2 || extra[0] != "/usr/local/bin" {
		t.Fatalf("linux extra without HOME = %q", extra)
	}
}

func TestLaunchFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "arguments")
	t.Setenv("BONSAI_EDITOR_TEST_OUTPUT", out)
	command := executable(t, dir, "code", "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$BONSAI_EDITOR_TEST_OUTPUT\"\n")
	d := Discovery{OS: runtime.GOOS}
	if err := d.LaunchFolder(Editor{Name: "Kate", Command: command}, dir); !errors.Is(err, ErrFolderUnsupported) {
		t.Fatalf("folder-less editor = %v", err)
	}
	// --goto only makes sense for files; a folder is passed bare.
	if err := d.LaunchFolder(Editor{Name: "Code", Command: command, Folders: true, args: []string{"--goto"}}, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != dir+"\n" {
		t.Fatalf("argv = %q (%v)", got, err)
	}
}
