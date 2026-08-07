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
