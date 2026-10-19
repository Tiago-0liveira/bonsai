package agentactivity

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func evt(typ EventType) Event { return Event{Type: typ, At: t0} }

func changedTo(project string, s State) Event {
	return Event{Type: ActivityChanged, Session: SessionRef{ProjectID: project}, To: &Activity{State: s}}
}

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(waitFor):
		t.Fatal("timeout waiting for event")
		return Event{}
	}
}

func TestBusDeliversInOrder(t *testing.T) {
	var b Bus
	ch := make(chan Event, 16)
	cancel := b.Subscribe("all", nil, func(e Event) { ch <- e })
	defer cancel()
	for i := range 10 {
		b.Publish(Event{Type: SignalReceived, Lifecycle: string(rune('a' + i))})
	}
	for i := range 10 {
		if got := recv(t, ch).Lifecycle; got != string(rune('a'+i)) {
			t.Fatalf("event %d = %q", i, got)
		}
	}
}

func TestBusFiltersPerSubscriber(t *testing.T) {
	var b Bus
	waiting, other := make(chan Event, 8), make(chan Event, 8)
	defer b.Subscribe("waiting", Became(Waiting), func(e Event) { waiting <- e })()
	defer b.Subscribe("other", Project("p2"), func(e Event) { other <- e })()
	b.Publish(changedTo("p1", Working))
	b.Publish(changedTo("p1", Waiting))
	b.Publish(changedTo("p2", Idle))
	if e := recv(t, waiting); e.To.State != Waiting {
		t.Fatalf("waiting got %+v", e)
	}
	if e := recv(t, other); e.Session.ProjectID != "p2" {
		t.Fatalf("other got %+v", e)
	}
}

func TestBusSlowSubscriberDropsOldestAndNeverBlocks(t *testing.T) {
	var problems atomic.Int64
	b := &Bus{OnProblem: func(name string, err error) {
		if name == "slow" && errors.Is(err, ErrQueueOverflow) {
			problems.Add(1)
		}
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	got := make(chan int, 1024)
	var once sync.Once
	cancel := b.Subscribe("slow", nil, func(e Event) {
		once.Do(func() { close(started) })
		<-release
		got <- int(e.At.Sub(t0) / time.Second)
	})
	defer cancel()

	at := func(i int) Event { return Event{Type: SignalReceived, At: t0.Add(time.Duration(i) * time.Second)} }
	b.Publish(at(0))
	<-started // event 0 is in flight, the queue is empty

	done := make(chan struct{})
	const total = queueSize + 50
	go func() {
		for i := 1; i <= total; i++ {
			b.Publish(at(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(waitFor):
		t.Fatal("publisher blocked by a slow subscriber")
	}
	close(release)

	// The in-flight event, then the newest queueSize events.
	var seq []int
	for range queueSize + 1 {
		select {
		case v := <-got:
			seq = append(seq, v)
		case <-time.After(waitFor):
			t.Fatalf("got %d events", len(seq))
		}
	}
	if seq[0] != 0 || seq[1] != total-queueSize+1 || seq[len(seq)-1] != total {
		t.Fatalf("delivered %d..%d, first=%d, want oldest dropped", seq[1], seq[len(seq)-1], seq[0])
	}
	if problems.Load() != total-queueSize {
		t.Fatalf("overflow reports = %d, want %d", problems.Load(), total-queueSize)
	}
	select {
	case v := <-got:
		t.Fatalf("extra event %d", v)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBusCancelIdempotentAndStopsDelivery(t *testing.T) {
	var b Bus
	var calls atomic.Int64
	ch := make(chan Event, 4)
	cancel := b.Subscribe("c", nil, func(e Event) { calls.Add(1); ch <- e })
	b.Publish(evt(SignalReceived))
	recv(t, ch)
	cancel()
	cancel()
	b.Publish(evt(SignalReceived))
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	b.mu.Lock()
	n := len(b.subs)
	b.mu.Unlock()
	if n != 0 {
		t.Fatalf("subscribers left = %d", n)
	}
}

func TestBusCancelDiscardsQueuedAndWorksFromCallback(t *testing.T) {
	var b Bus
	var calls atomic.Int64
	var cancel func()
	started := make(chan struct{})
	finished := make(chan struct{})
	cancel = b.Subscribe("self", nil, func(Event) {
		calls.Add(1)
		close(started)
		cancel() // must not deadlock
		close(finished)
	})
	b.Publish(evt(SignalReceived))
	b.Publish(evt(SignalReceived))
	b.Publish(evt(SignalReceived))
	<-started
	<-finished
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestBusPanickingSubscriberIsContained(t *testing.T) {
	problems := make(chan error, 4)
	b := &Bus{OnProblem: func(_ string, err error) { problems <- err }}
	ch := make(chan Event, 4)
	defer b.Subscribe("boom", nil, func(e Event) {
		if e.Lifecycle == "bad" {
			panic("kaboom")
		}
		ch <- e
	})()
	b.Publish(Event{Type: LifecycleChanged, Lifecycle: "bad"})
	b.Publish(Event{Type: LifecycleChanged, Lifecycle: "good"})
	if err := <-problems; !errors.Is(err, ErrSubscriberPanic) {
		t.Fatalf("problem = %v", err)
	}
	if e := recv(t, ch); e.Lifecycle != "good" {
		t.Fatalf("event after panic = %+v", e)
	}
}

func TestBusNoProblemHandlerIsFine(t *testing.T) {
	var b Bus
	ch := make(chan Event, 1)
	defer b.Subscribe("quiet", nil, func(e Event) {
		ch <- e
		panic("ignored")
	})()
	b.Publish(evt(SignalReceived))
	recv(t, ch)
}

func TestBusConcurrentPublishSubscribe(t *testing.T) {
	var b Bus
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				cancel := b.Subscribe("churn", nil, func(Event) {})
				b.Publish(evt(SignalReceived))
				cancel()
			}
		}()
	}
	wg.Wait()
}

func TestFilters(t *testing.T) {
	ev := Event{
		Type:      LifecycleChanged,
		Lifecycle: "exited",
		Session:   SessionRef{ProjectID: "p1"},
	}
	act := changedTo("p1", Waiting)
	tests := []struct {
		name string
		f    Filter
		e    Event
		want bool
	}{
		{"became match", Became(Waiting, Idle), act, true},
		{"became other state", Became(Idle), act, false},
		{"became wrong type", Became(Waiting), Event{Type: SignalReceived, To: &Activity{State: Waiting}}, false},
		{"became nil To", Became(Waiting), Event{Type: ActivityChanged}, false},
		{"lifecycle match", Lifecycle("exited", "failed"), ev, true},
		{"lifecycle other", Lifecycle("running"), ev, false},
		{"lifecycle wrong type", Lifecycle("exited"), Event{Type: ActivityChanged, Lifecycle: "exited"}, false},
		{"project match", Project("p1"), ev, true},
		{"project other", Project("p2"), ev, false},
		{"all match", All(Lifecycle("exited"), Project("p1")), ev, true},
		{"all one fails", All(Lifecycle("exited"), Project("p2")), ev, false},
		{"all skips nil", All(nil, Project("p1")), ev, true},
		{"all empty", All(), ev, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f(tt.e); got != tt.want {
				t.Fatalf("filter = %v, want %v", got, tt.want)
			}
		})
	}
}
