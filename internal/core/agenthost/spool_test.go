//go:build linux || darwin

package agenthost

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// reportingProvider is a provider whose agent reports through the activity
// spool: its fixture script appends signal lines to $SPOOL, which stands for
// what the provider's hook command would write.
type reportingProvider struct {
	*testProvider
	profile  agents.ActivityProfile
	requests chan agents.PrepareSessionRequest
	// spoolMode is the file mode of the spool as PrepareSession saw it.
	spoolMode atomic.Uint32
	// afterEnd runs in FinalizeSession, after the process ended and the holder
	// asked for the tail to stop, before the runtime directory is removed.
	afterEnd func(spool string)
	spool    atomic.Value
}

func (p *reportingProvider) ActivityProfile() agents.ActivityProfile { return p.profile }
func (p *reportingProvider) HookOutput(string) []byte                { return nil }
func (p *reportingProvider) TranslateHook(string, []byte) []agentactivity.Signal {
	return nil
}

func (p *reportingProvider) ClassifyTerminalNotification(_, body string) (agentactivity.Signal, bool) {
	if strings.Contains(body, "approval") {
		return agentactivity.Signal{Kind: agentactivity.PermissionRequested, Detail: "Bash"}, true
	}
	return agentactivity.Signal{}, false
}

func (p *reportingProvider) PrepareSession(ctx context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	prepared, err := p.testProvider.PrepareSession(ctx, r)
	if err != nil {
		return prepared, err
	}
	if r.Activity != nil {
		if info, err := os.Stat(r.Activity.SpoolPath); err == nil {
			p.spoolMode.Store(uint32(info.Mode().Perm()))
		}
		p.spool.Store(r.Activity.SpoolPath)
		prepared.EnvSet[r.Activity.SpoolEnv] = r.Activity.SpoolPath
	}
	p.requests <- r
	return prepared, nil
}

func (p *reportingProvider) FinalizeSession(ctx context.Context, r agents.FinalizeSessionRequest) error {
	if spool, _ := p.spool.Load().(string); spool != "" && p.afterEnd != nil {
		p.afterEnd(spool)
	}
	return p.testProvider.FinalizeSession(ctx, r)
}

func newReportingFixture(t *testing.T, script string, profile agents.ActivityProfile, hook []string) (fixture, *reportingProvider) {
	t.Helper()
	f := newFixture(t, script)
	provider := &reportingProvider{testProvider: &testProvider{script: script, capabilities: agents.Capabilities{Interactive: true}}, profile: profile, requests: make(chan agents.PrepareSessionRequest, 1)}
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	f.deps.Registry = registry
	f.deps.HookCommand = hook
	return f, provider
}

// appendSignal is the shell for one spool line, as a hook would write it.
func appendSignal(kind, detail string) string {
	line := `{"kind":"` + kind + `","source":"hook"`
	if detail != "" {
		line += `,"detail":"` + detail + `"`
	}
	return `printf '%s\n' '` + line + `}' >>"$SPOOL";`
}

// trailer follows a status connection and keeps the activity states it
// reports, consecutive repeats collapsed. The holder's own connection ends
// when the session does.
type trailer struct {
	t       *testing.T
	monitor *Conn
	trail   []string
	current *agentactivity.Activity
	// lifecycle is the session state the latest frame carried.
	lifecycle string
}

func newTrailer(t *testing.T, monitor *Conn, ready Frame) *trailer {
	tr := &trailer{t: t, monitor: monitor}
	tr.add(ready.Status)
	return tr
}

func (tr *trailer) add(s *agentterminal.Summary) {
	if s != nil {
		tr.lifecycle = s.State
	}
	if s == nil || s.Activity == nil {
		return
	}
	a := s.Activity
	tr.current = a
	if a.State == agentactivity.Unknown {
		return
	}
	label := string(a.State)
	if a.Reason != "" {
		label += "/" + string(a.Reason)
	}
	if n := len(tr.trail); n == 0 || tr.trail[n-1] != label {
		tr.trail = append(tr.trail, label)
	}
}

