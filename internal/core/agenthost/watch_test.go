//go:build linux || darwin

package agenthost

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/fsnotify/fsnotify"
)

func watchRecord(id, state string) Record {
	return Record{ID: id, Summary: agentterminal.Summary{ID: id, State: state, CreatedAt: time.Now().UTC()}}
}

func expectSignal(t *testing.T, ch <-chan struct{}, within time.Duration, what string) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed", what)
		}
	case <-time.After(within):
		t.Fatalf("%s: no signal within %s", what, within)
	}
}

// quiet drains signals until none arrives for a while, up to a fixed deadline.
func quiet(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	idle := time.NewTimer(300 * time.Millisecond)
	defer idle.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("watch channel closed while waiting for quiet")
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(300 * time.Millisecond)
		case <-idle.C:
			return
		case <-deadline.C:
			t.Fatal("watch did not become quiet")
		}
	}
}

func TestWatchSignalsRecordChanges(t *testing.T) {
	p := testPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(p, watchRecord("sess_a1", "running")); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, ch, time.Second, "create")
	quiet(t, ch)
	if err := writeRecord(p, watchRecord("sess_a1", "exited")); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, ch, time.Second, "rewrite")
	quiet(t, ch)
	if err := remove(p, "sess_a1"); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, ch, time.Second, "remove")
}

func TestWatchRescanSeesDeadHolder(t *testing.T) {
	old := rescanInterval
	rescanInterval = 50 * time.Millisecond
	t.Cleanup(func() { rescanInterval = old })
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockFile(p.lock("sess_b2"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := writeRecord(p, watchRecord("sess_b2", "running")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	quiet(t, ch)
	// The holder dies without rewriting its record: only the lock goes.
	_ = lock.Close()
	expectSignal(t, ch, time.Second, "dead holder")
}

func TestWatchCoalescesBursts(t *testing.T) {
	p := testPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := writeRecord(p, watchRecord("sess_c3", "running")); err != nil {
			t.Fatal(err)
		}
	}
	signals := 0
	for timeout := time.After(700 * time.Millisecond); ; {
		select {
		case <-ch:
			signals++
			continue
		case <-timeout:
		}
		break
	}
	if signals < 1 || signals > 2 {
		t.Fatalf("burst gave %d signals", signals)
	}
}

func TestWatchClosesOnCancelWithoutLeaks(t *testing.T) {
	p := testPaths(t)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Watch(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected signal")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

func activityRecord(id string, a *agentactivity.Activity) Record {
	r := watchRecord(id, "running")
	r.Summary.Activity = a
	return r
}

func TestFingerprintTracksActivityTransitionsOnly(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockFile(p.lock("sess_d4"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	at := time.Now().UTC()
	fingerprintOf := func(a *agentactivity.Activity) string {
		t.Helper()
		if err := writeRecord(p, activityRecord("sess_d4", a)); err != nil {
			t.Fatal(err)
		}
		return fingerprint(p)
	}
	none := fingerprintOf(nil)
	working := fingerprintOf(&agentactivity.Activity{State: agentactivity.Working, Since: at})
	if none == working {
		t.Fatal("activity appearing left the fingerprint alone")
	}
	if got := fingerprintOf(&agentactivity.Activity{State: agentactivity.Working, Detail: "Edit", Since: at.Add(time.Minute), Source: agentactivity.SourceHook}); got != working {
		t.Fatalf("detail and age changed the fingerprint:\n%q\n%q", working, got)
	}
	for name, a := range map[string]*agentactivity.Activity{
		"state":   {State: agentactivity.Idle},
		"reason":  {State: agentactivity.Working, Reason: agentactivity.ReasonQuestion},
		"stalled": {State: agentactivity.Working, Stalled: true},
	} {
		if fingerprintOf(a) == working {
			t.Errorf("%s change left the fingerprint alone", name)
		}
	}
}

// Without file events only the rescan sees a record change, so the activity
// must be part of what it compares.
func TestWatchRescanSeesActivityChange(t *testing.T) {
	oldInterval, oldWatcher := rescanInterval, newFSWatcher
	rescanInterval = 50 * time.Millisecond
	newFSWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("no file events") }
	t.Cleanup(func() { rescanInterval, newFSWatcher = oldInterval, oldWatcher })
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockFile(p.lock("sess_e5"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	unknown := &agentactivity.Activity{State: agentactivity.Unknown}
	if err := writeRecord(p, activityRecord("sess_e5", unknown)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Watch(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	quiet(t, ch)
	if err := writeRecord(p, activityRecord("sess_e5", &agentactivity.Activity{State: agentactivity.Working})); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, ch, time.Second, "activity transition")
	quiet(t, ch)
	if err := writeRecord(p, activityRecord("sess_e5", &agentactivity.Activity{State: agentactivity.Working, Detail: "Bash"})); err != nil {
		t.Fatal(err)
	}
	quiet(t, ch)
}
