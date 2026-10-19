package ui

import "fmt"

// keyScope is where an action's key is live. Two actions can only collide when
// they can be active together: same scope, or either one global.
type keyScope int

const (
	ksGlobal    keyScope = iota // always live, including the shell's own keys
	ksWorktree                  // the worktree list and its tabs
	ksAgents                    // the Agents tab
	ksProcesses                 // the Processes tab
)

// sectionScopes maps every keymapSections name to its scope. A section missing
// here fails TestEverySectionHasOneScope, so new sections cannot silently skip
// the conflict check.
var sectionScopes = map[string]keyScope{
	"Navigation":        ksGlobal,
	"Worktree":          ksWorktree,
	"Tabs":              ksWorktree,
	"Files & clipboard": ksWorktree,
	"Run":               ksWorktree,
	"Git":               ksWorktree,
	"Agents tab":        ksAgents,
	"Processes tab":     ksProcesses,
}

// fixedKeys can never be bound: they drive modals, quit and the terminal
// escape. They extend the legacy preferences list with ctrl+].
var fixedKeys = map[string]bool{
	"esc": true, "ctrl+c": true, "ctrl+]": true, "enter": true, " ": true,
	"tab": true, "shift+tab": true,
	"up": true, "down": true, "left": true, "right": true,
}

// shellNavKeys are the letters Settings and the workspace tabs read. A global
// action is matched before them, so binding one would shadow that navigation;
// scoped actions only act inside their own tab and may use these letters (the
// default log_tab already uses l).
var shellNavKeys = map[string]bool{"h": true, "j": true, "k": true, "l": true}

// scopeOfAction finds the scope and section of an action.
func scopeOfAction(action string) (keyScope, string, bool) {
	for _, sec := range keymapSections {
		for _, a := range sec.actions {
			if a == action {
				s, ok := sectionScopes[sec.name]
				return s, sec.name, ok
			}
		}
	}
	return 0, "", false
}

func scopesOverlap(a, b keyScope) bool { return a == b || a == ksGlobal || b == ksGlobal }

// keysOf lists every key an action answers to under overrides: an override
// replaces the defaults, as binding() does.
func keysOf(action string, overrides map[string]string) []string {
	if ov, ok := overrides[action]; ok && ov != "" {
		return []string{ov}
	}
	return defaultBindings[action].keys
}

// validateKeyBinding says why candidate cannot be bound to action, or nil.
// overrides is the merged view (repo plus personal) without the candidate; the
// caller handles "the action's own default" before asking.
func validateKeyBinding(action, candidate string, overrides map[string]string) error {
	for i := workspace(0); i < workspaceCount; i++ {
		if candidate == fmt.Sprintf("alt+%d", i+1) {
			return fmt.Errorf("%s jumps between workspaces", candidate)
		}
	}
	if fixedKeys[candidate] {
		return fmt.Errorf("%s is reserved", candidate)
	}
	scope, _, ok := scopeOfAction(action)
	if ok && scope == ksGlobal && shellNavKeys[candidate] {
		return fmt.Errorf("%s is used for navigation in Settings and the tabs", candidate)
	}
	return validateKeyScope(action, candidate, overrides)
}

// validateKeyScope is only the active-scope collision check. Restoring an
// action's own default skips the reserved-key rules above (shell is enter) but
// must still not take a key another override now holds.
func validateKeyScope(action, candidate string, overrides map[string]string) error {
	scope, _, ok := scopeOfAction(action)
	if !ok {
		return fmt.Errorf("unknown action %q", action)
	}
	for _, sec := range keymapSections {
		other, ok := sectionScopes[sec.name]
		if !ok || !scopesOverlap(scope, other) {
			continue
		}
		for _, name := range sec.actions {
			if name == action {
				continue
			}
			for _, k := range keysOf(name, overrides) {
				if k == candidate {
					return fmt.Errorf("%s is already used by '%s' (%s)", candidate, defaultBindings[name].desc, sec.name)
				}
			}
		}
	}
	return nil
}
