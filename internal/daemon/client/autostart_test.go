package client

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveDaemonBin(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(t.TempDir(), "bonsai")
	for _, tt := range []struct {
		name string
		bin  string
		want string
	}{
		{name: "absolute", bin: abs, want: abs},
		{name: "relative", bin: "." + string(filepath.Separator) + "x", want: filepath.Join(cwd, "x")},
		{name: "PATH", bin: "bonsai", want: "bonsai"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDaemonBin(tt.bin)
			if err != nil || got != tt.want {
				t.Fatalf("resolveDaemonBin(%q) = %q, %v; want %q", tt.bin, got, err, tt.want)
			}
		})
	}
}

func TestDaemonCommand(t *testing.T) {
	for _, tt := range []struct {
		name     string
		userHome bool
		flag     string
	}{
		{name: "repo", flag: "--repo"},
		{name: "home", userHome: true, flag: "--home"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(t.TempDir(), "bonsai")
			cmd := daemonCommand(bin, tt.userHome, root)
			if cmd.Dir != root {
				t.Fatalf("Dir = %q, want %q", cmd.Dir, root)
			}
			wantArgs := []string{bin, "__daemon", tt.flag, root}
			if !reflect.DeepEqual(cmd.Args, wantArgs) {
				t.Fatalf("Args = %q, want %q", cmd.Args, wantArgs)
			}
		})
	}
}
