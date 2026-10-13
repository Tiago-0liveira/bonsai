package editors

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestFileManagerLabels(t *testing.T) {
	for _, test := range []struct {
		d    Discovery
		want string
	}{
		{Discovery{OS: "windows"}, "Explorer"},
		{Discovery{OS: "linux", WSL: true}, "Explorer"},
		{Discovery{OS: "darwin"}, "Finder"},
		{Discovery{OS: "linux"}, "file manager"},
	} {
		if got := test.d.FileManager().Label; got != test.want {
			t.Fatalf("%+v: got %q, want %q", test.d, got, test.want)
		}
	}
}

func TestRevealCommandsPerEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	bin := tempDir(t)
	xdg := executable(t, bin, "xdg-open", "#!/bin/sh\nexit 0\n")
	gio := executable(t, bin, "gio", "#!/bin/sh\nexit 0\n")
	open := executable(t, bin, "open", "#!/bin/sh\nexit 0\n")
	folder := "/home/me/repo/wt"

	linux, err := Discovery{OS: "linux", Path: bin}.revealCommands(folder)
	if err != nil {
		t.Fatal(err)
	}
	if want := []revealCommand{{path: xdg, args: []string{folder}, dir: folder}, {path: gio, args: []string{"open", folder}, dir: folder}}; !reflect.DeepEqual(linux, want) {
		t.Fatalf("linux: %+v", linux)
	}
	darwin, err := Discovery{OS: "darwin", Path: bin}.revealCommands(folder)
	if err != nil || len(darwin) != 1 || darwin[0].path != open || !reflect.DeepEqual(darwin[0].args, []string{folder}) {
		t.Fatalf("darwin: %+v (%v)", darwin, err)
	}
	windows, err := Discovery{OS: "windows"}.revealCommands(`C:\repo\wt`)
	if err != nil || !reflect.DeepEqual(windows, []revealCommand{{path: "explorer.exe", args: []string{`C:\repo\wt`}, dir: `C:\repo\wt`, anyExit: true}}) {
		t.Fatalf("windows: %+v (%v)", windows, err)
	}
	if _, err := (Discovery{OS: "linux", Path: tempDir(t)}).revealCommands(folder); !errors.Is(err, ErrNoFileManager) {
		t.Fatalf("missing xdg-open: %v", err)
	}
}

func TestRevealOnWSLOpensWindowsExplorer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	drive := tempDir(t)
	if err := os.Mkdir(filepath.Join(drive, "Windows"), 0755); err != nil {
		t.Fatal(err)
	}
	explorer := executable(t, filepath.Join(drive, "Windows"), "explorer.exe", "#!/bin/sh\nexit 1\n")
	failing := func(string) (string, error) { return "", errors.New("wslpath unavailable") }
	d := Discovery{OS: "linux", WSL: true, Distro: "Ubuntu", WindowsMounts: []string{drive}, convertPath: failing}

	commands, err := d.revealCommands("/home/me/repo/wt")
	if err != nil {
		t.Fatal(err)
	}
	want := []revealCommand{{path: explorer, args: []string{`\\wsl.localhost\Ubuntu\home\me\repo\wt`}, dir: drive, windows: true, anyExit: true}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("got %+v, want %+v", commands, want)
	}
	// explorer.exe exits 1 after handing the folder to the Windows shell.
	if err := d.RevealFolder("/home/me/repo/wt"); err != nil {
		t.Fatalf("explorer exit status treated as failure: %v", err)
	}

	d.convertPath = func(path string) (string, error) {
		return `\\wsl.localhost\Ubuntu` + strings.ReplaceAll(path, "/", `\`), nil
	}
	if commands, _ := d.revealCommands("/srv/a b"); commands[0].args[0] != `\\wsl.localhost\Ubuntu\srv\a b` {
		t.Fatalf("wslpath result ignored: %+v", commands)
	}
	if _, err := (Discovery{OS: "linux", WSL: true, WindowsMounts: []string{tempDir(t)}}).revealCommands("/home/me"); !errors.Is(err, ErrNoFileManager) {
		t.Fatalf("missing explorer.exe: %v", err)
	}
}

func TestWindowsPathFallbacks(t *testing.T) {
	failing := func(string) (string, error) { return "", errors.New("no wslpath") }
	d := Discovery{WSL: true, Distro: "Debian", convertPath: failing}
	for path, want := range map[string]string{
		"/mnt/c/Users/me/repo": `C:\Users\me\repo`,
		"/mnt/d":               `D:\`,
		"/home/me/repo":        `\\wsl.localhost\Debian\home\me\repo`,
		"/mnt/data/repo":       `\\wsl.localhost\Debian\mnt\data\repo`,
	} {
		if got, err := d.windowsPath(path); err != nil || got != want {
			t.Fatalf("%s: got %q (%v), want %q", path, got, err, want)
		}
	}
	if _, err := (Discovery{WSL: true, convertPath: failing}).windowsPath("/home/me"); err == nil {
		t.Fatal("converted a Linux path without a distribution name")
	}
}

func TestRevealFallsBackAndReportsFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix script fixture")
	}
	bin := tempDir(t)
	output := filepath.Join(tempDir(t), "args")
	executable(t, bin, "xdg-open", "#!/bin/sh\necho 'no method available' >&2\nexit 3\n")
	executable(t, bin, "gio", "#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+output+"\"\n")
	folder := tempDir(t)
	if err := (Discovery{OS: "linux", Path: bin}).RevealFolder(folder); err != nil {
		t.Fatal(err)
	}
	if args, err := os.ReadFile(output); err != nil || string(args) != "open\n"+folder+"\n" {
		t.Fatalf("gio fallback: %q (%v)", args, err)
	}
	if err := os.Remove(filepath.Join(bin, "gio")); err != nil {
		t.Fatal(err)
	}
	if err := (Discovery{OS: "linux", Path: bin}).RevealFolder(folder); err == nil || !strings.Contains(err.Error(), "no method available") {
		t.Fatalf("failure not surfaced: %v", err)
	}
}
