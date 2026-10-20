//go:build linux || darwin

package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

const oscBusy = `printf '\033]9;4;3\007'`

type screenClassifierFunc func([]string) (agentactivity.Signal, bool)

func (f screenClassifierFunc) ClassifyTerminalScreen(lines []string) (agentactivity.Signal, bool) {
	return f(lines)
}

func TestHostInfersInputFromRenderedDialog(t *testing.T) {
	a := newActivityHost(t)
	a.lifecycle(t, "running")
	a.h.screenClassifier = screenClassifierFunc(func(lines []string) (agentactivity.Signal, bool) {
		if strings.Contains(strings.Join(lines, "\n"), "QUESTION\nSECRET-PROMPT\nSELECT") {
			return agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, true
		}
		return agentactivity.Signal{}, false
	})
	var output agentactivity.OutputObserver
	a.h.observe(&output, []byte("\x1b[2J\x1b[HQUEST"))
	if got := a.record(t).Summary.Activity; got.State == agentactivity.Waiting {
		t.Fatal("partial dialog inferred waiting")
	}
	a.h.observe(&output, []byte("ION\r\nSECRET-PROMPT\r\nSELECT"))
	got := a.record(t).Summary.Activity
	if got.State != agentactivity.Waiting || got.Reason != agentactivity.ReasonQuestion || !got.Inferred || got.Source != agentactivity.SourceTerminal {
		t.Fatalf("rendered question: %+v", got)
	}
	if bytes.Contains(a.recordBytes(t), []byte("SECRET")) {
		t.Fatal("dialog contents leaked into record")
	}
	a.signal(agentactivity.UserSubmitted, agentactivity.SourceInput, "")
	a.h.observe(&output, []byte("\x1b]9;4;3\x07"))
	if got := a.record(t).Summary.Activity; got.State != agentactivity.Working {
		t.Fatalf("unchanged dialog resurrected answered question: %+v", got)
	}
	a.h.observe(&output, []byte("\x1b[2J\x1b[HWorking"))
	a.h.observe(&output, []byte("\x1b[2J\x1b[HQUESTION\r\nSECRET-PROMPT\r\nSELECT"))
	if got := a.record(t).Summary.Activity; got.State != agentactivity.Waiting {
		t.Fatalf("new dialog not detected: %+v", got)
	}
}

type testClock struct{ ns atomic.Int64 }

func (c *testClock) now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *testClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

// activityHost is a holder object without a process: tests drive its
// lifecycle and signals directly and watch what it publishes.
type activityHost struct {
	h       *host
	paths   Paths
	clock   *testClock
	monitor *serverConn
	events  chan agentactivity.Event
}