// until reads frames until the latest activity satisfies stop.
func (tr *trailer) until(stop func(*agentactivity.Activity) bool) {
	tr.t.Helper()
	_ = tr.monitor.SetDeadline(time.Now().Add(10 * time.Second))
	for tr.current == nil || !stop(tr.current) {
		f, err := tr.monitor.Recv()
		if err != nil {
			tr.t.Fatalf("connection ended before the expected activity: trail %v (%v)", tr.trail, err)
		}
		tr.add(f.Status)
	}
}

// running reads frames until the session runs. The holder drops input until
// the process is bound, so a test that types first must wait for this.
func (tr *trailer) running() {
	tr.t.Helper()
	_ = tr.monitor.SetDeadline(time.Now().Add(10 * time.Second))
	for tr.lifecycle != "running" {
		f, err := tr.monitor.Recv()
		if err != nil {
			tr.t.Fatalf("connection ended before the session ran: %v", err)
		}
		tr.add(f.Status)
	}
}

// drain reads until the holder closes the connection.
func (tr *trailer) drain() {
	_ = tr.monitor.SetDeadline(time.Now().Add(10 * time.Second))
	for {
		f, err := tr.monitor.Recv()
		if err != nil {
			return
		}
		tr.add(f.Status)
	}
}

func state(want agentactivity.State) func(*agentactivity.Activity) bool {
	return func(a *agentactivity.Activity) bool { return a.State == want }
}

func dialMonitor(t *testing.T, r Record) (*Conn, Frame) {
	t.Helper()
	monitor, ready, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { monitor.Close() })
	return monitor, ready
}

func sendLine(t *testing.T, w *Conn) {
	t.Helper()
	if err := w.Send(Request{Type: "input", Data: []byte("go\r")}); err != nil {
		t.Fatal(err)
	}
}

