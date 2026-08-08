package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

// paletteModel builds a model with a single selected worktree (no PR) ready for
// palette inspection.
func paletteModel(branch string, isMain bool) Model {
	m := renderModel()
	m.cfg = &config.Config{Upstream: "origin/main"}
	m.state = &config.State{}
	m.procs = coreexec.NewManager()
	m.statuses = map[string]git.StatusSummary{}
	m.worktrees = []git.Worktree{{Path: "/w/" + branch, Branch: branch, IsMain: isMain}}
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: m.worktrees[0]}})
	return m
}

func paletteByLabel(cmds []paletteCmd) map[string]paletteCmd {
	out := make(map[string]paletteCmd, len(cmds))
	for _, c := range cmds {
		out[c.label] = c
	}
	return out
}

func TestPaletteCommandsScopes(t *testing.T) {
	m := paletteModel("feat", false)
	m.prByBranch = map[string]gh.PR{"feat": {Number: 42}}
	byLabel := paletteByLabel(m.paletteCommands())

	commit := byLabel["Commit changes"]
	if got := commit.scopeHint(m); got != "feat" {
		t.Errorf("commit scope = %q, want feat", got)
	}
	if ok, reason := commit.available(m); !ok {
		t.Errorf("commit should be available, got reason %q", reason)
	}
	if commit.binding.Help().Key != "C" {
		t.Errorf("commit key hint = %q, want C", commit.binding.Help().Key)
	}

	approve := byLabel["PR: approve"]
	if got := approve.scopeHint(m); got != "PR #42" {
		t.Errorf("approve scope = %q, want PR #42", got)
	}
	if ok, _ := approve.available(m); !ok {
		t.Error("approve should be available for a PR-backed worktree")
	}

	prune := byLabel["Prune worktree"]
	if ok, reason := prune.available(m); !ok {
		t.Errorf("prune should be available on a feature worktree, got %q", reason)
	}
}

func TestPaletteCommandsAvailabilityReasons(t *testing.T) {
	// Main worktree: prune/update/diff refused with a reason.
	main := paletteModel("main", true)
	byLabel := paletteByLabel(main.paletteCommands())
	if ok, reason := byLabel["Prune worktree"].available(main); ok || reason != "main worktree" {
		t.Errorf("prune on main: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := byLabel["PR: approve"].available(main); ok || reason != "no PR connected" {
		t.Errorf("approve without PR: ok=%v reason=%q", ok, reason)
	}

	// No selection at all.
	empty := renderModel()
	empty.cfg = &config.Config{Upstream: "origin/main"}
	empty.state = &config.State{}
	byLabel = paletteByLabel(empty.paletteCommands())
	if ok, reason := byLabel["Commit changes"].available(empty); ok || reason != "no worktree selected" {
		t.Errorf("commit without selection: ok=%v reason=%q", ok, reason)
	}
}

func TestPaletteLabelsUnique(t *testing.T) {
	m := paletteModel("feat", false)
	m.cfg.Aliases = []config.Alias{{Name: "dev", Command: "npm run dev"}}
	seen := map[string]bool{}
	for _, c := range m.paletteCommands() {
		if seen[c.label] {
			t.Errorf("duplicate palette label %q", c.label)
		}
		seen[c.label] = true
	}
	if !seen["alias: dev"] {
		t.Error("config alias should appear as a palette entry")
	}
}

func TestOpenPaletteRendersScopeAndKey(t *testing.T) {
	m := paletteModel("feat", false)
	nm, _ := m.openPalette()
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindPalette {
		t.Fatal("openPalette should open a KindPalette modal")
	}
	found := false
	for label := range model.paletteByLabel {
		if strings.HasPrefix(label, "Commit changes") {
			found = true
			if !strings.Contains(label, "feat") {
				t.Errorf("commit row should show the selected branch: %q", label)
			}
			if !strings.Contains(label, "C") {
				t.Errorf("commit row should show its keybinding: %q", label)
			}
		}
	}
	if !found {
		t.Error("palette is missing the Commit changes entry")
	}
}

func TestPaletteSubmitUnavailableSetsStatus(t *testing.T) {
	m := paletteModel("main", true)
	nm, _ := m.openPalette()
	model := nm.(Model)

	var pruneLabel string
	for label := range model.paletteByLabel {
		if strings.HasPrefix(label, "Prune worktree") {
			pruneLabel = label
		}
	}
	if pruneLabel == "" {
		t.Fatal("palette is missing the Prune worktree entry")
	}
	nm, cmd := model.Update(modals.SubmitMsg{Kind: modals.KindPalette, Value: pruneLabel})
	model = nm.(Model)
	if cmd != nil {
		t.Error("unavailable palette command should not return a command")
	}
	if model.status != "main worktree" {
		t.Errorf("status = %q, want the refusal reason", model.status)
	}
}

func TestPaletteSubmitRunsCommand(t *testing.T) {
	m := paletteModel("feat", false)
	nm, _ := m.openPalette()
	model := nm.(Model)

	var refreshLabel string
	for label := range model.paletteByLabel {
		if strings.HasPrefix(label, "Refresh") {
			refreshLabel = label
		}
	}
	if refreshLabel == "" {
		t.Fatal("palette is missing the Refresh entry")
	}
	nm, cmd := model.Update(modals.SubmitMsg{Kind: modals.KindPalette, Value: refreshLabel})
	model = nm.(Model)
	if cmd == nil {
		t.Error("available palette command should return its command")
	}
	if model.modal != nil {
		t.Error("palette should close after a command runs")
	}
}

func TestPaletteOpensViaKeybinding(t *testing.T) {
	m := paletteModel("feat", false)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindPalette {
		t.Error("ctrl+k should open the command palette")
	}
}
