package ui

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func TestAliasCommandUserShadowsConfig(t *testing.T) {
	m := testModel()
	m.cfg = &config.Config{
		Aliases: []config.Alias{
			{Name: "build", Command: "make from-config"},
			{Name: "only-config", Command: "cfg-cmd"},
		},
	}
	m.state = &config.State{
		Aliases: []config.Alias{
			{Name: "build", Command: "make from-user"},
			{Name: "only-user", Command: "user-cmd"},
		},
	}

	if got := m.aliasCommand("build"); got != "make from-user" {
		t.Errorf("shadowing: aliasCommand(build) = %q, want user command", got)
	}
	if got := m.aliasCommand("only-config"); got != "cfg-cmd" {
		t.Errorf("config-only: aliasCommand = %q, want cfg-cmd", got)
	}
	if got := m.aliasCommand("only-user"); got != "user-cmd" {
		t.Errorf("state-only: aliasCommand = %q, want user-cmd", got)
	}
	if got := m.aliasCommand("missing"); got != "" {
		t.Errorf("not-found: aliasCommand = %q, want empty", got)
	}
}