func TestHolderFollowsHookSpool(t *testing.T) {
	hook := []string{"/opt/bonsai", "__agent-hook"}
	script := `SPOOL="$BONSAI_AGENT_ACTIVITY";` + appendSignal("session_started", "") + `read l;` +
		appendSignal("prompt_submitted", "") + appendSignal("permission_requested", "Bash") + `read l;` +
		appendSignal("tool_finished", "Bash") + `read l;` + appendSignal("turn_completed", "") + `read l`
	f, provider := newReportingFixture(t, script, agents.ActivityProfile{Native: []agentactivity.State{agentactivity.Working}}, hook)
	r, _ := f.start(t)

	req := <-provider.requests
	if req.Activity == nil {
		t.Fatal("provider got no activity request")
	}
	runtimeDir := filepath.Dir(req.Activity.SpoolPath)
	if want := filepath.Join(runtimeDir, agents.ActivitySpoolName); req.Activity.SpoolPath != want || runtimeDir != req.Session.RuntimeDir {
		t.Fatalf("spool %q is not %q", req.Activity.SpoolPath, want)
	}
	if want := []string{"/opt/bonsai", "__agent-hook", "fake"}; !reflect.DeepEqual(req.Activity.HookCommand, want) || req.Activity.SpoolEnv != agents.ActivityEnv {
		t.Fatalf("activity request %+v", req.Activity)
	}
	if mode := os.FileMode(provider.spoolMode.Load()); mode != 0o600 {
		t.Fatalf("spool mode = %v, want 0600", mode)
	}

	monitor, ready := dialMonitor(t, r)
	writer, _, err := Dial(r, Request{Mode: ModeTerminal, Client: "web"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tr := newTrailer(t, monitor, ready)
	// Each step waits for the state it caused before typing the next line.
	tr.until(state(agentactivity.Idle))
	sendLine(t, writer)
	tr.until(state(agentactivity.Waiting))
	sendLine(t, writer)
	tr.until(state(agentactivity.Working))
	sendLine(t, writer)
	tr.until(state(agentactivity.Idle))
	if want := []string{"idle/ready", "working", "waiting/permission", "working", "idle/turn_complete"}; !reflect.DeepEqual(tr.trail, want) {
		t.Fatalf("trail %v, want %v", tr.trail, want)
	}

	// The record carries the state, with the detail the hook reported.
	rec, err := Read(f.deps.Paths, r.ID)
	if err != nil || rec.Summary.Activity == nil || rec.Summary.Activity.State != agentactivity.Idle || rec.Summary.Activity.Source != agentactivity.SourceHook || rec.Summary.Activity.Inferred {
		t.Fatalf("record %+v %v", rec.Summary.Activity, err)
	}
}

func TestHolderStopsTailBeforeCleanup(t *testing.T) {
	// A prompt written just before exit must still count (the tail drains once
	// more); one written after the process ended and the tail stopped must not.
	f, provider := newReportingFixture(t, `SPOOL="$BONSAI_AGENT_ACTIVITY";`+appendSignal("prompt_submitted", ""), agents.ActivityProfile{}, []string{"/opt/bonsai", "__agent-hook"})
	var late atomic.Bool
	provider.afterEnd = func(spool string) {
		f, err := os.OpenFile(spool, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Errorf("spool gone before cleanup: %v", err)
			return
		}
		_, _ = f.WriteString(`{"kind":"turn_failed","source":"hook","detail":"late"}` + "\n")
		_ = f.Close()
		// Longer than the poll interval, so a tail left running would apply it.
		time.Sleep(900 * time.Millisecond)
		late.Store(true)
	}
	r, done := f.start(t)
	monitor, ready := dialMonitor(t, r)
	tr := newTrailer(t, monitor, ready)
	tr.drain()
	<-done
	if !late.Load() {
		t.Fatal("the provider's finalize step never ran")
	}
	if want := []string{"working"}; !reflect.DeepEqual(tr.trail, want) {
		t.Fatalf("trail %v, want %v (a signal after the process ended must be ignored)", tr.trail, want)
	}
	if _, err := os.Stat(provider.spool.Load().(string)); !os.IsNotExist(err) {
		t.Fatalf("spool outlived the session: %v", err)
	}
}

func TestHolderTerminalSignalsUseProviderProfile(t *testing.T) {
	// Progress clear ends the turn only for a provider that says so, and a
	// notification only counts through the provider's classifier.
	// The pauses let the holder see each input before the output it provokes.
	script := `read l; sleep 0.2; printf '\033]9;4;3\007'; read l; sleep 0.2; printf '\033]9;needs approval\007'; read l; sleep 0.2; printf '\033]9;4;0\007'; read l`
	f, provider := newReportingFixture(t, script, agents.ActivityProfile{ProgressMeansTurn: true, Inferred: []agentactivity.State{agentactivity.Waiting}}, []string{"/opt/bonsai", "__agent-hook"})
	r, _ := f.start(t)
	<-provider.requests
	monitor, ready := dialMonitor(t, r)
	writer, _, err := Dial(r, Request{Mode: ModeTerminal, Client: "web"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tr := newTrailer(t, monitor, ready)
	tr.running()
	for _, want := range []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle} {
		sendLine(t, writer)
		tr.until(state(want))
	}
	// The third line answers the wait (inferred), then the progress clear ends the turn.
	if want := []string{"working", "waiting/permission", "working", "idle/turn_complete"}; !reflect.DeepEqual(tr.trail, want) {
		t.Fatalf("trail %v, want %v", tr.trail, want)
	}
	if a := tr.current; a.Source != agentactivity.SourceTerminal || !a.Inferred {
		t.Fatalf("final activity %+v, want terminal and inferred", a)
	}
}

func TestHolderRequestsActivityOnlyWhenItCanHear(t *testing.T) {
	// A provider that reports nothing is never asked for hooks, and a holder
	// that is not the bonsai binary has no hook command to give.
	plain := newFixture(t, `true`)
	prepared := make(chan agents.PrepareSessionRequest, 1)
	registry := agents.NewRegistry()
	if err := registry.Register(&testProvider{script: `true`, prepared: prepared, capabilities: agents.Capabilities{Interactive: true}}); err != nil {
		t.Fatal(err)
	}
	plain.deps.Registry = registry
	plain.deps.HookCommand = []string{"/opt/bonsai", "__agent-hook"}
	plain.start(t)
	if req := <-prepared; req.Activity != nil {
		t.Fatalf("non-reporting provider got %+v", req.Activity)
	}

	f, provider := newReportingFixture(t, `true`, agents.ActivityProfile{}, nil)
	f.start(t)
	if req := <-provider.requests; req.Activity != nil {
		t.Fatalf("test binary offered itself as the hook command: %+v", req.Activity)
	}
}
