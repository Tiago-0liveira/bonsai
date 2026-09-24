package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// mergedKeyOverrides returns the effective key overrides: repo-level
// (.bonsai.yaml) with personal (state.json) overrides layered on top.
func (m Model) mergedKeyOverrides() map[string]string {
	out := make(map[string]string, len(m.cfg.Keys)+len(m.state.Prefs.Keys))
	for k, v := range m.cfg.Keys {
		out[k] = v
	}
	for k, v := range m.state.Prefs.Keys {
		out[k] = v
	}
	return out
}

// currentThemePreset is the palette preset in effect (personal pick over repo
// config, falling back to bonsai).
func (m Model) currentThemePreset() string {
	cur := m.state.Prefs.Theme
	if cur == "" {
		cur = m.cfg.Theme.Preset
	}
	if !theme.Valid(cur) {
		cur = "bonsai"
	}
	return cur
}

// effectiveEditor is the "open editor" command in effect: a personal
// override (state.json) wins over the repo's .bonsai.yaml editor setting;
// "" defers to $VISUAL/$EDITOR/vi (see coreexec.EditorCmd).
func (m Model) effectiveEditor() string {
	if m.state.Prefs.Editor != "" {
		return m.state.Prefs.Editor
	}
	return m.cfg.Editor
}

// prefsActions lists every bindable action grouped for display. Default is the
// key the action reverts to when the personal override is cleared — i.e. the
// repo-level override if one exists, else the built-in default.
func prefsActions(repoOverrides map[string]string) []prefs.Action {
	var out []prefs.Action
	for _, sec := range keymapSections {
		for _, name := range sec.actions {
			out = append(out, prefs.Action{
				Name:    name,
				Desc:    defaultBindings[name].desc,
				Default: effectiveKey(name, repoOverrides),
				Section: sec.name,
			})
		}
	}
	return out
}

// openPrefs opens the preferences overlay. startOnKeys places the cursor on
// the keybindings editor (used when arriving from the keymap modal).
func (m Model) openPrefs(startOnKeys bool) (tea.Model, tea.Cmd) {
	pm := prefs.New(
		m.currentThemePreset(),
		m.sort.name(),
		m.state.Prefs.PruneMerge,
		m.state.Prefs.PRStatus,
		m.state.Prefs.Editor,
		theme.Presets(),
		prefsActions(m.cfg.Keys),
		startOnKeys,
	)
	pm.SetOverrides(m.state.Prefs.Keys)
	pm.SetSize(m.width, m.height)
	m.prefs = &pm
	return m, nil
}

// applyPrefs applies a preferences snapshot (theme, keys, sort, prune default),
// persists it, and restyles every component — this is what makes theme picks
// preview live while the overlay is open.
func (m Model) applyPrefs(p prefs.SaveMsg) (tea.Model, tea.Cmd) {
	m.state.Prefs = config.Prefs{
		Theme:      p.Theme,
		Sort:       p.Sort,
		PruneMerge: p.PruneMerge,
		PRStatus:   p.PRStatus,
		Editor:     p.Editor,
		Keys:       p.Keys,
	}
	if err := m.state.Save(); err != nil {
		m.err = err
		return m, nil
	}

	theme.Current = theme.Resolve(p.Theme, m.cfg.Theme.Overrides)
	applyTheme()
	worktreelist.SetTheme(theme.Current)
	worktreelist.SetPRStatusMode(p.PRStatus)
	modals.SetTheme(theme.Current)
	prefs.SetTheme(theme.Current)

	m.keys = newKeyMap(m.mergedKeyOverrides())
	if cols := keyCollisions(m.mergedKeyOverrides()); len(cols) > 0 {
		m.status = "key conflicts ignored: " + cols[0]
	}
	if p.Sort != "" {
		m.sort = sortModeFromName(p.Sort)
		m.rebuildItems()
	}
	return m, nil
}

