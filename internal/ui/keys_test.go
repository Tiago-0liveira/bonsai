package ui

import "testing"

func TestNewKeyMapDefaults(t *testing.T) {
	k := newKeyMap(nil)
	if got := k.Create.Keys(); len(got) != 1 || got[0] != "n" {
		t.Errorf("default new_worktree keys = %v", got)
	}
	if k.Create.Help().Desc != "new worktree" {
		t.Errorf("default help desc = %q", k.Create.Help().Desc)
	}
}

func TestNewKeyMapOverride(t *testing.T) {
	k := newKeyMap(map[string]string{"new_worktree": "N"})
	if got := k.Create.Keys(); len(got) != 1 || got[0] != "N" {
		t.Errorf("overridden keys = %v", got)
	}
	// Help description is preserved, help key reflects the override.
	if k.Create.Help().Key != "N" || k.Create.Help().Desc != "new worktree" {
		t.Errorf("help after override = %+v", k.Create.Help())
	}
}

func TestPaletteBinding(t *testing.T) {
	k := newKeyMap(nil)
	if got := k.Palette.Keys(); len(got) != 1 || got[0] != "ctrl+k" {
		t.Errorf("default palette keys = %v", got)
	}
	k = newKeyMap(map[string]string{"palette": ":"})
	if got := k.Palette.Keys(); len(got) != 1 || got[0] != ":" {
		t.Errorf("overridden palette keys = %v", got)
	}
	if k.Palette.Help().Desc != "commands" {
		t.Errorf("palette help desc = %q", k.Palette.Help().Desc)
	}
}

func TestKeyCollisionsNoneByDefault(t *testing.T) {
	if got := keyCollisions(nil); got != nil {
		t.Errorf("expected no collisions for defaults, got %v", got)
	}
}

func TestKeyCollisionsDetected(t *testing.T) {
	// Remap new_worktree onto prune's key.
	got := keyCollisions(map[string]string{"new_worktree": "x"})
	if len(got) == 0 {
		t.Fatalf("expected a collision for new_worktree=x")
	}
}

func TestPrefsBinding(t *testing.T) {
	k := newKeyMap(nil)
	if got := k.Prefs.Keys(); len(got) != 1 || got[0] != "," {
		t.Errorf("default prefs keys = %v", got)
	}
}

func TestKeymapSectionsCoverEveryAction(t *testing.T) {
	seen := map[string]int{}
	for _, sec := range keymapSections {
		for _, a := range sec.actions {
			seen[a]++
		}
	}
	for action := range defaultBindings {
		if seen[action] != 1 {
			t.Errorf("action %q appears %d times in keymapSections, want 1", action, seen[action])
		}
	}
	if len(seen) != len(defaultBindings) {
		t.Errorf("keymapSections covers %d actions, defaultBindings has %d", len(seen), len(defaultBindings))
	}
}

func TestEffectiveKey(t *testing.T) {
	if got := effectiveKey("prune", nil); got != "x" {
		t.Errorf("effectiveKey default = %q", got)
	}
	if got := effectiveKey("prune", map[string]string{"prune": "z"}); got != "z" {
		t.Errorf("effectiveKey override = %q", got)
	}
}
