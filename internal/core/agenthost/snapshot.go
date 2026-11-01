package agenthost

import (
	"context"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/termscreen"
)

// snapshotTimeout bounds a peek: a stuck holder must not hold a picker open.
const snapshotTimeout = 3 * time.Second

// Snapshot renders the tail of a running session's screen without taking part
// in it. It attaches read-only (never the writer, cannot take control), feeds
// the replay buffer through the terminal emulator and detaches at the replay's
// closing status frame, so the bytes read are bounded by the stream's replay
// limit. An ended session has no holder and yields no lines. The text is
// returned to the caller only: nothing is stored or logged here.
func Snapshot(ctx context.Context, r Record, rows int) ([]string, error) {
	if !r.Alive || !r.Summary.Active() || rows < 1 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	info, err := QueryContext(ctx, r)
	if err != nil {
		return nil, err
	}
	c, _, err := DialContext(ctx, r, Request{Mode: ModeTerminal, Client: "tui", ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()
	_ = c.SetDeadline(time.Now().Add(snapshotTimeout))
	screen := termscreen.New(info.Cols, info.Rows)
	for {
		f, err := c.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		switch f.Type {
		case "output":
			_, _ = screen.Write(f.Data)
		case "status":
			// The replay always ends with a status frame; live output after it
			// is not part of a snapshot.
			return tail(screen.Lines(), rows), nil
		}
	}
}

// tail is the last n non-blank-trailing lines.
func tail(lines []string, n int) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines[max(len(lines)-n, 0):]
}
