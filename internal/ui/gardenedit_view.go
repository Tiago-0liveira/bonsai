package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// fitCell clips s to w display cells and pads to exactly w.
func fitCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func whenText(t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// remoteBanner is the line shown when someone else wrote a revision since the editor's base.
func remoteBanner(e *editorDoc) string {
	r := e.remote
	who := "someone"
	if r.Author != "" {
		who = "@" + r.Author
	}
	rev := ""
	if r.Hash != "" {
		rev = " · rev " + shortHash(r.Hash)
	}
	verb := "reload"
	if e.dirty() {
		verb = "compare"
	}
	return fmt.Sprintf("Updated on GitHub by %s · %s%s. Press c to %s; your text is kept.", boardSafe(who), whenText(r.At), rev, verb)
}

var editorHelp = []string{
	"SPECIFICATION EDITOR KEYS",
	"",
	"Sections focus (the rail)",
	"  up/down or k/j   choose a section",
	"  enter or right   type in the section",
	"  /                search the whole document",
	"  n / N            next / previous match",
	"  c                compare or reload after a remote edit",
	"  ctrl+s           review and save a revision",
	"  esc              leave (asks first when there are changes)",
	"",
	"Text focus",
	"  tab or esc       back to the sections",
	"  ctrl+s           review and save a revision",
	"",
	"Saving appends a new revision comment. The Issue body, title and labels are never changed.",
	"Press any key to close this help.",
}

func (m Model) gsEditor(width, rows int) []string {
	p := theme.Current
	e := m.garden.drafts.edit
	if e == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "No document is open.")}}}, nil)
	}
	m.gardenSizeEditor()
	railW := gardenEditorRail(width)
	tw, th := m.gardenEditorText()
	blank := blankRow(width, p.Background)

	out := make([]string, 0, rows)
	out = append(out, gText(width, p.Warning, true, fmt.Sprintf("‹ #%d %s · Specification (Esc)", e.number, boardTitle(e.title))))
	state := "saved"
	if e.dirty() {
		state = "modified"
	}
	status := e.position() + " · " + state
	if e.query != "" {
		status += fmt.Sprintf(" · search %q: %d match%s", e.query, len(e.matches), map[bool]string{true: "", false: "es"}[len(e.matches) == 1])
		if len(e.matches) > 0 {
			status += fmt.Sprintf(" (%d/%d)", e.mi+1, len(e.matches))
		}
	}
	out = append(out, gText(width, p.Muted, false, status))
	if e.remote != nil {
		out = append(out, gText(width, p.Warning, true, "! "+remoteBanner(e)))
	} else {
		out = append(out, blank)
	}

	body := make([]string, th)
	if e.help {
		for i := range body {
			text := ""
			if i < len(editorHelp) {
				text = editorHelp[i]
			}
			fg := p.Text
			if i == 0 {
				fg = p.Warning
			}
			body[i] = gText(width, fg, i == 0, text)
		}
	} else {
		ta := e.ta
		taLines := strings.Split(ta.View(), "\n")
		avail := width - railW - 2
		pad := max(0, (avail-tw)/2)
		first := 0
		if len(e.secs) > th {
			first = max(0, min(e.cur-th/2, len(e.secs)-th))
		}
		for i := 0; i < th; i++ {
			rail := fitCell("", railW)
			bg, fg, bold := p.Surface, p.Muted, false
			if n := first + i; n < len(e.secs) {
				mark := " "
				if e.modified(n) {
					mark = "*"
				}
				label := " " + mark + " " + e.secs[n].title
				if n == e.cur {
					bg, fg, bold = p.Raised, p.Warning, true
					if !e.text {
						label = ">" + label[1:]
					}
				}
				rail = fitCell(label, railW)
			}
			text := ""
			if i < len(taLines) {
				text = ansi.Truncate(taLines[i], tw, "")
			}
			text += strings.Repeat(" ", max(0, tw-ansi.StringWidth(text)))
			line := seg(fg, bg, bold).Render(rail) + seg("", p.Background, false).Render(strings.Repeat(" ", 2+pad)) + text
			body[i] = shellRow(width, p.Background, line, "")
		}
	}
	out = append(out, body...)

	switch {
	case e.discard:
		keep, drop := gButton("Keep editing", e.btn == 0), gButton("Discard changes", e.btn == 1)
		out = append(out, shellRow(width, p.Background, seg(p.Warning, p.Background, true).Render(" Discard changes? ")+keep+seg("", p.Background, false).Render("  ")+drop, ""))
	case e.searching:
		e.si.Width = max(1, width-12)
		out = append(out, shellRow(width, p.Background, seg(p.Muted, p.Background, false).Render(" Search: ")+e.si.View(), ""))
	case m.garden.err != nil:
		out = append(out, gText(width, p.Danger, true, "! "+m.garden.err.Message+" Your text is kept."))
	case e.notice != "":
		out = append(out, gText(width, p.Warning, false, e.notice))
	default:
		out = append(out, gText(width, p.Muted, false, "Saving appends a revision comment. Body, title and labels are never changed."))
	}
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