func newActivityHost(t *testing.T) *activityHost {
	t.Helper()
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	// A record whose holder holds no lock reads back as lost.
	lock, err := lockFile(p.lock("sess_a1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	summary := Summary{ID: "sess_a1", ProjectID: "project", WorktreeID: "tree", Provider: testProviderID, State: "starting", CreatedAt: time.Now().UTC()}
	h := newHost(Deps{Paths: p}, "gen", agentterminal.NewStream[Summary](80, 24), t.TempDir(), func() {}, summary)
	t.Cleanup(h.tracker.Stop)
	clock := &testClock{}
	clock.ns.Store(time.Now().UnixNano())
	h.now = clock.now
	monitor := &serverConn{out: make(chan Frame, 64), gone: make(chan struct{})}
	h.monitors[monitor] = struct{}{}
	events := make(chan agentactivity.Event, 64)
	cancel := h.bus.Subscribe("test", nil, func(e agentactivity.Event) { events <- e })
	t.Cleanup(cancel)
	return &activityHost{h: h, paths: p, clock: clock, monitor: monitor, events: events}
}

func (a *activityHost) lifecycle(t *testing.T, state string) {
	t.Helper()
	if err := a.h.update(func(s *Summary) error { s.State = state; return nil }); err != nil {
		t.Fatal(err)
	}
}

func (a *activityHost) signal(kind agentactivity.Kind, source agentactivity.Source, detail string) {
	a.h.applyActivity(agentactivity.Signal{Kind: kind, Source: source, Detail: detail})
}

// frames returns the frames queued to the monitor so far.
func (a *activityHost) frames() []Frame {
	var out []Frame
	for {
		select {
		case f := <-a.monitor.out:
			out = append(out, f)
		default:
			return out
		}
	}
}

func (a *activityHost) record(t *testing.T) Record {
	t.Helper()
	r, err := Read(a.paths, "sess_a1")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (a *activityHost) recordBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(a.paths.record("sess_a1"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (a *activityHost) next(t *testing.T, typ agentactivity.EventType) agentactivity.Event {
	t.Helper()
	select {
	case e := <-a.events:
		if e.Type != typ {
			t.Fatalf("event %s (%+v), want %s", e.Type, e, typ)
		}
		return e
	case <-time.After(2 * time.Second):
		t.Fatalf("no %s event", typ)
		return agentactivity.Event{}
	}
}

func TestHostActivityFollowsLifecycle(t *testing.T) {
	a := newActivityHost(t)
	a.signal(agentactivity.TerminalBusy, agentactivity.SourceTerminal, "")
	if a.h.summary.Activity != nil {
		t.Fatalf("activity before running: %+v", a.h.summary.Activity)
	}
	a.lifecycle(t, "running")
	if act := a.h.summary.Activity; act == nil || act.State != agentactivity.Unknown {
		t.Fatalf("running summary: %+v", act)
	}
	if e := a.next(t, agentactivity.LifecycleChanged); e.Lifecycle != "running" || e.Session.ID != "sess_a1" || e.Session.ProjectID != "project" || e.Session.Provider != string(testProviderID) {
		t.Fatalf("lifecycle event %+v", e)
	}
	a.signal(agentactivity.TerminalBusy, agentactivity.SourceTerminal, "")
	if act := a.record(t).Summary.Activity; act == nil || act.State != agentactivity.Working {
		t.Fatalf("record activity %+v", act)
	}

	a.h.stop("")
	if a.h.summary.Activity != nil || a.h.summary.State != "stopping" {
		t.Fatalf("stopping summary %+v", a.h.summary)
	}
	if r := a.record(t); r.Summary.Activity != nil || r.Summary.State != "stopping" {
		t.Fatalf("stopping record %+v", r.Summary)
	}
	a.signal(agentactivity.PermissionRequested, agentactivity.SourceHook, "")
	if a.h.summary.Activity != nil {
		t.Fatal("signal after stop set activity")
	}
	a.lifecycle(t, "exited")
	if a.h.summary.Activity != nil || a.record(t).Summary.Activity != nil {
		t.Fatal("activity survived the end")
	}
}

func TestHostKeepsHookSignalFromBeforeRunning(t *testing.T) {
	// The agent starts before the holder marks it running, and the spool tail
	// reads each line once: a hook that reports in that window must still count
	// once the session runs, and a terminal signal must not.
	a := newActivityHost(t)
	a.signal(agentactivity.PromptSubmitted, agentactivity.SourceHook, "")
	a.signal(agentactivity.TerminalBusy, agentactivity.SourceTerminal, "")
	if a.h.summary.Activity != nil {
		t.Fatalf("activity published before running: %+v", a.h.summary.Activity)
	}
	if _, err := Read(a.paths, "sess_a1"); err == nil {
		t.Fatal("a record was written for a signal before running")
	}
	a.lifecycle(t, "running")
	if act := a.h.summary.Activity; act == nil || act.State != agentactivity.Working || act.Source != agentactivity.SourceHook || act.Inferred {
		t.Fatalf("running summary %+v, want the hook's working", act)
	}
}

func TestHostActivityTransitionsWriteRecordsAndDetailDoesNot(t *testing.T) {
	a := newActivityHost(t)
	a.lifecycle(t, "running")
	a.next(t, agentactivity.LifecycleChanged)
	a.frames()

	a.signal(agentactivity.ToolStarted, agentactivity.SourceHook, "Bash")
	e := a.next(t, agentactivity.ActivityChanged)
	if e.From == nil || e.From.State != agentactivity.Unknown || e.To == nil || e.To.State != agentactivity.Working {
		t.Fatalf("transition event %+v", e)
	}
	frames := a.frames()
	if len(frames) != 1 || frames[0].Type != "activity" || frames[0].Status.Activity.State != agentactivity.Working {
		t.Fatalf("transition frames %+v", frames)
	}
	written := a.recordBytes(t)
	if r := a.record(t); r.Summary.Activity == nil || r.Summary.Activity.Detail != "Bash" {
		t.Fatalf("record %+v", r.Summary.Activity)
	}

	// Detail-only: sockets see it (rate limited), the disk and bus do not.
	a.signal(agentactivity.ToolStarted, agentactivity.SourceHook, "Edit")
	frames = a.frames()
	if len(frames) != 1 || frames[0].Type != "activity" || frames[0].Status.Activity.Detail != "Edit" {
		t.Fatalf("detail frames %+v", frames)
	}
	a.signal(agentactivity.ToolStarted, agentactivity.SourceHook, "Write")
	if frames = a.frames(); len(frames) != 0 {
		t.Fatalf("detail change inside the interval was sent: %+v", frames)
	}
	if got := a.h.summary.Activity.Detail; got != "Write" {
		t.Fatalf("held summary detail %q", got)
	}
	a.clock.advance(detailInterval)
	a.signal(agentactivity.ToolStarted, agentactivity.SourceHook, "Read")
	frames = a.frames()
	if len(frames) != 1 || frames[0].Status.Activity.Detail != "Read" {
		t.Fatalf("detail frames after the interval %+v", frames)
	}
	if !bytes.Equal(written, a.recordBytes(t)) {
		t.Fatal("detail-only changes rewrote the record")
	}

	// The next event on the bus is the next transition, so no detail change
	// published one in between.
	a.signal(agentactivity.PermissionRequested, agentactivity.SourceHook, "Bash")
	if e := a.next(t, agentactivity.ActivityChanged); e.To.State != agentactivity.Waiting || e.To.Reason != agentactivity.ReasonPermission {
		t.Fatalf("waiting event %+v", e)
	}
	if bytes.Equal(written, a.recordBytes(t)) {
		t.Fatal("transition left the record alone")
	}
	frames = a.frames()
	if len(frames) != 1 || frames[0].Status.Activity.State != agentactivity.Waiting {
		t.Fatalf("waiting frames %+v", frames)
	}

	// The user answering moves a waiting agent on, inferred.
	a.signal(agentactivity.UserSubmitted, agentactivity.SourceInput, "")
	if e := a.next(t, agentactivity.ActivityChanged); e.To.State != agentactivity.Working || !e.To.Inferred {
		t.Fatalf("answered event %+v", e)
	}
}

func TestHostStallIsATransition(t *testing.T) {
	a := newActivityHost(t)
	a.h.tracker.Stop()
	a.h.tracker = agentactivity.NewTracker(agentactivity.TrackerOptions{StallAfter: 20 * time.Millisecond, OnStall: a.h.stalled})
	t.Cleanup(a.h.tracker.Stop)
	a.lifecycle(t, "running")
	a.next(t, agentactivity.LifecycleChanged)
	a.signal(agentactivity.TerminalBusy, agentactivity.SourceTerminal, "")
	if e := a.next(t, agentactivity.ActivityChanged); e.To.State != agentactivity.Working || e.To.Stalled {
		t.Fatalf("working event %+v", e)
	}
	if e := a.next(t, agentactivity.ActivityChanged); !e.To.Stalled {
		t.Fatalf("stall event %+v", e)
	}
	if act := a.record(t).Summary.Activity; act == nil || !act.Stalled {
		t.Fatalf("record %+v", act)
	}
}

func TestInputSubmits(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want bool
	}{
		{"enter", "\r", true},
		{"text and enter", "yes\r", true},
		{"lone escape", "\x1b", true},
		{"arrow key", "\x1b[A", false},
		{"text", "yes", false},
		{"newline only", "\n", false},
		{"empty", "", false},
	} {
		if got := submits([]byte(tc.data)); got != tc.want {
			t.Errorf("%s: submits(%q) = %v", tc.name, tc.data, got)
		}
	}
}

// A holder with a process reports what the agent's progress sequences say.
func TestHolderReportsTerminalActivity(t *testing.T) {
	f := newFixture(t, `read line; `+oscBusy+`; read line`)
	r, _ := f.start(t)
	monitor, ready, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	// The ready frame is the first status; the running one follows if the
	// session was still starting.
	status := ready.Status
	if status.State != "running" {
		frame, _ := readUntil(t, monitor, func(fr Frame, _ string) bool { return fr.Type == "status" && fr.Status.State == "running" })
		status = frame.Status
	}
	if act := status.Activity; act == nil || act.State != agentactivity.Unknown {
		t.Fatalf("activity at start %+v", act)
	}

	writer, _, err := Dial(r, Request{Mode: ModeTerminal, Client: "web"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Send(Request{Type: "input", Data: []byte("go\r")}); err != nil {
		t.Fatal(err)
	}
	frame, _ := readUntil(t, monitor, func(fr Frame, _ string) bool { return fr.Type == "activity" })
	act := frame.Status.Activity
	if frame.Status.State != "running" || act == nil || act.State != agentactivity.Working || act.Source != agentactivity.SourceTerminal || !act.Inferred {
		t.Fatalf("activity frame %+v", frame.Status)
	}
	// The record is written before the frame goes out.
	if rec, err := Read(f.deps.Paths, r.ID); err != nil || rec.Summary.Activity == nil || rec.Summary.Activity.State != agentactivity.Working {
		t.Fatalf("record %+v %v", rec.Summary.Activity, err)
	}
	if info, err := Query(r); err != nil || info.Summary.Activity == nil || info.Summary.Activity.State != agentactivity.Working {
		t.Fatalf("info %+v %v", info.Summary.Activity, err)
	}

	if err := Stop(r, ""); err != nil {
		t.Fatal(err)
	}
	final, _ := readUntil(t, monitor, func(fr Frame, _ string) bool { return fr.Type == "status" && !fr.Status.Active() })
	if final.Status.Activity != nil {
		t.Fatalf("ended session keeps activity %+v", final.Status.Activity)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec, err := Read(f.deps.Paths, r.ID)
		if err == nil && rec.Summary.State == "exited" {
			if rec.Summary.Activity != nil {
				t.Fatalf("final record keeps activity %+v", rec.Summary.Activity)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("record never ended: %+v %v", rec.Summary, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRecordWithoutActivityStillLoads(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockFile(p.lock("sess_b1"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	old := `{"version":1,"id":"sess_b1","pid":1,"socket":"/x","generation":"g","binary_version":"v","protocol":1,"work_dir":"/w",
"summary":{"id":"sess_b1","project_id":"p","worktree_id":"t","account_id":"a","provider":"fake","profile_name":"n","name":"","state":"running","created_at":"2026-01-01T00:00:00Z"}}`
	if err := os.WriteFile(p.record("sess_b1"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Read(p, "sess_b1")
	if err != nil || !r.Alive || r.Summary.State != "running" || r.Summary.Activity != nil {
		t.Fatalf("old record %+v %v", r.Summary, err)
	}
	// Records without activity do not mention it.
	b, _ := json.Marshal(r.Summary)
	if bytes.Contains(b, []byte("activity")) {
		t.Fatalf("summary json %s", b)
	}
}

// manager fixtures -----------------------------------------------------

type notifyCounter struct {
	mu    sync.Mutex
	n     int
	m     *Manager
	acted bool // a notification saw a working agent
}

func (c *notifyCounter) notify(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	for _, s := range c.m.List(project) {
		if s.Activity != nil && s.Activity.State == agentactivity.Working {
			c.acted = true
		}
	}
}

func (c *notifyCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func busyEvents(m *Manager) chan agentactivity.Event {
	ch := make(chan agentactivity.Event, 16)
	m.Activity().Subscribe("test", nil, func(e agentactivity.Event) { ch <- e })
	return ch
}

func expectEvent(t *testing.T, ch <-chan agentactivity.Event, typ agentactivity.EventType) agentactivity.Event {
	t.Helper()
	select {
	case e := <-ch:
		if e.Type != typ {
			t.Fatalf("event %s (%+v), want %s", e.Type, e, typ)
		}
		return e
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s event", typ)
		return agentactivity.Event{}
	}
}

// The manager keeps activity off the heavy notify path and ignores frame types
// it does not know, such as those a newer holder sends.
func TestManagerWatchActivityFramesDoNotNotify(t *testing.T) {
	counter := &notifyCounter{}
	m := NewManager(nil, nil, nil, Paths{}, nil, counter.notify)
	counter.m = m
	events := busyEvents(m)
	holder, api := net.Pipe()
	conn := &Conn{conn: api, dec: newDecoder(api)}
	summary := Summary{ID: "sess_c1", ProjectID: "project", State: "running", CreatedAt: time.Now().UTC()}
	e := &entry{summary: Summary{ID: "sess_c1", ProjectID: "project", State: "starting"}, live: true, attachments: map[*Attachment]struct{}{}, done: make(chan struct{})}
	m.entries[e.summary.ID] = e
	ended := make(chan bool, 1)
	go func() { ended <- m.watch(e, conn) }()
	send := func(typ string, s Summary) {
		t.Helper()
		_ = holder.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := encode(holder, Frame{Type: typ, Status: &s}); err != nil {
			t.Fatal(err)
		}
	}

	send("status", summary)
	if e := expectEvent(t, events, agentactivity.LifecycleChanged); e.Lifecycle != "running" || e.Session.ID != "sess_c1" {
		t.Fatalf("lifecycle event %+v", e)
	}
	send("from-the-future", Summary{ID: "sess_c1", ProjectID: "project", State: "exited"})
	working := summary
	working.Activity = &agentactivity.Activity{State: agentactivity.Working, Detail: "Bash", Source: agentactivity.SourceHook}
	send("activity", working)
	if e := expectEvent(t, events, agentactivity.ActivityChanged); e.To.State != agentactivity.Working || e.From != nil {
		t.Fatalf("activity event %+v", e)
	}
	if got := m.List("project"); len(got) != 1 || got[0].State != "running" || got[0].Activity == nil || got[0].Activity.Detail != "Bash" {
		t.Fatalf("list %+v", got)
	}
	detail := summary
	detail.Activity = &agentactivity.Activity{State: agentactivity.Working, Detail: "Edit", Source: agentactivity.SourceHook}
	send("activity", detail)
	waiting := summary
	waiting.Activity = &agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonPermission}
	send("activity", waiting)
	// Events arrive in order, so the detail-only frame published none.
	if e := expectEvent(t, events, agentactivity.ActivityChanged); e.To.State != agentactivity.Waiting || e.From.Detail != "Edit" {
		t.Fatalf("waiting event %+v", e)
	}
	exited := summary
	exited.State = "exited"
	send("status", exited)
	if !<-ended {
		t.Fatal("exit status did not end the watch")
	}
	if got, _ := m.Get("project", "sess_c1"); got.State != "exited" || got.Activity != nil {
		t.Fatalf("ended session %+v", got)
	}
	// One notification per status frame; the activity frames sent none.
	if n := counter.count(); n != 2 {
		t.Fatalf("%d notifications, want 2", n)
	}
	if counter.acted {
		t.Fatal("a notification ran with a working agent")
	}
}

// A stale activity frame cannot revive a session the manager is stopping.
func TestManagerIgnoresActivityOfStoppingSession(t *testing.T) {
	m := NewManager(nil, nil, nil, Paths{}, nil, nil)
	e := &entry{summary: Summary{ID: "sess_c2", ProjectID: "project", State: "stopping"}}
	late := Summary{ID: "sess_c2", ProjectID: "project", State: "running", Activity: &agentactivity.Activity{State: agentactivity.Working}}
	m.applyActivity(e, late)
	if e.summary.Activity != nil {
		t.Fatalf("stopping session got %+v", e.summary.Activity)
	}
}

// Activity transitions rewrite the holder's record, which wakes WatchRecords;
// that must not reach the heavy notify path.
func TestWatchRecordsIgnoresActivityOnlyRewrites(t *testing.T) {
	s, _, account := testStarter(t, `read line; `+oscBusy+`; while :; do sleep 1; done`, agents.Capabilities{Interactive: true})
	counter := &notifyCounter{}
	m := NewManager(s.Accounts, s.Sessions, s.Registry, s.Paths, s.Launch, counter.notify)
	counter.m = m
	t.Cleanup(func() { m.Close(true, 0) })
	events := busyEvents(m)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.WatchRecords(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	record, err := s.Start(context.Background(), startRequest(t, account))
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, record.ID, "running")
	writer, _, err := Dial(record, Request{Mode: ModeTerminal, Client: "tui"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Send(Request{Type: "input", Data: []byte("go\r")}); err != nil {
		t.Fatal(err)
	}
	// Adoption may publish lifecycle events first; the activity event follows.
	var e agentactivity.Event
	for e.Type != agentactivity.ActivityChanged {
		select {
		case e = <-events:
		case <-time.After(5 * time.Second):
			t.Fatal("no activity event")
		}
	}
	if e.To.State != agentactivity.Working {
		t.Fatalf("activity event %+v", e)
	}
	rec, err := Read(s.Paths, record.ID)
	if err != nil || rec.Summary.Activity == nil || rec.Summary.Activity.State != agentactivity.Working {
		t.Fatalf("record %+v %v", rec.Summary.Activity, err)
	}
	// The record rewrite is on disk: a sync now sees the new record.
	before := counter.count()
	m.syncRecords(ctx)
	if after := counter.count(); after != before {
		t.Fatalf("activity-only record rewrite notified (%d -> %d)", before, after)
	}
	if counter.acted {
		t.Fatal("a notification ran with a working agent")
	}
	if got, _ := m.Get("project", record.ID); got.Activity == nil || got.Activity.State != agentactivity.Working {
		t.Fatalf("manager summary %+v", got.Activity)
	}
}
