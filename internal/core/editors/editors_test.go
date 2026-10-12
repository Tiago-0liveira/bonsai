package editors

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func executable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoveryUsesNativeEnvironment(t *testing.T) {
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
	if len(got) != 2 || got[0].ID != "vscode" || got[1].ID != "zed" || got[1].Command != nativeZed {
		t.Fatalf("native editors = %+v", got)
	}
	// Even without the mount filter, Windows shims and PE binaries are foreign.
	d.WindowsMounts = nil
	got = d.Editors()
	if len(got) != 2 || got[1].Command != nativeZed {
		t.Fatalf("accepted Windows forwarding wrapper: %+v", got)
	}
	if !d.foreignPath("/mnt/c/Users/person/Zed/bin") || !d.foreignPath("/mnt/d/bin") || d.foreignPath("/mnt/code") {
		t.Fatal("default WSL mount filtering")
	}
	// Rescan after uninstall; do not fall back to the Windows shim.
	if err := os.Remove(nativeZed); err != nil {
		t.Fatal(err)
	}
	if got := d.Editors(); len(got) != 1 || got[0].ID != "vscode" {
		t.Fatalf("uninstalled editor still available: %+v", got)
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
	dir := t.TempDir()
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