func diffLabel(added, removed int) string { return fmt.Sprintf("+%d −%d", added, removed) }

func (m Model) gsSavePreview(width, rows int) []string {
	p := theme.Current
	g := m.garden
	d := g.drafts
	pv := d.savePreview
	if pv == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "Nothing to review.")}}}, nil)
	}
	resolve := pv.Resolution != nil
	title, intro, button := "✎ REVIEW BEFORE SAVING", fmt.Sprintf("Appends one specification revision to #%d on GitHub. The Issue body, title and labels are not changed.", pv.Number), "Save revision"
	if resolve {
		title = "✎ REVIEW THE RESULT BEFORE PUBLISHING"
		intro = fmt.Sprintf("Publishes your result as one revision on #%d that supersedes %d head%s, so the divergence ends. Nothing else is changed.", pv.Number, len(pv.Parents), plural(len(pv.Parents)))
		button = "Publish result"
	}
	var facts []string
	for _, sd := range pv.Diff {
		facts = append(facts, gField(width, 24, sd.Section, grun{diffLabel(sd.Added, sd.Removed), p.Text, false}))
	}
	if len(facts) == 0 {
		facts = append(facts, gText(width, p.Muted, false, "No section text changed."))
	}
	notes := []string{}
	if pv.BodyChanged {
		notes = append(notes, "The Issue body was edited on GitHub (not part of the specification).")
	}
	if pv.AlreadySaved {
		notes = append(notes, "This exact revision is already on GitHub; saving returns it without appending again.")
	}
	var action string
	switch g.busy {
	case busySave:
		action = gText(width, p.Warning, true, "Saving… keys are ignored until GitHub answers.")
	default:
		action = gButtons(width, "                    ", gButton("Back to edit", d.btn == 0), gButton(button, d.btn == 1))
	}
	blocks := []gblock{
		{lines: []string{gText(width, p.Warning, true, title)}},
		{prio: 1, lines: gWrap(width, p.Text, false, intro)},
		gSpaced(2, width, facts),
	}
	if len(notes) > 0 {
		blocks = append(blocks, gblock{prio: 2, lines: gWrap(width, p.Warning, false, strings.Join(notes, "\n"))})
	}
	if v := d.saveView; v != nil && v.Err != nil {
		blocks = append(blocks, gblock{lines: m.gErrorLines(width, v.Err)})
	}
	blocks = blocksWithNotices(m, width, append(blocks, gblock{lines: []string{action}},
		gblock{prio: 3, lines: []string{gText(width, p.Muted, false, "Bonsai re-reads GitHub right before it saves and again afterwards.")}})...)
	return m.settingsASCII(gCompose(width, rows, blocks, nil))
}