// openPrefEditorModal opens the input for a personal "open editor" command
// override (state.json). An empty submission clears it, deferring to
// .bonsai.yaml's editor setting (and then $VISUAL/$EDITOR/vi).
func (m Model) openPrefEditorModal() (tea.Model, tea.Cmd) {
	modal := modals.NewInput(modals.KindPrefEditor, "Personal editor override", "code -w, nvim, hx …")
	body := "current: " + m.state.Prefs.Editor
	if m.state.Prefs.Editor == "" {
		body = "current: (none — using .bonsai.yaml's editor, or $VISUAL/$EDITOR/vi)"
	}
	body += "\nEmpty clears the override. Takes priority over .bonsai.yaml's editor setting."
	modal.SetBody(body)
	modal.SetInitial(m.state.Prefs.Editor)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// openKeymap opens the searchable key reference. Enter on any row jumps to
// the preferences key editor.
func (m Model) openKeymap() (tea.Model, tea.Cmd) {
	keySt := lipgloss.NewStyle().Foreground(theme.Current.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)

	overrides := m.mergedKeyOverrides()
	type kmSection struct {
		name    string
		entries []paletteEntry
	}
	sections := make([]kmSection, 0, len(keymapSections))
	for _, sec := range keymapSections {
		entries := make([]paletteEntry, 0, len(sec.actions))
		for _, name := range sec.actions {
			k := effectiveKey(name, overrides)
			custom := ""
			if _, ok := overrides[name]; ok {
				custom = " (custom)"
			}
			plain := fmt.Sprintf("%s %s %s%s", k, defaultBindings[name].desc, sec.name, custom)
			display := keySt.Render(padRight(k, 10)) + padRight(defaultBindings[name].desc, 20) +
				dim.Render("· "+sec.name+custom)
			entries = append(entries, paletteEntry{display: display, plain: plain})
		}
		sections = append(sections, kmSection{name: sec.name, entries: entries})
	}

	filter := func(q string) []modals.Section {
		q = strings.ToLower(strings.TrimSpace(q))
		out := make([]modals.Section, 0, len(sections))
		for _, s := range sections {
			items := make([]string, 0, len(s.entries))
			for _, e := range s.entries {
				if q == "" || strings.Contains(strings.ToLower(e.plain), q) {
					items = append(items, e.display)
				}
			}
			out = append(out, modals.Section{Name: s.name, Items: items})
		}
		return out
	}

	modal := modals.NewSectionedFuzzy(modals.KindKeymap, "Keybindings · enter to edit in preferences", filter, filter(""))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// openLegend opens the status-glyph legend.
func (m Model) openLegend() (tea.Model, tea.Cmd) {
	modal := modals.NewScroll(modals.KindLegend, "Status glyphs", m.legendContent())
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// legendContent builds the glyph reference with live-colored examples.
func (m Model) legendContent() string {
	p := theme.Current
	pass := lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	fail := lipgloss.NewStyle().Foreground(p.Danger).Bold(true)
	pend := lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
	badge := lipgloss.NewStyle().Foreground(p.PRBadge).Bold(true)
	dirty := lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
	run := lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	dim := lipgloss.NewStyle().Foreground(p.Dim)
	sec := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)

	var b strings.Builder
	row := func(glyph, meaning string) {
		fmt.Fprintf(&b, "  %-16s %s\n", glyph, meaning)
	}

	b.WriteString(sec.Render("Worktree list") + "\n")
	row(pass.Render("●")+" / "+fail.Render("●")+" / "+pend.Render("◐"), "CI status of the connected PR: passing / failing / pending")
	row(badge.Render("#123"), "open pull request connected to this branch")
	row(badge.Render("#123 merged")+"  "+dim.Render("#123 closed"), "PR state once it is no longer open")
	row("↑2 ↓1", "commits ahead of / behind the base ref ("+m.cfg.Upstream+")")
	row(dirty.Render("●3"), "3 tracked files changed (staged + modified)")
	row(dim.Render("?2"), "2 untracked files")
	row(run.Render("▶1"), "1 process currently running in this worktree")

	b.WriteString("\n" + sec.Render("Checks & PR tabs") + "\n")
	row(pass.Render("✓"), "check passed / review approved / mergeable")
	row(fail.Render("✗"), "check failed / changes requested / conflicting")
	row(pend.Render("◐"), "check still pending")

	b.WriteString("\n" + sec.Render("Inspector") + "\n")
	row(run.Render("✓ clean"), "no working-tree changes")
	row(dirty.Render("● 2 staged · 1 modified · 3 untracked"), "working-tree change breakdown")

	b.WriteString("\n" + dim.Render("Colors follow your theme — change it in preferences ( , )."))
	return b.String()
}

// padRight pads s with spaces to width w.
func padRight(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
