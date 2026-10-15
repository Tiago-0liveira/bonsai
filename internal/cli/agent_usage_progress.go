package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

const (
	// usageProgressShowAfter keeps fast, cached reads from flickering a block.
	usageProgressShowAfter = 300 * time.Millisecond
	usageProgressTick      = 100 * time.Millisecond
	usageProgressMinWidth  = 40
	// usageProgressMaxCells is the most accounts drawn one cell each; more
	// collapse into a proportional bar.
	usageProgressMaxCells = 12
	usageProgressWaiting  = 3
	usageProgressFailed   = 2
	usageProgressName     = 14
)

var (
	usageSpinner      = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	usageSpinnerASCII = []string{"|", "/", "-", "\\"}
)

type usageCellState int

const (
	usageCellQueued usageCellState = iota
	usageCellRunning
	usageCellDone
	usageCellFailed
	// usageCellSkipped is an account whose provider has no usage to read. It is
	// finished, not a failure.
	usageCellSkipped
)

type usageProgressAccount struct {
	name  string
	state usageCellState
	err   error
}

// usageProgress is the live block bonsai agent usage draws on stderr while the
// accounts resolve: one line per provider, one cell per account.
type usageProgress struct {
	out   io.Writer
	color bool
	width func() int
	clock func() time.Time

	mu        sync.Mutex
	begin     time.Time
	providers []agents.ProviderID
	accounts  map[agents.ProviderID][]*usageProgressAccount
	index     map[agents.AccountID]*usageProgressAccount
	frame     int
	drawn     int

	stop chan struct{}
	done chan struct{}
}

func newUsageProgress(out io.Writer, color bool, width func() int, clock func() time.Time) *usageProgress {
	return &usageProgress{
		out:      out,
		color:    color,
		width:    width,
		clock:    clock,
		begin:    clock(),
		accounts: map[agents.ProviderID][]*usageProgressAccount{},
		index:    map[agents.AccountID]*usageProgressAccount{},
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// usageProgressEnabled reports whether the live block may draw to w: a real
// terminal, not dumb, and not turned off by --no-progress or BONSAI_PROGRESS=0.
func usageProgressEnabled(w io.Writer, disabled bool) bool {
	if disabled || os.Getenv("BONSAI_PROGRESS") == "0" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return false
	}
	return terminalWidth(w) >= usageProgressMinWidth
}

func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return cols
}

// Observe is the AllObserved callback.
func (p *usageProgress) Observe(event agents.UsageEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	account := p.lookup(event.Account)
	switch event.Phase {
	case agents.UsageStarted:
		account.state = usageCellRunning
	case agents.UsageFinished:
		account.err = event.Err
		switch {
		case event.Err == nil:
			account.state = usageCellDone
		case errors.Is(event.Err, agents.ErrUsageUnsupported):
			account.state = usageCellSkipped
		default:
			account.state = usageCellFailed
		}
	}
}

func (p *usageProgress) lookup(a agents.Account) *usageProgressAccount {
	if account, ok := p.index[a.ID]; ok {
		return account
	}
	account := &usageProgressAccount{name: a.Name}
	p.index[a.ID] = account
	if _, ok := p.accounts[a.Provider]; !ok {
		p.providers = append(p.providers, a.Provider)
		sort.SliceStable(p.providers, func(i, j int) bool {
			return usageProviderRank(p.providers[i]) < usageProviderRank(p.providers[j])
		})
	}
	p.accounts[a.Provider] = append(p.accounts[a.Provider], account)
	return account
}

// Start begins redrawing the block in the background.
func (p *usageProgress) Start() {
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(usageProgressTick)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				if now := p.clock(); now.Sub(p.begin) >= usageProgressShowAfter {
					p.draw(now)
				}
				p.frame++
				p.mu.Unlock()
			}
		}
	}()
}

// Stop ends the redraws and erases the block, so the dashboard prints where it
// stood.
func (p *usageProgress) Stop() {
	close(p.stop)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.drawn > 0 {
		fmt.Fprintf(p.out, "\x1b[%dA\x1b[J", p.drawn)
		p.drawn = 0
	}
}

