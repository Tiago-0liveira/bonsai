package accounts

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// View uses the surrounding Agents pane's border.
func (q Quick) View() string {
	if q.w <= 0 || q.h <= 0 {
		return ""
	}
	w, h := FrameSize(q.w, q.h)
	inner := ContentWidth(w)
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	header := q.header(inner)
	footer := "↑↓ Profile · r Refresh · m Profiles · Esc Close"
	if inner < ansi.StringWidth(footer) {
		footer = "↑↓ Profile · r Refresh · m More · Esc"
	}
	if q.ascii {
		footer = strings.ReplaceAll(footer, "↑↓", "j/k")
	}
	var body []string
	rows := max(h-len(header)-1, 0)
	switch {
	case !q.listed:
		body = []string{dim.Render("Loading accounts…")}
	case q.listErr != "":
		body = []string{lipgloss.NewStyle().Foreground(theme.Current.Danger).Render("Couldn't load accounts"), dim.Render(q.listErr)}
	case len(q.accounts) == 0:
		body = []string{dim.Render("No accounts yet.")}
	default:
		body = q.body(inner, rows)
	}
	lines := append([]string{}, header...)
	for i := range rows {
		lines = append(lines, at(body, i))
	}
	lines = append(lines, dim.Render(footer))
	if len(lines) > h {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	for i := range lines {
		lines[i] = fit(" "+fit(lines[i], inner), w)
	}
	return strings.Join(lines, "\n")
}

func (q Quick) updated() string {
	var parts []string
	if e := q.usage[q.focus]; e.Have && !e.Usage.FetchedAt.IsZero() {
		parts = append(parts, updatedText(q.now(), e.Usage.FetchedAt))
	}
	if q.busy() {
		parts = append(parts, "Refreshing…")
	}
	return strings.Join(parts, " · ")
}

func (q Quick) header(w int) []string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Text).Render("Usage")
	subtitle := " · Remaining capacity"
	updated := q.updated()
	if ansi.StringWidth(title+subtitle)+2+ansi.StringWidth(updated) > w {
		subtitle = ""
	}
	left := title + dim.Render(subtitle)
	if updated == "" {
		return []string{left}
	}
	if gap := w - ansi.StringWidth(left) - ansi.StringWidth(updated); gap >= 2 {
		return []string{left + strings.Repeat(" ", gap) + dim.Render(updated)}
	}
	lines := []string{left}
	for _, line := range wrapUsageFields(strings.Split(updated, " · "), w) {
		lines = append(lines, dim.Render(line))
	}
	return lines
}

func (q Quick) providerHeading(p agents.ProviderID, w int) string {
	rule := "─"
	if q.ascii {
		rule = "-"
	}
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	glyph := theme.ProviderGlyph(p, q.ascii)
	label := rule + rule + " " + glyph + " " + strings.ToUpper(providerName(p)) + " "
	return dim.Render(rule+rule+" ") + theme.ProviderStyle(p).Render(glyph) + dim.Render(" "+strings.ToUpper(providerName(p))+" "+strings.Repeat(rule, max(w-ansi.StringWidth(label), 0)))
}

type stripColumns struct {
	labels [2]int
	cells  int
	width  int
	paired bool
}

func headlineWindows(u agentsapi.Usage) []*agents.UsageLimit {
	return []*agents.UsageLimit{u.Headline.Primary, u.Headline.Secondary}
}

