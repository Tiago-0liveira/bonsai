package agenthost

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/termscreen"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

// Spec is the session a holder runs. It reaches the holder on stdin, so the
// prompt never lands on disk or in argv.
type Spec struct {
	// Summary is the session as the API created it, in state starting.
	Summary agentterminal.Summary `json:"summary"`
	WorkDir string                `json:"work_dir"`
	Cols    int                   `json:"cols"`
	Rows    int                   `json:"rows"`
	Launch  agents.LaunchOptions  `json:"launch"`
}

// Deps are the stores and providers a holder runs its session with.
type Deps struct {
	Accounts agents.AccountStore
	Sessions agents.SessionStore
	Registry *agents.Registry
	Paths    Paths
	// Linger is how long an ended holder waits for its clients to read the
	// final status; zero means ten seconds.
	Linger time.Duration
	// HookCommand is the argv prefix of the agent's activity hook command, to
	// which the provider ID and the hook event are appended. Empty means
	// `<this binary> __agent-hook`; a process that is not the bonsai binary
	// (a test) must set it, or the session reports no activity.
	HookCommand []string
	// Notifications is the desktop-notification preference, read once by the
	// caller; Notify sends one (nil means core/notify). Tests set Notify.
	Notifications NotifyPrefs
	Notify        func(title, body string)
}

// replayChunk bounds one replayed output frame.
const replayChunk = 64 << 10

// detailInterval spaces the broadcasts of Detail-only activity changes, which
// can follow each tool call; transitions are never delayed.
const detailInterval = 250 * time.Millisecond

type host struct {
	deps       Deps
	generation string
	stream     *agentterminal.Stream[agentterminal.Summary]
	workDir    string
	cancel     context.CancelFunc
	// tracker reduces the session's signals to its activity; bus carries the
	// session's events to consumers inside this holder.
	tracker *agentactivity.Tracker
	bus     *agentactivity.Bus
	now     func() time.Time
	// classifier turns the provider's terminal notifications into signals; nil
	// drops them. Set before the session runs.
	classifier agents.TerminalClassifier
	// Only providers without complete hook coverage need a rendered screen.
	// These fields belong to the PTY output reader and are never persisted.
	screenClassifier       agents.TerminalScreenClassifier
	activityScreen         *termscreen.Screen
	screenCols, screenRows int
	screenKind             agentactivity.Kind
	screenReason           agentactivity.Reason

	mu       sync.Mutex
	summary  agentterminal.Summary
	monitors map[*serverConn]struct{}
	conns    map[*serverConn]struct{}
	ended    chan struct{}
	// detailAt is when a Detail-only change was last broadcast.
	detailAt time.Time
}

func newHost(deps Deps, generation string, stream *agentterminal.Stream[agentterminal.Summary], workDir string, cancel context.CancelFunc, summary agentterminal.Summary) *host {
	h := &host{deps: deps, generation: generation, stream: stream, workDir: workDir, cancel: cancel, bus: &agentactivity.Bus{}, now: time.Now, summary: summary, monitors: map[*serverConn]struct{}{}, conns: map[*serverConn]struct{}{}, ended: make(chan struct{})}
	h.tracker = agentactivity.NewTracker(agentactivity.TrackerOptions{OnStall: h.stalled})
	return h
}

// Serve runs spec's session to its end, serving clients on the session's
// socket. ready is called once: with nil when the socket accepts clients, or
// with the error that prevented it. Canceling ctx stops the session (SIGTERM
// reaches the holder that way); Serve returns after the session ended and
// clients had Linger to read its final status.
func Serve(ctx context.Context, spec Spec, deps Deps, ready func(error)) error {
	err := serve(ctx, spec, deps, ready)
	if ready != nil && err != nil {
		ready(err)
	}
	return err
}