func (p *usageProgress) draw(now time.Time) {
	lines := p.render(now)
	if len(lines) == 0 {
		return
	}
	var b strings.Builder
	if p.drawn > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", p.drawn)
	}
	for _, line := range lines {
		b.WriteString("\r\x1b[2K" + line + "\n")
	}
	p.drawn = len(lines)
	io.WriteString(p.out, b.String())
}

// usageSegment is a piece of a line in two forms: plain, to measure, and
// styled, to print.
type usageSegment struct{ plain, styled string }

// render returns one line per provider, each fitted to the terminal width. It
// returns none when the terminal is too narrow, since a wrapped line would
// break the redraw.
func (p *usageProgress) render(now time.Time) []string {
	width := p.width()
	if width < usageProgressMinWidth || len(p.providers) == 0 {
		return nil
	}
	nameWidth, cellWidth, countWidth := 0, 0, 0
	for _, provider := range p.providers {
		nameWidth = max(nameWidth, utf8.RuneCountInString(string(provider)))
		cells, _ := p.cells(p.accounts[provider])
		cellWidth = max(cellWidth, utf8.RuneCountInString(cells.plain))
		countWidth = max(countWidth, len(fmt.Sprintf("%d/%d", len(p.accounts[provider]), len(p.accounts[provider]))))
	}
	lines := make([]string, 0, len(p.providers))
	for _, provider := range p.providers {
		lines = append(lines, p.line(provider, now, width, nameWidth, cellWidth, countWidth))
	}
	return lines
}

func (p *usageProgress) line(provider agents.ProviderID, now time.Time, width, nameWidth, cellWidth, countWidth int) string {
	accounts := p.accounts[provider]
	cells, finished := p.cells(accounts)
	total := len(accounts)
	elapsed := usageProgressDuration(now.Sub(p.begin))

	badge := usageProviderBadge(provider)
	head := usageSegment{
		plain:  badge + " " + padUsage(string(provider), nameWidth) + "  " + padUsage(cells.plain, cellWidth) + "  " + padUsage(fmt.Sprintf("%d/%d", finished, total), countWidth),
		styled: usageStyle(badge, usageProviderColor(provider), p.color) + " " + padUsage(string(provider), nameWidth) + "  " + cells.styled + strings.Repeat(" ", cellWidth-utf8.RuneCountInString(cells.plain)) + "  " + padUsage(fmt.Sprintf("%d/%d", finished, total), countWidth),
	}
	status, tail := p.status(accounts, finished, elapsed)
	parts := []usageSegment{head, status, tail}
	for len(parts) > 1 {
		if usageSegmentsWidth(parts) <= width-1 {
			break
		}
		parts = parts[:len(parts)-1]
	}
	var styled []string
	for _, part := range parts {
		if part.plain != "" {
			styled = append(styled, part.styled)
		}
	}
	return strings.Join(styled, "  ")
}

func usageSegmentsWidth(parts []usageSegment) int {
	width := 0
	for i, part := range parts {
		if part.plain == "" {
			continue
		}
		if i > 0 {
			width += 2
		}
		width += utf8.RuneCountInString(part.plain)
	}
	return width
}

// cells draws one provider's accounts and counts how many have finished.
func (p *usageProgress) cells(accounts []*usageProgressAccount) (usageSegment, int) {
	finished := 0
	for _, account := range accounts {
		if account.state >= usageCellDone {
			finished++
		}
	}
	ascii := !p.color
	if len(accounts) > usageProgressMaxCells {
		const barWidth = usageProgressMaxCells
		filled := finished * barWidth / len(accounts)
		full, empty := "█", "░"
		if ascii {
			full, empty = "#", "."
		}
		bar := strings.Repeat(full, filled) + strings.Repeat(empty, barWidth-filled)
		return usageSegment{plain: "[" + bar + "]", styled: "[" + usageStyle(bar, "\x1b[32m", p.color) + "]"}, finished
	}
	var plain, styled strings.Builder
	if ascii {
		plain.WriteString("[")
		styled.WriteString("[")
	}
	for _, account := range accounts {
		glyph, code := p.cell(account.state, ascii)
		plain.WriteString(glyph)
		styled.WriteString(usageStyle(glyph, code, p.color))
	}
	if ascii {
		plain.WriteString("]")
		styled.WriteString("]")
	}
	return usageSegment{plain: plain.String(), styled: styled.String()}, finished
}

