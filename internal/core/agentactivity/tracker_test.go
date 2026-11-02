package agentactivity

import (
	"sync"
	"testing"
	"time"
)

// fakeClock fires timers only when advanced, so tests never sleep.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	due     time.Time
	fn      func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: t0} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) StopTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, due: c.now.Add(d), fn: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.due.After(c.now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.fn()
	}
}

func TestTrackerStartsUnknown(t *testing.T) {
	clk := newFakeClock()
	tr := NewTracker(TrackerOptions{Clock: clk})
	got := tr.Current()
	if got.State != Unknown || !got.Since.Equal(t0) || got.Source != SourceLifecycle {
		t.Fatalf("initial = %+v", got)
	}
}

func TestTrackerApplyReportsChange(t *testing.T) {
	clk := newFakeClock()
	tr := NewTracker(TrackerOptions{Clock: clk})
	a, changed := tr.Apply(Signal{Kind: PromptSubmitted, At: t0.Add(time.Second), Source: SourceHook})
	if !changed || a.State != Working {
		t.Fatalf("first apply = %+v changed=%v", a, changed)
	}
	if _, changed := tr.Apply(Signal{Kind: PromptSubmitted, At: t0.Add(2 * time.Second), Source: SourceHook}); changed {
		t.Fatal("repeat reported a change")
	}
	a, changed = tr.Apply(Signal{Kind: ToolStarted, At: t0.Add(3 * time.Second), Source: SourceHook, Detail: "Bash"})
	if !changed || a.Detail != "Bash" {
		t.Fatalf("detail apply = %+v changed=%v", a, changed)
	}
	if tr.Current() != a {
		t.Fatal("Current disagrees with Apply")
	}
}

func TestTrackerProgressMeansTurn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		clk := newFakeClock()
		tr := NewTracker(TrackerOptions{Clock: clk, ProgressMeansTurn: enabled})
		tr.Apply(Signal{Kind: TerminalBusy, At: t0.Add(time.Second), Source: SourceTerminal})
		a, changed := tr.Apply(Signal{Kind: TerminalClear, At: t0.Add(2 * time.Second), Source: SourceTerminal})
		if enabled {
			if !changed || a.State != Idle || !a.Inferred {
				t.Fatalf("enabled: %+v changed=%v", a, changed)
			}
		} else if changed || a.State != Working {
			t.Fatalf("disabled: %+v changed=%v", a, changed)
		}
	}
}

func TestTrackerStall(t *testing.T) {
	clk := newFakeClock()
	var stalled []Activity
	tr := NewTracker(TrackerOptions{
		Clock:      clk,
		StallAfter: time.Minute,
		OnStall:    func(a Activity) { stalled = append(stalled, a) },
	})
	if clk.pending() != 0 {
		t.Fatal("timer armed while unknown")
	}
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	if clk.pending() != 1 {
		t.Fatalf("pending = %d, want 1", clk.pending())
	}

	// Any signal restarts the countdown.
	clk.advance(50 * time.Second)
	tr.Apply(Signal{Kind: ToolStarted, Source: SourceHook})
	clk.advance(50 * time.Second)
	if len(stalled) != 0 || tr.Current().Stalled {
		t.Fatal("stalled despite recent signal")
	}

	prev := tr.Current()
	clk.advance(11 * time.Second)
	if len(stalled) != 1 || !stalled[0].Stalled {
		t.Fatalf("stalled callbacks = %+v", stalled)
	}
	cur := tr.Current()
	if !cur.Stalled || !prev.Changed(cur) {
		t.Fatalf("current = %+v", cur)
	}
	if clk.pending() != 0 {
		t.Fatal("timer still armed after stall")
	}

	// A signal un-stalls and re-arms.
	a, changed := tr.Apply(Signal{Kind: ToolFinished, Source: SourceHook})
	if !changed || a.Stalled || clk.pending() != 1 {
		t.Fatalf("after signal: %+v changed=%v pending=%d", a, changed, clk.pending())
	}
}

func TestTrackerStallOnlyWhileWorking(t *testing.T) {
	clk := newFakeClock()
	tr := NewTracker(TrackerOptions{Clock: clk, StallAfter: time.Minute})
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	tr.Apply(Signal{Kind: PermissionRequested, Source: SourceHook})
	if clk.pending() != 0 {
		t.Fatal("timer armed while waiting")
	}
	clk.advance(time.Hour)
	if tr.Current().Stalled {
		t.Fatal("waiting session stalled")
	}
	tr.Apply(Signal{Kind: TurnCompleted, Source: SourceHook})
	if clk.pending() != 0 {
		t.Fatal("timer armed while idle")
	}
}

func TestTrackerStaleTimerIgnored(t *testing.T) {
	clk := newFakeClock()
	tr := NewTracker(TrackerOptions{Clock: clk, StallAfter: time.Minute})
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	tr.mu.Lock()
	oldGen := tr.gen
	tr.mu.Unlock()
	tr.Apply(Signal{Kind: TurnCompleted, Source: SourceHook})
	tr.stall(oldGen) // a timer that fired just as the state moved on
	if tr.Current().Stalled {
		t.Fatal("stale timer stalled an idle session")
	}
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	tr.stall(oldGen)
	if tr.Current().Stalled {
		t.Fatal("old generation stalled a new turn")
	}
}

func TestTrackerStop(t *testing.T) {
	clk := newFakeClock()
	tr := NewTracker(TrackerOptions{Clock: clk, StallAfter: time.Minute})
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	tr.Stop()
	tr.Stop()
	if clk.pending() != 0 {
		t.Fatal("timer survived Stop")
	}
	if a, _ := tr.Apply(Signal{Kind: ToolStarted, Source: SourceHook}); a.State != Working {
		t.Fatalf("Apply after Stop = %+v", a)
	}
	if clk.pending() != 0 {
		t.Fatal("timer re-armed after Stop")
	}
}

func TestTrackerDefaultsAndRealClock(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	if tr.opts.StallAfter != DefaultStallAfter {
		t.Fatalf("StallAfter = %v", tr.opts.StallAfter)
	}
	tr.Apply(Signal{Kind: PromptSubmitted, Source: SourceHook})
	tr.Stop() // cancels the real timer
}

func TestTrackerConcurrent(t *testing.T) {
	tr := NewTracker(TrackerOptions{StallAfter: time.Millisecond})
	var wg sync.WaitGroup
	kinds := []Kind{PromptSubmitted, ToolStarted, PermissionRequested, ToolFinished, TurnCompleted}
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				tr.Apply(Signal{Kind: kinds[(i+j)%len(kinds)], Source: SourceHook})
				tr.Current()
			}
		}()
	}
	wg.Wait()
	tr.Stop()
}