func serve(ctx context.Context, spec Spec, deps Deps, ready func(error)) error {
	id := spec.Summary.ID
	if !validID(id) {
		return fmt.Errorf("invalid agent session id")
	}
	if !agentterminal.Supported {
		return fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if !agentterminal.Dimensions(spec.Cols, spec.Rows) {
		return fmt.Errorf("terminal dimensions must be between 1 and 500")
	}
	if err := deps.Paths.ensure(); err != nil {
		return err
	}
	lock, err := lockFile(deps.Paths.lock(id))
	if err != nil {
		return fmt.Errorf("agent session %s already has a host", id)
	}
	defer lock.Close()
	_ = Prune(deps.Paths)
	account, err := deps.Accounts.Get(spec.Summary.AccountID)
	if err != nil {
		return fmt.Errorf("profile unavailable")
	}
	provider, err := deps.Registry.Get(account.Provider)
	if err != nil {
		return fmt.Errorf("provider not available")
	}
	runtime, err := deps.Sessions.Get(agents.SessionID(id))
	if err != nil {
		return fmt.Errorf("session unavailable")
	}
	socket := deps.Paths.socket(id)
	// The lock is ours, so a socket left here belongs to a dead holder.
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer os.Remove(socket)
	defer listener.Close()
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	sessionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newHost(deps, hex.EncodeToString(b[:]), agentterminal.NewStream[agentterminal.Summary](spec.Cols, spec.Rows), spec.WorkDir, cancel, spec.Summary)
	reporter, _ := provider.(agents.ActivityReporter)
	if reporter != nil {
		h.useActivityProfile(reporter.ActivityProfile())
	}
	h.classifier, _ = provider.(agents.TerminalClassifier)
	h.screenClassifier, _ = provider.(agents.TerminalScreenClassifier)
	// h.tracker is replaced above, so stop whichever one is current.
	defer func() { h.tracker.Stop() }()
	defer h.startNotifier()()
	if err := h.writeRecord(); err != nil {
		return err
	}
	if ready != nil {
		ready(nil)
	}
	ready = nil
	go h.accept(listener)
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			h.stop("")
		case <-stopped:
		}
	}()
	var observer agentactivity.OutputObserver
	activity, stopSpool := h.startSpool(provider.ID(), reporter != nil, runtime.RuntimeDir)
	defer stopSpool()
	hooks := agentterminal.RunHooks{
		Activity: activity,
		Output:   func(p []byte) { h.observe(&observer, p) },
		// The spool lives in the runtime directory that cleanup removes right
		// after this, so the tail must be done by then.
		Ending: func() {
			stopSpool()
			h.tracker.Stop()
		},
	}
	agentterminal.RunSession(sessionCtx, h.stream, provider, account, runtime, deps.Sessions, spec.Cols, spec.Rows, spec.Launch, h.update, hooks)
	close(stopped)
	close(h.ended)
	h.linger()
	return nil
}

// useActivityProfile configures the tracker for a provider. It replaces the
// tracker, so it runs before any signal can arrive.
func (h *host) useActivityProfile(profile agents.ActivityProfile) {
	h.tracker.Stop()
	h.tracker = agentactivity.NewTracker(agentactivity.TrackerOptions{OnStall: h.stalled, ProgressMeansTurn: profile.ProgressMeansTurn})
}

// startSpool creates the session's activity spool in runtimeDir and starts
// following it. It returns the request that makes the provider install its
// hooks, or nil when the session gets no hook activity (the provider reports
// none, or any step failed: activity degrades to unknown, the session never
// fails for it), and the func that ends the tail; both are safe to call again.
func (h *host) startSpool(provider agents.ProviderID, reports bool, runtimeDir string) (*agents.ActivityRequest, func()) {
	noop := func() {}
	if !reports {
		return nil, noop
	}
	command := h.hookCommand()
	if len(command) == 0 {
		return nil, noop
	}
	path := filepath.Join(runtimeDir, agents.ActivitySpoolName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, noop
	}
	_ = f.Close()
	stop, err := agentactivity.Tail(context.Background(), path, h.applyActivity)
	if err != nil {
		_ = os.Remove(path)
		return nil, noop
	}
	return &agents.ActivityRequest{
		HookCommand: append(append([]string(nil), command...), string(provider)),
		SpoolEnv:    agents.ActivityEnv,
		SpoolPath:   path,
	}, stop
}

// hookCommand is the argv prefix hooks run, or nil when there is none.
func (h *host) hookCommand() []string {
	if len(h.deps.HookCommand) > 0 {
		return h.deps.HookCommand
	}
	bin, err := os.Executable()
	if err != nil {
		return nil
	}
	// A test binary would run the whole suite as a hook.
	if base := filepath.Base(bin); strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
		return nil
	}
	return []string{bin, "__agent-hook"}
}

