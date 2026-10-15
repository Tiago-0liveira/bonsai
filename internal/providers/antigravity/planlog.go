package antigravity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Every plan probe appends what it did to <AccountDir>/plan-probe.log: why it
// ran, each poll of the PTY screen, what agy painted, and how it ended. The
// file may hold the signed-in email and whatever agy showed, so it is 0600
// like the rest of the profile. It is a diagnostic aid; nothing reads it back.
const (
	planLogName    = "plan-probe.log"
	planLogMaxSize = 256 << 10
	// planLogMaxLines bounds one screen dump.
	planLogMaxLines = 40
)

// probeLog is nil-safe: a nil log drops every line.
type probeLog struct {
	mu    sync.Mutex
	f     *os.File
	start time.Time
}

type probeLogKey struct{}

func planLogPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), planLogName)
}

// openProbeLog opens the log for appending. A log over planLogMaxSize is moved
// to plan-probe.log.1 first, so two probes' worth of history is always kept.
// Failing to open it returns nil: logging never stops a probe.
func openProbeLog(path string) *probeLog {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	if info, err := os.Stat(path); err == nil && info.Size() > planLogMaxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	l := &probeLog{f: f, start: time.Now()}
	fmt.Fprintf(f, "\n=== probe %s ===\n", l.start.UTC().Format(time.RFC3339))
	return l
}

func (l *probeLog) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.f, "%s %s\n", time.Since(l.start).Round(time.Millisecond), fmt.Sprintf(format, args...))
}

func (l *probeLog) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.f.Close()
}

// Screen dumps the non-empty lines of a terminal screen, quoted so escape
// leftovers and trailing blanks stay visible.
func (l *probeLog) Screen(lines []string) {
	if l == nil {
		return
	}
	shown := 0
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		if shown == planLogMaxLines {
			l.Printf("  ... more lines not shown")
			return
		}
		l.Printf("  screen[%02d] %q", i, line)
		shown++
	}
	if shown == 0 {
		l.Printf("  screen is empty")
	}
}

func withProbeLog(ctx context.Context, l *probeLog) context.Context {
	return context.WithValue(ctx, probeLogKey{}, l)
}

func probeLogFrom(ctx context.Context) *probeLog {
	l, _ := ctx.Value(probeLogKey{}).(*probeLog)
	return l
}
