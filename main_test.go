package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

func TestNewTUIModelAgentsTab(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	size := tea.WindowSizeMsg{Width: 120, Height: 30}

	view := func(m tea.Model) string {
		m, _ = m.Update(size)
		return m.View()
	}

	api := fake.New().API()
	if got := view(newTUIModel(repo, &config.Config{}, &config.State{}, &api)); !strings.Contains(got, "Agents (g)") {
		t.Error("a backend should add the Agents tab")
	}
	if got := view(newTUIModel(repo, &config.Config{}, &config.State{}, nil)); strings.Contains(got, "Agents (g)") {
		t.Error("without a backend the tab stays hidden")
	}
}

func TestParseRootArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    rootArgs
		wantErr string
	}{
		{"bare", nil, rootArgs{layout: ui.LayoutLegacy}, ""},
		{"layout redesign", []string{"--tui-layout", "redesign"}, rootArgs{layout: ui.LayoutRedesign}, ""},
		{"layout equals", []string{"--tui-layout=legacy"}, rootArgs{layout: ui.LayoutLegacy}, ""},
		{"config then layout", []string{"--config", "a.yaml", "--tui-layout=redesign"}, rootArgs{cfgPath: "a.yaml", layout: ui.LayoutRedesign}, ""},
		{"layout then short config", []string{"--tui-layout", "redesign", "-c", "a.yaml"}, rootArgs{cfgPath: "a.yaml", layout: ui.LayoutRedesign}, ""},
		{"config equals then layout", []string{"--config=a.yaml", "--tui-layout", "redesign"}, rootArgs{cfgPath: "a.yaml", layout: ui.LayoutRedesign}, ""},
		{"config only", []string{"-c", "a.yaml"}, rootArgs{cfgPath: "a.yaml", layout: ui.LayoutLegacy}, ""},
		{"last layout wins", []string{"--tui-layout=redesign", "--tui-layout=legacy"}, rootArgs{layout: ui.LayoutLegacy}, ""},
		{"invalid layout", []string{"--tui-layout", "fancy"}, rootArgs{}, `invalid --tui-layout "fancy"`},
		{"invalid layout equals", []string{"--tui-layout="}, rootArgs{}, `invalid --tui-layout ""`},
		{"missing layout value", []string{"--tui-layout"}, rootArgs{}, "--tui-layout requires a value"},
		{"subcommand without flags", []string{"list", "--json"}, rootArgs{layout: ui.LayoutLegacy, rest: []string{"list", "--json"}}, ""},
		{"flag after subcommand is the subcommand's", []string{"ps", "--tui-layout", "redesign"}, rootArgs{layout: ui.LayoutLegacy, rest: []string{"ps", "--tui-layout", "redesign"}}, ""},
		{"value-less config stays for the CLI", []string{"--config"}, rootArgs{layout: ui.LayoutLegacy, rest: []string{"--config"}}, ""},
		{"duplicate config", []string{"-c", "a.yaml", "--config=b.yaml"}, rootArgs{}, "--config given more than once"},
		{"layout with subcommand", []string{"--tui-layout", "redesign", "list"}, rootArgs{}, "--tui-layout applies to the TUI only"},
		{"config with subcommand", []string{"-c", "a.yaml", "list"}, rootArgs{}, "--config applies to the TUI only"},
		{"empty config with subcommand", []string{"--config=", "list"}, rootArgs{}, "--config applies to the TUI only, not subcommands"},
	}
	for _, tc := range tests {
		got, err := parseRootArgs(tc.args)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if len(got.rest) == 0 {
			got.rest = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestNewTUIModelRedesignOption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newTUIModel(t.TempDir(), &config.Config{}, &config.State{}, nil, ui.WithLayout(ui.LayoutRedesign))
	if cmd := m.Init(); cmd != nil {
		t.Error("redesign layout must not start domain work")
	}
}

func TestGitSignals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []os.Signal
	}{
		{"TUI", nil, []os.Signal{syscall.SIGHUP}},
		{"create", []string{"create", "branch"}, []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
		{"update", []string{"--update"}, []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
		{"web", []string{"web", "--attach"}, nil},
		{"serve", []string{"serve"}, nil},
		{"agents", []string{"agents", "attach"}, nil},
		{"dev stack", []string{"__serve-dev-stack"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gitSignals(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("signals = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitSignalExitHelper(t *testing.T) {
	mode := os.Getenv("BONSAI_TEST_GIT_SIGNAL_HELPER")
	if mode == "" {
		return
	}
	args := []string{"create"}
	if mode == "tui" {
		args = nil
	}
	defer handleGitSignals(args)()
	_, _ = os.Stdout.WriteString("ready\n")
	select {}
}

func TestGitSignalExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	for _, tt := range []struct {
		mode string
		sig  syscall.Signal
		code int
	}{
		{"cli", syscall.SIGINT, 130},
		{"cli", syscall.SIGTERM, 143},
		{"cli", syscall.SIGHUP, 129},
		{"tui", syscall.SIGHUP, 129},
	} {
		t.Run(tt.mode+"/"+tt.sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGitSignalExitHelper$")
			cmd.Env = append(os.Environ(), "BONSAI_TEST_GIT_SIGNAL_HELPER="+tt.mode,
				"HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("helper readiness = %q, %v", line, err)
			}
			if err := cmd.Process.Signal(tt.sig); err != nil {
				t.Fatal(err)
			}
			var exit *exec.ExitError
			if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != tt.code {
				t.Fatalf("Wait = %v, want exit %d", err, tt.code)
			}
		})
	}
}