// update applies one summary change and publishes the result to terminal
// clients, monitors and the record.
func (h *host) update(change func(*agentterminal.Summary) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.summary.State
	if err := change(&h.summary); err != nil {
		return err
	}
	h.lifecycleChanged(prev)
	h.publish()
	return nil
}

// lifecycleChanged keeps the activity in step with a lifecycle change from
// prev: it exists only while the session runs. Callers hold h.mu and publish
// the summary afterwards.
func (h *host) lifecycleChanged(prev string) {
	state := h.summary.State
	if state == prev {
		return
	}
	h.summary.Activity = nil
	if state == "running" {
		current := h.tracker.Current()
		h.summary.Activity = &current
	}
	if !h.summary.Active() {
		h.tracker.Stop()
	}
	h.bus.Publish(agentactivity.Event{Type: agentactivity.LifecycleChanged, At: h.now(), Session: sessionRef(h.summary), Lifecycle: state})
}

// sessionRef identifies s on an activity bus.
func sessionRef(s agentterminal.Summary) agentactivity.SessionRef {
	return agentactivity.SessionRef{ID: s.ID, ProjectID: s.ProjectID, WorktreeID: s.WorktreeID, Name: s.Name, Origin: s.Origin, Provider: string(s.Provider), AccountID: string(s.AccountID)}
}

// observe turns the progress sequences in the agent's output into signals.
func (h *host) observe(o *agentactivity.OutputObserver, p []byte) {
	if h.screenClassifier != nil {
		cols, rows := h.stream.Size()
		if h.activityScreen == nil || h.screenCols != cols || h.screenRows != rows {
			h.activityScreen = termscreen.New(cols, rows)
			h.screenCols, h.screenRows = cols, rows
			h.screenKind, h.screenReason = "", ""
		}
		_, _ = h.activityScreen.Write(p)
	}
	notified := false
	for _, obs := range o.Observe(p) {
		sig, ok := obs.Signal(h.now())
		if !ok && h.classifier != nil && obs.Kind != agentactivity.ObservedProgress {
			// Notifications and bells mean something only to the provider that
			// wrote them; without a classifier they are dropped.
			if sig, ok = h.classifier.ClassifyTerminalNotification(obs.Title, obs.Body); ok {
				notified = true
				if sig.At.IsZero() {
					sig.At = h.now()
				}
				sig.Source = agentactivity.SourceTerminal
			}
		}
		if ok {
			h.applyActivity(sig)
		}
	}
	if h.screenClassifier != nil {
		sig, ok := h.screenClassifier.ClassifyTerminalScreen(h.activityScreen.Lines())
		if !ok {
			h.screenKind, h.screenReason = "", ""
			return
		}
		// Do not resurrect an answered question from an unchanged screen while
		// the next paint is still arriving. A new notification can refine its
		// generic "tool confirmation" hint using the dialog visible now.
		if notified || sig.Kind != h.screenKind || sig.Reason != h.screenReason {
			sig.At, sig.Source = h.now(), agentactivity.SourceTerminal
			h.applyActivity(sig)
			h.screenKind, h.screenReason = sig.Kind, sig.Reason
		}
	}
}

// applyActivity reduces sig into the session's activity and publishes the
// result. Signals outside the running state are dropped, except a hook's
// while starting: the agent runs before the holder marks it running, the spool
// is read once, and a SessionStart written in that window would be lost for
// good. The tracker keeps it and lifecycleChanged publishes it on running.
func (h *host) applyActivity(sig agentactivity.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.summary.State == "starting" && sig.Source == agentactivity.SourceHook {
		h.tracker.Apply(sig)
		return
	}
	if h.summary.State != "running" {
		return
	}
	act, _ := h.tracker.Apply(sig)
	h.setActivity(act)
}

// stalled is the tracker's stall timer. It re-reads the tracker: a signal may
// have moved the activity on while this waited for h.mu.
func (h *host) stalled(agentactivity.Activity) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.summary.State != "running" {
		return
	}
	h.setActivity(h.tracker.Current())
}

