package agenthost

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// coalesceDelay merges the several file events one record write produces
// (temp file, rename, chmod) into a single signal.
const coalesceDelay = 100 * time.Millisecond

// rescanInterval is a variable so tests can shorten it.
var rescanInterval = 5 * time.Second

// Watch signals whenever the records under p may have changed: a session
// started, changed state or was removed. Signals are coalesced and never
// block, so a slow reader sees at most one pending signal and must List again.
// The channel closes when ctx ends.
//
// A holder that dies without rewriting its record only releases its lock,
// which file events cannot see, so a periodic rescan compares each record's
// state and liveness too. If file events are unavailable (for example the
// inotify limit is reached) the rescan alone drives the signals.
func Watch(ctx context.Context, p Paths) (<-chan struct{}, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	var events <-chan fsnotify.Event
	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		if err = watcher.Add(p.Records); err != nil {
			_ = watcher.Close()
		} else {
			events = watcher.Events
		}
	}
	// Taken after the watcher is armed so no change falls between the two.
	last := fingerprint(p)
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		if events != nil {
			defer watcher.Close()
		}
		signal := func() {
			last = fingerprint(p)
			select {
			case out <- struct{}{}:
			default:
			}
		}
		rescan := time.NewTicker(rescanInterval)
		defer rescan.Stop()
		var pending <-chan time.Time
		var timer *time.Timer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if pending == nil && strings.HasSuffix(filepath.Base(ev.Name), ".json") {
					timer = time.NewTimer(coalesceDelay)
					pending = timer.C
				}
			case <-pending:
				pending = nil
				signal()
			case <-rescan.C:
				if fingerprint(p) != last {
					signal()
				}
			}
		}
	}()
	return out, nil
}

// fingerprint summarizes what a session list shows: which records exist and
// their state and liveness.
func fingerprint(p Paths) string {
	records, _ := List(p)
	var b strings.Builder
	for _, r := range records {
		b.WriteString(r.ID)
		b.WriteByte('|')
		b.WriteString(r.Summary.State)
		if r.Alive {
			b.WriteString("|1\n")
		} else {
			b.WriteString("|0\n")
		}
	}
	return b.String()
}
