package ui

import (
	"strings"
	"testing"
)

func TestEverySectionHasOneScope(t *testing.T) {
	seen := map[string]bool{}
	for _, sec := range keymapSections {
		if _, ok := sectionScopes[sec.name]; !ok {
			t.Errorf("section %q has no key scope", sec.name)
		}
		if seen[sec.name] {
			t.Errorf("section %q listed twice", sec.name)
		}
		seen[sec.name] = true
	}
	for name := range sectionScopes {
		if !seen[name] {
			t.Errorf("scope table names unknown section %q", name)
		}
	}
	actions := map[string]int{}
	for _, sec := range keymapSections {
		for _, a := range sec.actions {
			actions[a]++
		}
	}
	for a := range defaultBindings {
		if actions[a] != 1 {
			t.Errorf("action %q is in %d sections, want 1", a, actions[a])
		}
	}
}

func TestDefaultBindingsHaveNoScopedConflicts(t *testing.T) {
	// An action's own default is always allowed, so check pairs directly.
	for _, a := range keymapSections {
		for _, an := range a.actions {
			sa := sectionScopes[a.name]
			for _, b := range keymapSections {
				for _, bn := range b.actions {
					if an == bn || !scopesOverlap(sa, sectionScopes[b.name]) {
						continue
					}
					for _, ka := range defaultBindings[an].keys {
						for _, kb := range defaultBindings[bn].keys {
							if ka == kb {
								t.Errorf("defaults %s and %s share %q in overlapping scopes", an, bn, ka)
							}
						}
					}
				}
			}
		}
	}
}

func TestValidateKeyBinding(t *testing.T) {
	tests := []struct {
		name      string
		action    string
		candidate string
		overrides map[string]string
		wantErr   string // substring, "" = allowed
	}{
		{"free key", "pull", "ctrl+y", nil, ""},
		{"same scope conflict", "pull", "ctrl+u", nil, "already used by 'push' (Git)"},
		{"cross section same scope", "pull", "e", nil, "already used by 'editor' (Worktree)"},
		{"global conflict from scoped", "pull", "ctrl+k", nil, "already used by 'commands' (Navigation)"},
		{"scoped conflict from global", "palette", "ctrl+p", nil, "already used by 'pull' (Git)"},
		{"cross scope reuse worktree/processes", "kill_proc", "ctrl+u", nil, ""},
		{"cross scope reuse agents/worktree", "agent_stop", "f", nil, ""},
		{"cross scope default style reuse", "agent_new", "n", nil, ""},
		{"processes vs agents", "kill_proc", "g", nil, ""},
		{"same scope processes", "kill_proc", "p", nil, "already used by 'restart policy' (Processes tab)"},
		{"fixed ctrl+c", "pull", "ctrl+c", nil, "reserved"},
		{"fixed ctrl+]", "pull", "ctrl+]", nil, "reserved"},
		{"fixed esc", "pull", "esc", nil, "reserved"},
		{"fixed enter", "pull", "enter", nil, "reserved"},
		{"fixed space", "pull", " ", nil, "reserved"},
		{"fixed arrow", "pull", "left", nil, "reserved"},
		{"workspace jump", "pull", "alt+3", nil, "workspaces"},
		{"alt+6 is free", "pull", "alt+6", nil, ""},
		{"global cannot take h", "palette", "h", nil, "navigation"},
		{"global cannot take j", "help", "j", nil, "navigation"},
		{"scoped may take l", "pull", "l", map[string]string{"log_tab": "L2"}, ""},
		{"scoped l conflicts with log tab", "pull", "l", nil, "'git log'"},
		{"override frees a key", "pull", "ctrl+u", map[string]string{"push": "ctrl+t"}, ""},
		{"override occupies a key", "pull", "ctrl+t", map[string]string{"push": "ctrl+t"}, "'push'"},
		{"quit second default key counts", "pull", "ctrl+c", nil, "reserved"},
		{"unknown action", "nope", "x", nil, "unknown"},
	}
	for _, tc := range tests {
		err := validateKeyBinding(tc.action, tc.candidate, tc.overrides)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: rejected: %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.wantErr)
		}
	}
}
