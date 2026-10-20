package agenthost

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
)

var allNotify = NotifyPrefs{Enabled: true, NeedsInput: true, TurnComplete: true}

func change(from, to agentactivity.State, reason agentactivity.Reason, source agentactivity.Source, detail string) agentactivity.Event {
	f := &agentactivity.Activity{State: from}
	t := &agentactivity.Activity{State: to, Reason: reason, Source: source, Detail: detail}
	return agentactivity.Event{
		Type: agentactivity.ActivityChanged, At: time.Unix(100, 0), From: f, To: t,
		Session: agentactivity.SessionRef{ID: "sess_1", Name: "fix login", Provider: "claude"},
	}
}

func TestNotificationTable(t *testing.T) {
	hook := agentactivity.SourceHook
	cases := []struct {
		name  string
		prefs NotifyPrefs
		event agentactivity.Event
		title string // empty: no notification
		body  string
	}{
		{"permission", allNotify, change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, hook, "Bash"), "Bonsai: fix login needs input", "claude (permission: Bash)"},
		{"question has no detail", allNotify, change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonQuestion, hook, "Which database should we use?"), "Bonsai: fix login needs input", "claude (question)"},
		{"inferred terminal waiting", allNotify, change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, agentactivity.SourceTerminal, ""), "Bonsai: fix login needs input", "claude (permission)"},
		{"turn complete", allNotify, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonTurnComplete, hook, ""), "Bonsai: fix login finished", "claude"},
		{"error", allNotify, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonError, hook, "rate_limit"), "Bonsai: fix login failed", "claude"},
		{"ready at session start", allNotify, change(agentactivity.Unknown, agentactivity.Idle, agentactivity.ReasonReady, hook, ""), "", ""},
		{"interrupted", allNotify, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonInterrupted, agentactivity.SourceInput, ""), "", ""},
		{"input source", allNotify, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonTurnComplete, agentactivity.SourceInput, ""), "", ""},
		{"lifecycle source", allNotify, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonTurnComplete, agentactivity.SourceLifecycle, ""), "", ""},
		{"working", allNotify, change(agentactivity.Idle, agentactivity.Working, "", hook, ""), "", ""},
		{"same state, inferred confirmed", allNotify, func() agentactivity.Event {
			e := change(agentactivity.Waiting, agentactivity.Waiting, agentactivity.ReasonPermission, hook, "Bash")
			e.From.Reason = agentactivity.ReasonPermission
			return e
		}(), "", ""},
		{"lifecycle event", allNotify, agentactivity.Event{Type: agentactivity.LifecycleChanged, Lifecycle: "exited"}, "", ""},
		{"disabled", NotifyPrefs{NeedsInput: true, TurnComplete: true}, change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, hook, "Bash"), "", ""},
		{"needs_input off", NotifyPrefs{Enabled: true, TurnComplete: true}, change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, hook, "Bash"), "", ""},
		{"turn_complete off", NotifyPrefs{Enabled: true, NeedsInput: true}, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonTurnComplete, hook, ""), "", ""},
		{"turn_complete only", NotifyPrefs{Enabled: true, TurnComplete: true}, change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonError, hook, ""), "Bonsai: fix login failed", "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, title, body, ok := notification(tc.prefs, tc.event)
			if ok != (tc.title != "") || title != tc.title || body != tc.body {
				t.Fatalf("notification = %q, %q, %v; want %q, %q", title, body, ok, tc.title, tc.body)
			}
		})
	}
}

func TestNotificationContainsOnlyAllowedText(t *testing.T) {
	e := change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, agentactivity.SourceHook, "Bash $(rm -rf ~) \"x\" `y` ;&")
	e.Session.Name = "evil\" ; $(touch /tmp/x) `id` \\ \n" + strings.Repeat("n", 200)
	_, title, body, ok := notification(allNotify, e)
	if !ok {
		t.Fatal("no notification")
	}
	for _, text := range []string{title, body} {
		if strings.ContainsAny(text, "$`\"\\;&\n~") {
			t.Fatalf("unsafe text %q", text)
		}
	}
	if len(title) > 120 {
		t.Fatalf("title not capped: %d", len(title))
	}
}

type sentLog struct {
	mu    sync.Mutex
	sent  []string
	count chan struct{}
}

func newSentLog() *sentLog { return &sentLog{count: make(chan struct{}, 16)} }

func (l *sentLog) send(title, body string) {
	l.mu.Lock()
	l.sent = append(l.sent, title+" | "+body)
	l.mu.Unlock()
	l.count <- struct{}{}
}

func (l *sentLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.sent...)
}

func (l *sentLog) wait(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-l.count:
		case <-time.After(5 * time.Second):
			t.Fatalf("notification not sent; got %v", l.all())
		}
	}
}

func notifierHost(prefs NotifyPrefs, log *sentLog) *host {
	return &host{deps: Deps{Notifications: prefs, Notify: log.send}, bus: &agentactivity.Bus{}}
}

func TestNotifierRateLimitsPerType(t *testing.T) {
	log := newSentLog()
	h := notifierHost(allNotify, log)
	defer h.startNotifier()()
	at := func(e agentactivity.Event, d time.Duration) agentactivity.Event { e.At = e.At.Add(d); return e }
	wait := change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, agentactivity.SourceHook, "Bash")
	done := change(agentactivity.Working, agentactivity.Idle, agentactivity.ReasonTurnComplete, agentactivity.SourceHook, "")
	h.bus.Publish(wait)
	h.bus.Publish(at(wait, time.Second))           // dropped: same type within 2 s
	h.bus.Publish(at(done, time.Second))           // other type: sent
	h.bus.Publish(at(wait, 3*time.Second))         // window passed: sent
	h.bus.Publish(at(done, 1500*time.Millisecond)) // dropped
	log.wait(t, 3)
	want := []string{
		"Bonsai: fix login needs input | claude (permission: Bash)",
		"Bonsai: fix login finished | claude",
		"Bonsai: fix login needs input | claude (permission: Bash)",
	}
	got := log.all()
	if len(got) != 3 {
		t.Fatalf("sent %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sent[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNotifierDisabledSubscribesToNothing(t *testing.T) {
	log := newSentLog()
	for _, prefs := range []NotifyPrefs{{}, {NeedsInput: true, TurnComplete: true}, {Enabled: true}} {
		h := notifierHost(prefs, log)
		cancel := h.startNotifier()
		h.bus.Publish(change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, agentactivity.SourceHook, "Bash"))
		cancel()
		if got := log.all(); len(got) != 0 {
			t.Fatalf("prefs %+v sent %v", prefs, got)
		}
	}
}

func TestNotifierStopsOnCancel(t *testing.T) {
	log := newSentLog()
	h := notifierHost(allNotify, log)
	cancel := h.startNotifier()
	cancel()
	cancel()
	h.bus.Publish(change(agentactivity.Working, agentactivity.Waiting, agentactivity.ReasonPermission, agentactivity.SourceHook, "Bash"))
	if got := log.all(); len(got) != 0 {
		t.Fatalf("sent after cancel: %v", got)
	}
}