func (p *usageProgress) cell(state usageCellState, ascii bool) (glyph, code string) {
	pulse := p.frame%2 == 0
	switch state {
	case usageCellDone:
		glyph, code = "▰", "\x1b[32m"
		if ascii {
			glyph = "#"
		}
	case usageCellFailed:
		glyph, code = "▰", "\x1b[31m"
		if ascii {
			glyph = "!"
		}
	case usageCellSkipped:
		glyph, code = "▰", "\x1b[2m"
		if ascii {
			glyph = "="
		}
	case usageCellRunning:
		glyph, code = "▰", "\x1b[33m"
		if !pulse {
			glyph = "▱"
		}
		if ascii {
			glyph = "*"
			if !pulse {
				glyph = "+"
			}
		}
	default:
		glyph, code = "▱", "\x1b[2m"
		if ascii {
			glyph = "."
		}
	}
	return glyph, code
}

// status is the state column and, after it, who is still waited on or what
// failed.
func (p *usageProgress) status(accounts []*usageProgressAccount, finished int, elapsed string) (status, tail usageSegment) {
	var failed, pending []*usageProgressAccount
	started := false
	for _, account := range accounts {
		switch account.state {
		case usageCellFailed:
			failed = append(failed, account)
		case usageCellQueued, usageCellRunning:
			pending = append(pending, account)
		}
		if account.state != usageCellQueued {
			started = true
		}
	}
	switch {
	case len(pending) > 0 && !started:
		return usageSegment{plain: "queued", styled: usageStyle("queued", "\x1b[2m", p.color)}, usageSegment{}
	case len(pending) > 0:
		spinner := usageSpinner
		if !p.color {
			spinner = usageSpinnerASCII
		}
		text := spinner[p.frame%len(spinner)] + " " + elapsed
		status = usageSegment{plain: text, styled: usageStyle(text, "\x1b[33m", p.color)}
		text = "waiting: " + usageNames(pending, usageProgressWaiting, false)
		return status, usageSegment{plain: text, styled: usageStyle(text, "\x1b[2m", p.color)}
	case len(failed) > 0:
		text := "! " + elapsed
		status = usageSegment{plain: text, styled: usageStyle(text, "\x1b[31m", p.color)}
		text = fmt.Sprintf("%d failed: %s", len(failed), usageNames(failed, usageProgressFailed, true))
		return status, usageSegment{plain: text, styled: usageStyle(text, "\x1b[31m", p.color)}
	default:
		text := "✓ " + elapsed
		if !p.color {
			text = "ok " + elapsed
		}
		return usageSegment{plain: text, styled: usageStyle(text, "\x1b[32m", p.color)}, usageSegment{}
	}
}

// usageNames lists up to limit account names, then how many more. With reasons
// each name is followed by why it failed.
func usageNames(accounts []*usageProgressAccount, limit int, reasons bool) string {
	var parts []string
	for i, account := range accounts {
		if i == limit {
			break
		}
		name := truncateRunes(account.name, usageProgressName)
		if reasons {
			name += " (" + usageFailureReason(account.err) + ")"
		}
		parts = append(parts, name)
	}
	out := strings.Join(parts, ", ")
	if len(accounts) > limit {
		out += fmt.Sprintf(" +%d", len(accounts)-limit)
	}
	return out
}

func usageFailureReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "error"
	}
}

func usageProgressDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// usageProviderBadge is the one-cell marker of a provider.
func usageProviderBadge(provider agents.ProviderID) string {
	switch provider {
	case "claude", "claude-code":
		return "C"
	case "codex":
		return "X"
	case "antigravity":
		return "A"
	}
	if r, _ := utf8.DecodeRuneInString(string(provider)); r != utf8.RuneError {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func usageProviderColor(provider agents.ProviderID) string {
	switch provider {
	case "claude", "claude-code":
		return "\x1b[38;5;208m"
	case "codex":
		return "\x1b[38;5;250m"
	case "antigravity":
		return "\x1b[38;5;75m"
	}
	return "\x1b[38;5;141m"
}
