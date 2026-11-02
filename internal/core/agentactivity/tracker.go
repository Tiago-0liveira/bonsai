package agentactivity

import (
	"sync"
	"time"
)

// DefaultStallAfter is how long a Working session may stay silent before it is
// flagged Stalled.
const DefaultStallAfter = 10 * time.Minute

// Clock is the time seam for the stall timer.
type Clock interface {
	Now() time.Time
	// AfterFunc runs f once after d, like time.AfterFunc.
	AfterFunc(d time.Duration, f func()) StopTimer
}

// StopTimer cancels a pending AfterFunc.
type StopTimer interface {
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) StopTimer { return time.AfterFunc(d, f) }

// TrackerOptions configures a Tracker. The zero value is usable.
type TrackerOptions struct {
	// StallAfter defaults to DefaultStallAfter.
	StallAfter time.Duration
	// ProgressMeansTurn lets TerminalClear end a turn. Only providers whose
	// progress bar spans exactly one turn (agy) declare it; elsewhere a clear
	// is noise. It is an option, not a provider lookup, so this package stays
	// provider-free.
	ProgressMeansTurn bool
	// Clock defaults to the wall clock.
	Clock Clock
	// OnStall is called from the timer goroutine, with no Tracker lock held,
	// when a Working session turns Stalled.
	OnStall func(Activity)
}

// Tracker holds the activity of one session and owns its stall timer. It is
// safe for concurrent use.
type Tracker struct {
	opts TrackerOptions

	mu      sync.Mutex
	act     Activity
	timer   StopTimer
	gen     uint64 // invalidates timers that fired while Apply was running
	stopped bool
}

// NewTracker starts at Unknown, stamped with the clock.
func NewTracker(opts TrackerOptions) *Tracker {
	if opts.StallAfter <= 0 {
		opts.StallAfter = DefaultStallAfter
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	return &Tracker{
		opts: opts,
		act:  Activity{State: Unknown, Since: opts.Clock.Now(), Source: SourceLifecycle},
	}
}

// Current returns the activity now.
func (t *Tracker) Current() Activity {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.act
}

// Apply reduces s into the activity. changed is true when anything visible
// differs from before, Detail included; use Activity.Changed on the previous
// value to tell a transition from a Detail-only update.
func (t *Tracker) Apply(s Signal) (act Activity, changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.Kind == TerminalClear && !t.opts.ProgressMeansTurn {
		return t.act, false
	}
	prev := t.act
	t.act = Reduce(prev, s, t.opts.Clock.Now())
	t.armLocked()
	return t.act, !sameActivity(prev, t.act)
}

// Stop cancels the stall timer for good; Apply keeps reducing.
func (t *Tracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	t.gen++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

// armLocked restarts the stall countdown. Any signal counts as life, so every
// Apply re-arms; only a Working, not yet stalled session needs a timer.
func (t *Tracker) armLocked() {
	t.gen++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.stopped || t.act.State != Working || t.act.Stalled {
		return
	}
	gen := t.gen
	t.timer = t.opts.Clock.AfterFunc(t.opts.StallAfter, func() { t.stall(gen) })
}

func (t *Tracker) stall(gen uint64) {
	t.mu.Lock()
	if gen != t.gen || t.act.State != Working || t.act.Stalled {
		t.mu.Unlock()
		return
	}
	t.act.Stalled = true
	t.timer = nil
	act := t.act
	t.mu.Unlock()
	if t.opts.OnStall != nil {
		t.opts.OnStall(act)
	}
}

func sameActivity(a, b Activity) bool {
	return a.State == b.State && a.Reason == b.Reason && a.Detail == b.Detail &&
		a.Since.Equal(b.Since) && a.Source == b.Source && a.Stalled == b.Stalled && a.Inferred == b.Inferred
}