// stripWindows pairs each Antigravity pool's windows just like the other
// providers. Copies carry display labels without changing stored limits.
func stripWindows(a agentsapi.Account, u agentsapi.Usage) ([][]*agents.UsageLimit, []agents.UsageLimit) {
	rows := [][]*agents.UsageLimit{headlineWindows(u)}
	extra := u.Headline.Extra
	if u.Headline.Primary == nil && u.Headline.Secondary == nil && len(extra) == 0 {
		extra = u.Snapshot.Limits
	}
	if a.Provider != "antigravity" {
		return rows, extra
	}
	label := func(l *agents.UsageLimit, pool string, i int) *agents.UsageLimit {
		if l == nil {
			return nil
		}
		copy := *l
		copy.Window = pool + " " + stripWindowName(l, i)
		return &copy
	}
	// Third-party pools pair their own windows. A pool the provider names keeps
	// that name; unnamed ones share the generic "Claude/GPT" row.
	type pool struct {
		name    string
		windows [2]*agents.UsageLimit
	}
	var thirds []*pool
	var other []agents.UsageLimit
	for _, l := range extra {
		// Identify the model pool as well as the window: a snapshot fallback
		// can contain default windows and unfamiliar limits too.
		pk := strings.ToLower(l.Group + " " + l.ID)
		isThird := strings.Contains(pk, "claude") || strings.Contains(pk, "gpt") || strings.Contains(pk, "3p") || strings.Contains(pk, "third")
		i := -1
		switch l.Window {
		case "5h":
			i = 0
		case "weekly":
			i = 1
		}
		if i >= 0 && strings.Contains(pk, "gemini") && rows[0][i] == nil {
			copy := l
			rows[0][i] = &copy
		} else if i >= 0 && isThird {
			// The combined Claude+GPT pool keeps its short generic label; a
			// pool for one reported model keeps that model's name.
			name, g := l.Group, strings.ToLower(l.Group)
			if name == "" || (strings.Contains(g, "claude") && strings.Contains(g, "gpt")) {
				name = "Claude/GPT"
			}
			var p *pool
			for _, t := range thirds {
				if t.name == name {
					p = t
				}
			}
			if p == nil {
				p = &pool{name: name}
				thirds = append(thirds, p)
			}
			if p.windows[i] == nil {
				p.windows[i] = label(&l, name, i)
			} else {
				other = append(other, l)
			}
		} else {
			other = append(other, l)
		}
	}
	if len(thirds) > 0 {
		// A third-party balance is not the default balance. Keep the default
		// windows visibly unknown when the provider only reports that pool.
		for i, window := range []string{"5h", "weekly"} {
			if rows[0][i] == nil {
				rows[0][i] = &agents.UsageLimit{Window: window}
			}
		}
		for _, p := range thirds {
			rows = append(rows, p.windows[:])
		}
	}
	return rows, other
}

func stripWindowName(l *agents.UsageLimit, i int) string {
	return usagefmt.WindowName(l, []string{"Primary", "Secondary"}[i])
}

// columns sizes the whole list, so every provider and model pool shares the
// same meter, percentage and reset columns.
func (q Quick) columns(accts []agentsapi.Account, w int) stripColumns {
	c := stripColumns{cells: barCells, width: max(w-3, 0)}
	for _, a := range accts {
		windows, _ := stripWindows(a, q.usage[a.ID].Usage)
		for _, row := range windows {
			for i, l := range row {
				if l != nil {
					c.labels[i] = max(c.labels[i], ansi.StringWidth(stripWindowName(l, i)))
				}
			}
		}
	}
	colW := max((c.width-2)/2, 0)
	for cells := barCells; cells >= 4; cells-- {
		fits := true
		for _, a := range accts {
			windows, _ := stripWindows(a, q.usage[a.ID].Usage)
			for _, row := range windows {
				for i, l := range row {
					if l != nil && ansi.StringWidth(q.windowLine(l, i, c.labels[i], cells)) > colW {
						fits = false
					}
				}
			}
		}
		if fits {
			c.cells, c.width, c.paired = cells, colW, true
			return c
		}
	}
	c.labels[0] = max(c.labels[0], c.labels[1])
	c.labels[1] = c.labels[0]
	return c
}

func (q Quick) body(inner, rows int) []string {
	if rows <= 0 {
		return nil
	}
	accts := q.ordered()
	c := q.columns(accts, inner)
	var lines []string
	headings := map[int]agents.ProviderID{}
	selectedStart, selectedEnd := 0, 0
	for start := 0; start < len(accts); {
		end := start + 1
		for end < len(accts) && accts[end].Provider == accts[start].Provider {
			end++
		}
		headings[len(lines)] = accts[start].Provider
		lines = append(lines, q.providerHeading(accts[start].Provider, inner))
		for _, a := range accts[start:end] {
			strip := q.accountStrip(a, c, max(inner-3, 0))
			if a.ID == q.focus {
				selectedStart, selectedEnd = len(lines), len(lines)+len(strip)
			}
			lines = append(lines, strip...)
			lines = append(lines, "")
		}
		start = end
	}
	start := 0
	var heading []string
	if selectedEnd > rows {
		// Keep all selected details above the footer whenever they fit. When
		// scrolling into a provider, repeat its heading without a blank row.
		start = max(selectedEnd-rows+1, 0)
		start = min(start, selectedStart)
		if _, isHeading := headings[start]; !isHeading && rows > 1 {
			for i := start; i >= 0; i-- {
				if provider, ok := headings[i]; ok {
					heading = []string{q.providerHeading(provider, inner)}
					break
				}
			}
		}
	}
	return append(heading, lines[start:min(start+rows-len(heading), len(lines))]...)
}

