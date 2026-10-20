package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// secMark words and symbols the state of a section; color is never the only cue.
func (c *conflictState) secMark(i int) string {
	switch c.status[i] {
	case secSame:
		return "= same"
	case secYours:
		return "Y yours"
	case secRemote:
		return "R remote"
	}
	if c.resolved[i] {
		return "✓ chosen"
	}
	return "! both"
}

// remoteMeta is "@login · time · rev abcd1234 · source" for the remote head in view.
func (c *conflictState) remoteMeta() string {
	r := c.shown[c.ri]
	who := "someone"
	if r.Author != "" {
		who = "@" + boardSafe(r.Author)
	}
	rev := ""
	if r.Hash != "" {
		rev = " · rev " + shortHash(r.Hash)
	}
	s := fmt.Sprintf("%s · %s%s", who, whenText(r.At), rev)
	if r.Source != "" {
		s += " · " + boardSafe(r.Source)
	}
	if len(c.shown) > 1 {
		s += fmt.Sprintf(" · head %d of %d ([ ] switch)", c.ri+1, len(c.shown))
	}
	return s
}

func (m Model) gsConflict(width, rows int) []string {
	c := m.garden.drafts.conflict
	p := theme.Current
	if c == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "No conflict is open.")}}}, nil)
	}
	if c.intro {
		return m.gsConflictIntro(width, rows)
	}
	m.gardenSizeConflict()
	blank := blankRow(width, p.Background)
	out := []string{gText(width, p.Warning, true, fmt.Sprintf("‹ Remote edit conflict (Esc)   #%d %s", c.number, boardTitle(c.title)))}
	var tabs strings.Builder
	tabs.WriteString(seg("", p.Background, false).Render(" "))
	for pn := paneBase; pn < paneCount; pn++ {
		label := " " + pn.String() + " "
		if pn == c.pane {
			tabs.WriteString(seg(p.Background, p.Warning, true).Render("[" + pn.String() + "]"))
		} else {
			tabs.WriteString(seg(p.Muted, p.Background, false).Render(label))
		}
		tabs.WriteString(seg("", p.Background, false).Render(" "))
	}
	tabs.WriteString(seg(p.Muted, p.Background, false).Render(" Remote: " + c.remoteMeta()))
	out = append(out, shellRow(width, p.Background, ansi.Truncate(tabs.String(), width, "…"), ""))

	foot := 3 // the action rows and the notice
	body := max(1, rows-len(out)-foot)
	railW := 22
	if width < 100 {
		railW = 15
	}
	avail := width - railW - 2
	panes := []conflictPane{c.pane}
	if width >= 120 {
		left := c.pane
		if left == paneResult {
			left = paneRemote
		}
		panes = []conflictPane{left, paneResult}
	}
	colW := (avail - (len(panes)-1)*3) / len(panes)
	cols := make([][]string, len(panes))
	for i, pn := range panes {
		header := " " + pn.String() + " · " + c.titles[c.sel]
		lines := []string{gText(colW, p.Warning, true, header)}
		text := c.texts(pn, c.sel)
		if pn == paneResult && c.editing {
			ta := c.ta
			for _, l := range strings.Split(ta.View(), "\n") {
				lines = append(lines, shellRow(colW, p.Background, " "+ansi.Truncate(l, colW-1, ""), ""))
			}
		} else {
			if text == "" {
				text = "(empty)"
			}
			wrapped := gWrap(colW, p.Text, false, boardSafe(text))
			start := max(0, min(c.scroll, max(0, len(wrapped)-(body-1))))
			lines = append(lines, wrapped[start:]...)
		}
		for len(lines) < body {
			lines = append(lines, blankRow(colW, p.Background))
		}
		cols[i] = lines[:body]
	}
	for i := 0; i < body; i++ {
		rail := fitCell("", railW)
		bg, fg, bold := p.Surface, p.Muted, false
		if i < len(c.titles) {
			rail = fitCell(" "+fitCell(c.titles[i], railW-10)+" "+c.secMark(i), railW)
			if i == c.sel {
				bg, fg, bold = p.Raised, p.Warning, true
			}
			if c.status[i] == secBoth && !c.resolved[i] {
				fg = p.Danger
			}
		}
		parts := []string{seg(fg, bg, bold).Render(rail), seg("", p.Background, false).Render("  ")}
		for j := range cols {
			if j > 0 {
				parts = append(parts, seg(p.Border, p.Background, false).Render(" "+m.chrome("│")+" "))
			}
			parts = append(parts, cols[j][i])
		}
		out = append(out, shellRow(width, p.Background, strings.Join(parts, ""), ""))
	}
	switch {
	case c.notice != "":
		out = append(out, gText(width, p.Warning, true, "! "+c.notice))
	case m.garden.err != nil:
		out = append(out, gText(width, p.Danger, true, "! "+m.garden.err.Message))
	case c.unresolved() > 0:
		out = append(out, gText(width, p.Warning, false, fmt.Sprintf("%d section%s changed on both sides and need a choice before you can publish.", c.unresolved(), plural(c.unresolved()))))
	default:
		out = append(out, gText(width, p.Muted, false, "Every section is settled. Publish the result to end the divergence."))
	}
	if m.garden.busy == busyPreviewSave {
		out = append(out, gText(width, p.Warning, true, "Checking GitHub…"))
	} else {
		out = append(out, gText(width, p.Muted, false, "y take yours · m take remote · b take base · e edit result · p publish result · 1-4 pane"))
	}
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

func (m Model) gsConflictIntro(width, rows int) []string {
	p := theme.Current
	c := m.garden.drafts.conflict
	both := 0
	for _, s := range c.status {
		if s == secBoth {
			both++
		}
	}
	title := "! BOTH COPIES CHANGED"
	text := fmt.Sprintf("%d section%s changed on both sides.", both, plural(both))
	if c.view.Saved {
		title = "! COMPETING REVISIONS"
		text = "Your revision was saved, but another revision landed at the same time, so two heads exist. Nothing is preferred silently."
	} else if both == 0 && strings.TrimSpace(c.view.Yours) == "" {
		title = "! COMPETING REVISIONS"
		text = fmt.Sprintf("GitHub holds %d competing specification heads. Join them into one result.", len(c.view.Remote))
	}
	text += " Compare Base / Yours / Remote / Result."
	remote := "Remote: " + c.remoteMeta()
	if len(c.view.Remote) > 0 && c.view.Remote[0].Hash == "" {
		remote = "Remote: " + boardSafe(c.view.Remote[0].Source)
	}
	notes := ""
	if c.note != "" {
		notes = c.note
	}
	blocks := []gblock{
		{lines: []string{gHeadChip(width, title, p.Warning)}},
		{prio: 1, lines: gWrap(width, p.Text, false, text)},
		{prio: 2, lines: gWrap(width, p.Muted, false, remote)},
	}
	if notes != "" {
		blocks = append(blocks, gblock{prio: 1, lines: gWrap(width, p.Warning, true, "! "+notes)})
	}
	blocks = append(blocks, gblock{prio: 3, lines: gWrap(width, p.Muted, false, "Your content and the last good data are retained.")})
	return m.settingsASCII(gCompose(width, rows, blocks, []string{gButtons(width, "", gButton("Inspect revision changes ›", true))}))
}
