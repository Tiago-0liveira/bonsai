package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type progressClock struct{ now time.Time }

func (c *progressClock) Now() time.Time { return c.now }

func progressAccount(provider agents.ProviderID, name string) agents.Account {
	return agents.Account{ID: agents.AccountID(string(provider) + "_" + name), Provider: provider, Name: name}
}

func newTestProgress(color bool, width int) (*usageProgress, *progressClock, *bytes.Buffer) {
	clock := &progressClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	var out bytes.Buffer
	return newUsageProgress(&out, color, func() int { return width }, clock.Now), clock, &out
}

func queueAll(p *usageProgress, accounts ...agents.Account) {
	for _, a := range accounts {
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageQueued})
	}
}

func TestUsageProgressRendersOneLinePerProvider(t *testing.T) {
	p, clock, _ := newTestProgress(false, 100)
	claude := []agents.Account{progressAccount("claude", "a"), progressAccount("claude", "b"), progressAccount("claude", "c")}
	codex := []agents.Account{progressAccount("codex", "p"), progressAccount("codex", "q")}
	var agy []agents.Account
	for _, n := range []string{"work", "side", "qa", "lab", "ci", "dev"} {
		agy = append(agy, progressAccount("antigravity", n))
	}
	queueAll(p, append(append(agy, codex...), claude...)...)

	lines := p.render(clock.now)
	want := []string{
		"C claude       [...]     0/3  queued",
		"X codex        [..]      0/2  queued",
		"A antigravity  [......]  0/6  queued",
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %q", len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	for _, a := range claude {
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageStarted})
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageFinished})
	}
	for _, a := range codex {
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageStarted})
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageFinished})
	}
	for _, a := range agy[:3] {
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageStarted})
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageFinished})
	}
	p.Observe(agents.UsageEvent{Account: agy[3], Phase: agents.UsageStarted})
	clock.now = clock.now.Add(2400 * time.Millisecond)

	lines = p.render(clock.now)
	want = []string{
		"C claude       [###]     3/3  ok 2.4s",
		"X codex        [##]      2/2  ok 2.4s",
		"A antigravity  [###*..]  3/6  | 2.4s  waiting: lab, ci, dev",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestUsageProgressNamesFailures(t *testing.T) {
	p, clock, _ := newTestProgress(false, 100)
	ok, slow, bad := progressAccount("antigravity", "work"), progressAccount("antigravity", "lab"), progressAccount("antigravity", "ci")
	queueAll(p, ok, slow, bad)
	p.Observe(agents.UsageEvent{Account: ok, Phase: agents.UsageFinished})
	p.Observe(agents.UsageEvent{Account: slow, Phase: agents.UsageFinished, Err: agents.ErrUsageUnsupported})
	p.Observe(agents.UsageEvent{Account: bad, Phase: agents.UsageFinished, Err: fmt.Errorf("fetch: %w", context.DeadlineExceeded)})
	clock.now = clock.now.Add(5100 * time.Millisecond)

	got := p.render(clock.now)
	want := "A antigravity  [#=!]  3/3  ! 5.1s  1 failed: ci (timeout)"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUsageProgressCollapsesManyAccountsIntoABar(t *testing.T) {
	p, clock, _ := newTestProgress(false, 100)
	var accounts []agents.Account
	for i := 0; i < 20; i++ {
		accounts = append(accounts, progressAccount("antigravity", fmt.Sprintf("n%02d", i)))
	}
	queueAll(p, accounts...)
	for _, a := range accounts[:10] {
		p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageFinished})
	}
	p.Observe(agents.UsageEvent{Account: accounts[10], Phase: agents.UsageStarted})
	line := p.render(clock.now)[0]
	if !strings.Contains(line, "[######......]  10/20") {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, "waiting: n10, n11, n12 +7") {
		t.Errorf("line = %q", line)
	}
}

func TestUsageProgressFitsTheTerminalWidth(t *testing.T) {
	for _, color := range []bool{false, true} {
		for width := usageProgressMinWidth; width <= 90; width += 5 {
			p, clock, _ := newTestProgress(color, width)
			var accounts []agents.Account
			for _, n := range []string{"work-account-one", "side-account-two", "third-long-account-name"} {
				accounts = append(accounts, progressAccount("antigravity", n))
			}
			queueAll(p, accounts...)
			p.Observe(agents.UsageEvent{Account: accounts[0], Phase: agents.UsageStarted})
			for _, line := range p.render(clock.now) {
				if n := utf8.RuneCountInString(stripUsageANSI(line)); n > width-1 {
					t.Errorf("color=%v width=%d: line is %d wide: %q", color, width, n, line)
				}
			}
		}
	}
	p, clock, _ := newTestProgress(false, usageProgressMinWidth-1)
	queueAll(p, progressAccount("codex", "a"))
	if lines := p.render(clock.now); lines != nil {
		t.Errorf("narrow terminal drew %q", lines)
	}
}

func TestUsageProgressDrawsOnlyWhenSlowAndErasesTheBlock(t *testing.T) {
	p, clock, out := newTestProgress(false, 100)
	a := progressAccount("codex", "a")
	queueAll(p, a)

	// Finishing before the show-after delay leaves the terminal untouched.
	p.Start()
	p.Observe(agents.UsageEvent{Account: a, Phase: agents.UsageFinished})
	p.Stop()
	if out.Len() != 0 {
		t.Fatalf("a fast run wrote %q", out.String())
	}

	p, clock, out = newTestProgress(false, 100)
	queueAll(p, a)
	clock.now = clock.now.Add(time.Second)
	p.mu.Lock()
	p.draw(clock.now)
	p.draw(clock.now)
	p.mu.Unlock()
	if got := strings.Count(out.String(), "\n"); got != 2 {
		t.Errorf("two draws wrote %d lines, want 2: %q", got, out.String())
	}
	if !strings.Contains(out.String(), "\x1b[1A") {
		t.Errorf("second draw did not move back up: %q", out.String())
	}
	out.Reset()
	p.Start()
	p.Stop()
	if got := out.String(); got != "\x1b[1A\x1b[J" {
		t.Errorf("stop wrote %q, want the erase sequence", got)
	}
}

func TestUsageFailureReason(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{fmt.Errorf("wrapped: %w", context.Canceled), "cancelled"},
		{errors.New("boom"), "error"},
	} {
		if got := usageFailureReason(tc.err); got != tc.want {
			t.Errorf("reason(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestUsageProviderBadgeIsOneCell(t *testing.T) {
	for provider, want := range map[agents.ProviderID]string{"claude": "C", "claude-code": "C", "codex": "X", "antigravity": "A", "gemini": "G", "": "?"} {
		if got := usageProviderBadge(provider); got != want {
			t.Errorf("badge(%q) = %q, want %q", provider, got, want)
		}
	}
}