func (q Quick) accountStrip(a agentsapi.Account, c stripColumns, w int) []string {
	e := q.usage[a.ID]
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	metadata := usageMetadata(e.Usage.Snapshot, q.now())
	plan := a.Info.Plan
	if e.Usage.Snapshot.Plan != "" {
		plan = e.Usage.Snapshot.Plan
	}
	status := ""
	if e.Stale() {
		status += " · stale"
	}
	if e.Have && e.Refreshing {
		status += " · loading…"
	}
	nameW := w
	inline := ""
	if len(metadata) == 1 {
		text := strings.Join(metadata[0], " · ")
		if ansi.StringWidth(text)+2+min(ansi.StringWidth(a.Name), 8)+ansi.StringWidth(status) <= w {
			inline = text
			nameW -= ansi.StringWidth(text) + 2
			metadata = nil
		}
	}
	// Plan labels yield to names and metadata, rather than consuming the
	// entire title when the content pane is narrow.
	suffix := status
	if plan != "" && ansi.StringWidth(" · "+plan+status)+min(ansi.StringWidth(a.Name), 8) <= nameW {
		suffix = " · " + plan + status
	}
	name := ansi.Truncate(a.Name, max(nameW-ansi.StringWidth(suffix), 1), "…")
	name = lipgloss.NewStyle().Foreground(theme.Current.Text).Bold(a.ID == q.focus).Render(name)
	title := name + dim.Render(suffix)
	if inline != "" {
		title += strings.Repeat(" ", max(w-ansi.StringWidth(title)-ansi.StringWidth(inline), 2)) + inline
	}
	lines := []string{title}
	switch {
	case !e.Have && e.Refreshing:
		lines = append(lines, dim.Render("Loading…"))
	case !e.Have:
		lines = append(lines, dim.Render("Usage unavailable"))
		for _, line := range wrapUsageFields([]string{e.Err}, w) {
			lines = append(lines, dim.Render(line))
		}
	default:
		windows, _ := stripWindows(a, e.Usage)
		if windows[0][0] == nil && windows[0][1] == nil {
			lines = append(lines, dim.Render("No shared windows"))
		}
		for _, row := range windows {
			lines = append(lines, q.windowPair(row, c, w)...)
		}
	}
	for _, fields := range metadata {
		for _, line := range wrapUsageFields(fields, w) {
			lines = append(lines, line)
		}
	}
	_, extra := stripWindows(a, e.Usage)
	for _, l := range extra {
		label := limitLabel(l)
		if l.Group != "" {
			label = l.Group + " · " + usagefmt.WindowName(&l, label)
		}
		// Extra and model-specific limits use the same treatment as windows.
		l.Window, l.Label = "", label
		lines = append(lines, q.stackedWindow(&l, 0, ansi.StringWidth(label), w)...)
	}
	rail := "▌"
	if q.ascii {
		rail = "|"
	}
	rail = theme.ProviderStyle(a.Provider).Render(rail)
	for i := range lines {
		marker := " "
		if i == 0 && a.ID == q.focus {
			marker = lipgloss.NewStyle().Bold(true).Render(">")
		}
		lines[i] = marker + rail + " " + lines[i]
	}
	return lines
}

func (q Quick) windowPair(windows []*agents.UsageLimit, c stripColumns, w int) []string {
	if windows[0] == nil && windows[1] == nil {
		return nil
	}
	if c.paired {
		left, right := "", ""
		if windows[0] != nil {
			left = q.windowLine(windows[0], 0, c.labels[0], c.cells)
		}
		if windows[1] != nil {
			right = q.windowLine(windows[1], 1, c.labels[1], c.cells)
		}
		return []string{fit(left, c.width) + "  " + right}
	}
	var lines []string
	for i, l := range windows {
		if l != nil {
			lines = append(lines, q.stackedWindow(l, i, c.labels[i], w)...)
		}
	}
	return lines
}

func (q Quick) windowLine(l *agents.UsageLimit, i, labelW, cells int) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	label := stripWindowName(l, i)
	line := dim.Render(label+strings.Repeat(" ", max(labelW-ansi.StringWidth(label), 0))) + " "
	f, ok := agents.RemainingFraction(l)
	if !ok {
		return line + dim.Render("Unavailable")
	}
	line += RemainingCapacity(f, cells, q.ascii)
	if l.ResetsAt != nil {
		line += dim.Render(" · Reset ") + usageValue(usagefmt.ResetsIn(q.now(), l.ResetsAt, true), theme.Current.Accent)
	}
	return line
}

