package agentactivity

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// EventType says what an Event reports.
type EventType string

const (
	SignalReceived   EventType = "signal"            // every normalized signal (verbose, holder only)
	ActivityChanged  EventType = "activity_changed"  // transitions only
	LifecycleChanged EventType = "lifecycle_changed" // starting, running, exited ...
)

// SessionRef identifies the session an event is about. Provider and AccountID
// are plain strings so this package stays free of the agents types.
type SessionRef struct {
	ID, ProjectID, WorktreeID, Name, Origin string
	Provider, AccountID                     string
}

// Event is one thing that happened to a session. Pointer fields are shared
// with every subscriber: neither publisher nor subscriber may mutate them.
type Event struct {
	Type      EventType
	At        time.Time
	Session   SessionRef
	Signal    *Signal   // SignalReceived
	From, To  *Activity // ActivityChanged
	Lifecycle string    // LifecycleChanged: new lifecycle state
}

// Filter selects events. A nil Filter matches everything.
type Filter func(Event) bool

// queueSize bounds each subscriber's backlog.
const queueSize = 256

// Bus fans events out to subscribers. The zero value is ready to use. One Bus
// lives in each process that cares (a holder, the API).
type Bus struct {
	// OnProblem, if set before first use, hears about subscriber panics
	// (ErrSubscriberPanic) and dropped events (ErrQueueOverflow). It runs
	// without bus locks but on the publisher or subscriber goroutine, so it
	// must be quick and must not call back into the Bus.
	OnProblem func(subscriber string, err error)

	mu   sync.Mutex
	subs []*subscriber
}

type subscriber struct {
	name   string
	filter Filter
	fn     func(Event)
	bus    *Bus

	mu     sync.Mutex
	queue  []Event
	closed bool
	wake   chan struct{} // capacity 1: "queue changed or closed"
}

// Subscribe registers fn for events matching filter. Every subscriber has its
// own goroutine and bounded queue (queueSize); a slow subscriber loses its
// oldest events and never blocks the publisher. fn is called serially and in
// order. cancel is idempotent; after it returns fn is not started again, but
// a call already running finishes. cancel is safe to call from fn.
func (b *Bus) Subscribe(name string, filter Filter, fn func(Event)) (cancel func()) {
	s := &subscriber{name: name, filter: filter, fn: fn, bus: b, wake: make(chan struct{}, 1)}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	go s.run()
	return func() {
		b.mu.Lock()
		if i := slices.Index(b.subs, s); i >= 0 {
			b.subs = slices.Delete(b.subs, i, i+1)
		}
		b.mu.Unlock()
		s.close()
	}
}

// Publish hands e to every matching subscriber without ever blocking on one.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	subs := slices.Clone(b.subs)
	b.mu.Unlock()
	for _, s := range subs {
		if s.filter == nil || s.filter(e) {
			s.push(e)
		}
	}
}

func (s *subscriber) push(e Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	dropped := false
	if len(s.queue) >= queueSize {
		// Newest state matters most to a notifier; the oldest event is the one
		// least likely to still be true.
		s.queue = append(s.queue[:0], s.queue[1:]...)
		dropped = true
	}
	s.queue = append(s.queue, e)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if dropped {
		s.bus.problem(s.name, ErrQueueOverflow)
	}
}

func (s *subscriber) close() {
	s.mu.Lock()
	s.closed = true
	s.queue = nil
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *subscriber) run() {
	for range s.wake {
		for {
			e, ok, closed := s.pop()
			if closed {
				return
			}
			if !ok {
				break
			}
			s.call(e)
		}
	}
}

func (s *subscriber) pop() (e Event, ok, closed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Event{}, false, true
	}
	if len(s.queue) == 0 {
		return Event{}, false, false
	}
	e = s.queue[0]
	s.queue = s.queue[1:]
	return e, true, false
}

func (s *subscriber) call(e Event) {
	defer func() {
		if r := recover(); r != nil {
			s.bus.problem(s.name, fmt.Errorf("%w: %v", ErrSubscriberPanic, r))
		}
	}()
	s.fn(e)
}

func (b *Bus) problem(name string, err error) {
	if b.OnProblem != nil {
		b.OnProblem(name, err)
	}
}

// Became matches ActivityChanged events that moved into one of states.
func Became(states ...State) Filter {
	return func(e Event) bool {
		return e.Type == ActivityChanged && e.To != nil && slices.Contains(states, e.To.State)
	}
}

// Lifecycle matches LifecycleChanged events into one of states.
func Lifecycle(states ...string) Filter {
	return func(e Event) bool {
		return e.Type == LifecycleChanged && slices.Contains(states, e.Lifecycle)
	}
}

// Project matches events of one project.
func Project(id string) Filter {
	return func(e Event) bool { return e.Session.ProjectID == id }
}

// All matches when every filter matches; nil filters are skipped.
func All(filters ...Filter) Filter {
	return func(e Event) bool {
		for _, f := range filters {
			if f != nil && !f(e) {
				return false
			}
		}
		return true
	}
}