// setActivity publishes act as the session's activity. Callers hold h.mu and
// have checked the session runs. Everyone learns every change over sockets,
// but only a transition (state, reason, stalled, inferred) is worth a record
// rewrite and a bus event; a Detail-only change is also rate limited.
func (h *host) setActivity(act agentactivity.Activity) {
	prev := h.summary.Activity
	if prev != nil && !prev.Changed(act) && prev.Detail == act.Detail && prev.Since.Equal(act.Since) && prev.Source == act.Source {
		return
	}
	transition := prev == nil || prev.Changed(act)
	h.summary.Activity = &act
	if !transition {
		now := h.now()
		if now.Sub(h.detailAt) < detailInterval {
			return
		}
		h.detailAt = now
	}
	if transition {
		_ = h.writeRecord()
	}
	h.stream.Status(h.summary)
	summary := h.summary
	frame := Frame{Type: "activity", Offset: h.stream.Offset(), Status: &summary}
	for c := range h.monitors {
		c.queue(frame)
	}
	if transition {
		h.bus.Publish(agentactivity.Event{Type: agentactivity.ActivityChanged, At: h.now(), Session: sessionRef(summary), From: prev, To: summary.Activity})
	}
}

// publish sends the summary everywhere. Callers hold h.mu.
func (h *host) publish() {
	// Admission reads records, so persist the state before clients can act on
	// its notification (including restarting an exited account).
	_ = h.writeRecord()
	h.stream.Status(h.summary)
	summary := h.summary
	frame := Frame{Type: "status", Offset: h.stream.Offset(), Status: &summary}
	for c := range h.monitors {
		c.queue(frame)
	}
}

// writeRecord saves the holder's record. Callers hold h.mu, or own h alone.
func (h *host) writeRecord() error {
	r := Record{Version: RecordVersion, ID: h.summary.ID, PID: os.Getpid(), Socket: h.deps.Paths.socket(h.summary.ID), Generation: h.generation, BinaryVersion: version.String(), Protocol: Version, WorkDir: h.workDir, Summary: h.summary}
	if at := h.stream.LastOutput(); !at.IsZero() {
		r.LastOutputAt = &at
	}
	return writeRecord(h.deps.Paths, r)
}

// stop ends the session: SIGTERM to its process group, SIGKILL two seconds
// later. reason, when set, is recorded as the session's error.
func (h *host) stop(reason string) {
	h.mu.Lock()
	if h.summary.Active() {
		if reason != "" {
			h.summary.Error = reason
		}
		if h.summary.State != "stopping" {
			prev := h.summary.State
			h.summary.State = "stopping"
			h.lifecycleChanged(prev)
			h.publish()
		}
	}
	h.mu.Unlock()
	h.cancel()
}

func (h *host) info() Info {
	h.mu.Lock()
	summary := h.summary
	h.mu.Unlock()
	cols, rows := h.stream.Size()
	info := Info{Summary: summary, PID: os.Getpid(), Clients: h.stream.Clients(), Cols: cols, Rows: rows}
	if at := h.stream.LastOutput(); !at.IsZero() {
		info.LastOutputAt = &at
	}
	return info
}

// linger gives clients time to read the final status, then closes them.
func (h *host) linger() {
	wait := h.deps.Linger
	if wait == 0 {
		wait = 10 * time.Second
	}
	deadline := time.After(wait)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		h.mu.Lock()
		clients := len(h.conns)
		h.mu.Unlock()
		if clients == 0 {
			break
		}
		select {
		case <-deadline:
		case <-tick.C:
			continue
		}
		break
	}
	h.stream.DetachAll()
	h.mu.Lock()
	for c := range h.conns {
		_ = c.conn.Close()
	}
	h.mu.Unlock()
}

func (h *host) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go h.handle(conn)
	}
}

// serverConn is one client connection. Frames reach it in order through out,
// written by one goroutine, so a slow client never blocks the stream.
type serverConn struct {
	conn net.Conn
	out  chan Frame
	once sync.Once
	gone chan struct{}
}

func (c *serverConn) queue(f Frame) {
	select {
	case c.out <- f:
	case <-c.gone:
	default:
		// Too slow: drop the client rather than the holder's pace.
		c.close()
	}
}

