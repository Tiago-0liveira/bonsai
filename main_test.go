package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
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