func (q Quick) stackedWindow(l *agents.UsageLimit, i, labelW, w int) []string {
	for cells := barCells; cells >= 4; cells-- {
		line := q.windowLine(l, i, labelW, cells)
		if ansi.StringWidth(line) <= w {
			return []string{line}
		}
	}
	// Keep the reset on a continuation row before sacrificing the meter.
	reset := l.ResetsAt
	copy := *l
	copy.ResetsAt = nil
	label := stripWindowName(l, i)
	labelW = min(labelW, max(w-10, 1))
	copy.Window, copy.Label = "", ansi.Truncate(label, labelW, "…")
	var line string
	for cells := barCells; cells >= 0; cells-- {
		if cells > 0 && cells < 4 {
			continue
		}
		line = q.windowLine(&copy, i, labelW, cells)
		if ansi.StringWidth(line) <= w {
			break
		}
	}
	lines := []string{line}
	if reset != nil {
		field := usageField("Reset", usagefmt.ResetsIn(q.now(), reset, true), theme.Current.Accent)
		for _, text := range wrapUsageFields([]string{field}, max(w-2, 1)) {
			lines = append(lines, "  "+text)
		}
	}
	return lines
}

func stripMeter(f float64, cells int, ascii bool) string {
	bar := usagefmt.Bar(f, cells)
	filled, empty := "▰", "▱"
	if ascii {
		filled, empty = "#", "."
	}
	n := strings.Count(bar, "█")
	return rankStyle(usagefmt.RankOf(f)).Render(strings.Repeat(filled, n)) +
		lipgloss.NewStyle().Foreground(theme.Current.Border).Render(strings.Repeat(empty, cells-n))
}

func usageValue(value string, color lipgloss.Color) string {
	return usageWords(value, lipgloss.NewStyle().Foreground(color).Bold(true))
}

func usageField(label, value string, color lipgloss.Color) string {
	return usageWords(label, lipgloss.NewStyle().Foreground(theme.Current.Dim)) + " " + usageValue(value, color)
}

// Each word closes its own style so metadata wrapping cannot bleed a value's
// color into the next line's provider rail or lose it on a continuation row.
func usageWords(text string, style lipgloss.Style) string {
	words := strings.Split(text, " ")
	for i, word := range words {
		words[i] = style.Render(word)
	}
	return strings.Join(words, " ")
}

func usageMetadata(s agents.UsageSnapshot, now time.Time) [][]string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	var pools [][]string
	for _, c := range s.Credits {
		label := c.Label
		if label == "" {
			label = "Credits"
		}
		value := strings.TrimSpace(creditText(c))
		color := theme.Current.Success
		if !c.Enabled {
			color = theme.Current.Warning
		}
		if value == "-" {
			value = usageWords(label+" Balance unavailable", dim)
		} else if c.Enabled && !c.Unlimited {
			value = usageField(label, value, color) + dim.Render(" available")
		} else {
			value = usageField(label, value, color)
		}
		date := usageWords("Reset date unavailable", dim)
		if c.ResetsAt != nil {
			date = usageField("Resets", usageDate(*c.ResetsAt, now), theme.Current.Accent)
		}
		pools = append(pools, []string{value, date})
	}
	count := 0
	var first *time.Time
	for _, grant := range s.ResetCredits {
		if grant.Status != "" && grant.Status != "available" || grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
			continue
		}
		count += max(grant.Count, 1)
		if grant.ExpiresAt != nil && (first == nil || grant.ExpiresAt.Before(*first)) {
			first = grant.ExpiresAt
		}
	}
	if count > 0 {
		date := usageWords("Expiry unavailable", dim)
		if first != nil {
			date = usageField("First expires", usageDate(*first, now), theme.Current.Warning)
		}
		countText := usageValue(fmt.Sprint(count), theme.Current.Success) + usageWords(strings.TrimPrefix(plural(count, "reset"), fmt.Sprint(count))+" available", dim)
		pools = append(pools, []string{countText, date})
	}
	return pools
}

func usageDate(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	layout := "2 Jan 15:04"
	if t.Year() != now.Year() {
		layout = "2 Jan 2006 15:04"
	}
	return t.Format(layout)
}

// wrapUsageFields keeps balances and dates together whenever possible. Only
// fields wider than the entire content pane are split at word boundaries.
func wrapUsageFields(fields []string, w int) []string {
	var lines []string
	line := ""
	for _, field := range fields {
		if field == "" {
			continue
		}
		if ansi.StringWidth(field) > w {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			for _, word := range strings.Fields(field) {
				if line != "" && ansi.StringWidth(line+" "+word) > w {
					lines, line = append(lines, line), ""
				}
				if line != "" {
					line += " "
				}
				line += word
			}
			continue
		}
		if line != "" && ansi.StringWidth(line+" · "+field) > w {
			lines, line = append(lines, line), ""
		}
		if line != "" {
			line += " · "
		}
		line += field
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