func (c *serverConn) close() {
	c.once.Do(func() {
		close(c.gone)
		_ = c.conn.Close()
	})
}

func (c *serverConn) writeLoop() {
	for {
		select {
		case <-c.gone:
			return
		case f := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if encode(c.conn, f) != nil {
				c.close()
				return
			}
		}
	}
}

func (h *host) handle(conn net.Conn) {
	c := &serverConn{conn: conn, out: make(chan Frame, 256), gone: make(chan struct{})}
	defer c.close()
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, c)
		delete(h.monitors, c)
		h.mu.Unlock()
	}()
	dec := newDecoder(conn)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello Request
	if err := dec.decode(&hello); err != nil || hello.Type != "hello" {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	go c.writeLoop()
	if hello.Version != Version {
		c.queue(Frame{Type: "error", Version: Version, Error: fmt.Sprintf("agent host speaks protocol %d, client %d", Version, hello.Version)})
		time.Sleep(100 * time.Millisecond)
		return
	}
	var subscriber uint64
	switch hello.Mode {
	case ModeStatus:
		h.mu.Lock()
		summary := h.summary
		h.monitors[c] = struct{}{}
		no := false
		c.queue(Frame{Type: "ready", Version: Version, Generation: h.generation, Writer: &no, Status: &summary})
		h.mu.Unlock()
	case ModeTerminal:
		offset := hello.Offset
		if hello.Generation != h.generation {
			offset = 0
		}
		h.mu.Lock()
		control := agentterminal.ControlAuto
		switch {
		case hello.ReadOnly:
			control = agentterminal.ControlNever
		case hello.Take:
			control = agentterminal.ControlTake
		}
		id, writer, replay, frames := h.stream.AttachAs(offset, h.summary, hello.Client, control)
		c.queue(Frame{Type: "ready", Version: Version, Generation: h.generation, Writer: &writer})
		for _, f := range replay {
			for f.Type == "output" && len(f.Data) > replayChunk {
				head := f
				head.Data = f.Data[:replayChunk]
				c.queue(streamFrame(head))
				f.Offset += replayChunk
				f.Data = f.Data[replayChunk:]
			}
			c.queue(streamFrame(f))
		}
		h.mu.Unlock()
		subscriber = id
		defer h.stream.Detach(subscriber)
		go func() {
			for {
				select {
				case f, ok := <-frames:
					if !ok {
						c.close()
						return
					}
					c.queue(streamFrame(f))
				case <-c.gone:
					return
				}
			}
		}()
	default:
		c.queue(Frame{Type: "error", Error: "unknown mode"})
		return
	}
	for {
		var r Request
		if dec.decode(&r) != nil {
			return
		}
		switch r.Type {
		case "input":
			if subscriber == 0 || len(r.Data) > agentterminal.InputLimit {
				c.queue(Frame{Type: "error", Error: "terminal input unavailable"})
				continue
			}
			err := h.stream.Input(subscriber, r.Data)
			if err != nil && !errors.Is(err, agentterminal.ErrNoProcess) {
				c.queue(Frame{Type: "error", Error: "terminal input unavailable"})
			}
			if err == nil && submits(r.Data) {
				h.applyActivity(agentactivity.Signal{Kind: agentactivity.UserSubmitted, At: h.now(), Source: agentactivity.SourceInput})
			}
		case "resize":
			if subscriber == 0 || h.stream.Resize(subscriber, r.Cols, r.Rows) != nil {
				c.queue(Frame{Type: "error", Error: "terminal resize unavailable"})
			}
		case "take":
			if subscriber == 0 || h.stream.Take(subscriber) != nil {
				c.queue(Frame{Type: "error", Error: "only terminal clients can take control"})
			}
		case "stop":
			h.stop(r.Error)
		case "info":
			info := h.info()
			c.queue(Frame{Type: "info", Info: &info})
		default:
			c.queue(Frame{Type: "error", Error: "unknown request"})
		}
	}
}

// submits reports whether input answers the agent: Enter, or Escape alone
// (longer escape sequences are keys such as the arrows).
func submits(data []byte) bool {
	return bytes.IndexByte(data, '\r') >= 0 || len(data) == 1 && data[0] == 0x1b
}
