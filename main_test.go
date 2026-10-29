package main

import (
	"reflect"
	"strings"
	"testing"

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
