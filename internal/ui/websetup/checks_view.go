package websetup

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// githubChecks are the rows of the GitHub screen; nil means every check
// (the doctor screen).
var githubChecks = []string{checks.IDGHInstalled, checks.IDGHAuth}

func (m Model) checkRows(ids []string) []checks.Check {
	if ids == nil {
		return m.checks
	}
	var out []checks.Check
	for _, id := range ids {
		if c, ok := checks.Find(m.checks, id); ok {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) checksScreen() screen {
	if m.current() == screenDoctor {
		return screenDoctor
	}
	return screenGitHub
}

// placeGitHubCursor puts the GitHub screen's cursor on the first problem, so
// enter runs its fix right away.
func (m *Model) placeGitHubCursor() {
	for i, c := range m.checkRows(githubChecks) {
		if c.State == checks.Warn || c.State == checks.Fail {
			m.cursor[screenGitHub] = i
			return
		}
	}
	m.cursor[screenGitHub] = 0
}

// renderChecks lists checks with their state glyph, title and detail; the
// focused row that has a fix shows it underneath.
func (m Model) renderChecks(rows []checks.Check, cursor int, showSkipFix bool) string {
	if len(rows) == 0 {
		return "  " + m.st.accent.Render("…") + " " + m.st.dim.Render("checking this computer")
	}
	w, _ := m.size()
	var b strings.Builder
	for i, c := range rows {
		focused := i == cursor
		label := m.marker(focused) + m.st.glyph(c.State) + " " + m.st.text.Render(c.Title)
		b.WriteString(m.hang(label, 25, m.st.dim.Render(c.Detail), w-1) + "\n")
		problem := c.State == checks.Warn || c.State == checks.Fail
		if c.Fix != nil && (problem || (showSkipFix && focused)) {
			how := "c copy"
			if c.Fix.Inline {
				how = "enter run it now · c copy"
			}
			fix := c.Fix.Command
			if focused {
				fix += "   " + m.st.dim.Render(how)
			}
			b.WriteString(m.hang("    "+m.st.dim.Render("fix"), 9, fix, w-1) + "\n")
		}
	}
	if m.checksLoading {
		b.WriteString("  " + m.st.accent.Render("…") + " " + m.st.dim.Render("checking again") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) githubView() string {
	rows := m.checkRows(githubChecks)
	w, _ := m.size()
	body := "\n" + m.renderChecks(rows, m.cursor[screenGitHub], false) + "\n\n" +
		indent(m.st.dim.Render(m.wrap(setup.GitHubLead+" "+setup.GitHubWithout, w-4)), 2)
	hs := []hint{{"enter", m.githubEnterHint(rows)}, {"r", "re-check"}, {"s", "skip GitHub"}, {"esc", "back"}}
	if m.mode == Edit {
		hs = []hint{{"enter", m.githubEnterHint(rows)}, {"c", "copy fix"}, {"r", "re-check"}, {"esc", "back"}}
	}
	return m.frame("Connect GitHub", m.counter(), body, hs)
}

func (m Model) githubEnterHint(rows []checks.Check) string {
	if c, ok := m.focusedCheck(rows, m.cursor[screenGitHub]); ok && inlineFix(c) {
		return "fix"
	}
	if m.mode == Edit {
		return "done"
	}
	return "next"
}

func (m Model) focusedCheck(rows []checks.Check, cursor int) (checks.Check, bool) {
	if cursor < 0 || cursor >= len(rows) {
		return checks.Check{}, false
	}
	return rows[cursor], true
}

func inlineFix(c checks.Check) bool {
	return c.Fix != nil && c.Fix.Inline && c.State != checks.OK
}

func (m Model) doctorView() string {
	failed, warned := checks.Counts(m.checks)
	summary := m.st.ok.Render("✓") + " " + m.st.text.Render("Everything bonsai web needs is in place.")
	switch {
	case failed > 0:
		summary = m.st.fail.Render("✗") + " " + m.st.text.Render(problems(failed, warned))
	case warned > 0:
		summary = m.st.warn.Render("!") + " " + m.st.text.Render(problems(failed, warned))
	}
	if len(m.checks) == 0 {
		summary = ""
	}
	body := "\n" + m.renderChecks(m.checks, m.cursor[screenDoctor], true) + "\n\n  " + summary
	return m.frame("Doctor", m.counter(), body, []hint{{"↑↓", "move"}, {"enter", "run fix"}, {"c", "copy fix"}, {"r", "re-check"}, {"esc", "back"}})
}

// problems is "1 problem, 2 warnings".
func problems(failed, warned int) string {
	var parts []string
	if failed > 0 {
		parts = append(parts, plural(failed, "problem"))
	}
	if warned > 0 {
		parts = append(parts, plural(warned, "warning"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

func (m Model) onChecks(key string, ids []string) (tea.Model, tea.Cmd) {
	s := m.checksScreen()
	rows := m.checkRows(ids)
	cursor := m.cursor[s]
	switch key {
	case "up", "k":
		m.cursor[s] = m.move(s, -1, len(rows))
	case "down", "j":
		m.cursor[s] = m.move(s, 1, len(rows))
	case "r":
		return m, m.startChecks()
	case "c":
		c, ok := m.focusedCheck(rows, cursor)
		if !ok || c.Fix == nil {
			m.flash = "This row has no command to copy."
			return m, nil
		}
		if err := m.backend.Copy(c.Fix.Command); err != nil {
			m.flash = "Could not copy (" + err.Error() + "). The command is shown above."
		} else {
			m.flash = "Copied: " + c.Fix.Command
		}
	case "enter":
		if c, ok := m.focusedCheck(rows, cursor); ok && inlineFix(c) {
			cmd := m.backend.FixCommand(*c.Fix)
			if cmd == nil {
				m.flash = "Run this in another terminal: " + c.Fix.Command
				return m, nil
			}
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return fixDoneMsg{err: err} })
		}
		if s == screenDoctor {
			return m, nil
		}
		return m.next()
	case "s":
		if s == screenGitHub && m.mode == Wizard {
			return m.next()
		}
	case "esc":
		return m.pop(), nil
	case "q":
		return m.requestQuit()
	}
	return m, nil
}
