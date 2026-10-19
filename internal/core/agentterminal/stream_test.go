//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package agentterminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type streamStatus struct{ State string }

func TestStreamReplayGapAndWriterElection(t *testing.T) {
	s := NewStream[streamStatus](80, 24)
	_, _ = s.Write([]byte("hello "))
	first, writer, replay, frames := s.Attach(0, streamStatus{"running"})
	if !writer || len(replay) != 2 || string(replay[0].Data) != "hello " || replay[1].Status.State != "running" || replay[1].Offset != 6 {
		t.Fatalf("first attach: writer=%v replay=%+v", writer, replay)
	}
	second, writer, replay, _ := s.Attach(3, streamStatus{"running"})
	if writer || string(replay[0].Data) != "lo " || replay[0].Offset != 3 {
		t.Fatalf("observer: writer=%v replay=%+v", writer, replay)
	}
	_, _ = s.Write([]byte("world"))
	if f := <-frames; f.Type != "output" || f.Offset != 6 || string(f.Data) != "world" {
		t.Fatalf("live frame %+v", f)
	}
	s.Status(streamStatus{"exited"})
	if f := <-frames; f.Type != "status" || f.Offset != 11 || f.Status.State != "exited" {
		t.Fatalf("status frame %+v", f)
	}
	if err := s.Input(second, []byte("x")); err == nil || errors.Is(err, ErrNoProcess) {
		t.Fatal("observer may not write")
	}
	if err := s.Input(first, []byte("x")); !errors.Is(err, ErrNoProcess) {
		t.Fatalf("writer without a process: %v", err)
	}
	if err := s.Resize(second, 100, 40); err == nil {
		t.Fatal("observer resized")
	}
	if err := s.Resize(first, 100, 40); err != nil {
		t.Fatal(err)
	}
	if cols, rows := s.Size(); cols != 100 || rows != 40 {
		t.Fatalf("size %dx%d", cols, rows)
	}
	if err := s.Resize(first, 0, 40); err == nil {
		t.Fatal("bad size accepted")
	}
	s.Detach(first)
	third, writer, _, _ := s.Attach(0, streamStatus{})
	if !writer {
		t.Fatal("writer not released on detach")
	}
	s.Detach(second)
	s.Detach(third)

	// Released streams keep counting offsets and replay a gap, never old bytes.
	s.Release()
	_, _, replay, _ = s.Attach(0, streamStatus{})
	if replay[0].Type != "gap" || replay[0].Offset != 11 || len(replay) != 2 || replay[1].Type != "status" {
		t.Fatalf("after release %+v", replay)
	}
}

func TestStreamDropsSlowSubscriber(t *testing.T) {
	s := NewStream[streamStatus](80, 24)
	_, writer, _, frames := s.Attach(0, streamStatus{})
	if !writer {
		t.Fatal("not writer")
	}
	for i := 0; i < 100; i++ {
		_, _ = s.Write([]byte("x"))
	}
	for range frames {
	}
	if s.writer != 0 || len(s.subscribers) != 0 {
		t.Fatal("slow subscriber retained")
	}
}

func readUntil(t *testing.T, frames <-chan StreamFrame[streamStatus], output *strings.Builder, want string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for !strings.Contains(output.String(), want) {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("stream closed; output %q", output.String())
			}
			output.Write(f.Data)
		case <-timeout:
			t.Fatalf("no %q in %q", want, output.String())
		}
	}
}

func TestRunInStreamInputResizeAndExit(t *testing.T) {
	s := NewStream[streamStatus](80, 24)
	sub, _, _, frames := s.Attach(0, streamStatus{})
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := RunInStream(context.Background(), s, agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", `echo "SIZE:$(stty size)"; IFS= read -r line; echo "GOT:$line"; IFS= read -r line; echo "SIZE:$(stty size)"; exit 7`}, Dir: t.TempDir()}, 90, 30)
		done <- result{code, err}
	}()
	var output strings.Builder
	readUntil(t, frames, &output, "SIZE:30 90")
	if err := s.Input(sub, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, frames, &output, "GOT:hello")
	if err := s.Resize(sub, 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.Input(sub, []byte("\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, frames, &output, "SIZE:40 100")
	r := <-done
	if r.code != 7 || r.err == nil {
		t.Fatalf("exit %d %v", r.code, r.err)
	}
	if err := s.Input(sub, []byte("late\n")); !errors.Is(err, ErrNoProcess) {
		t.Fatalf("input after exit: %v", err)
	}
}

func TestRunInStreamCancelKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	s := NewStream[streamStatus](80, 24)
	_, _, _, frames := s.Attach(0, streamStatus{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		// The shell ignores SIGTERM, so cancellation has to escalate to SIGKILL.
		code, _ := RunInStream(ctx, s, agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", `trap '' TERM; sleep 60 & echo $$ $! >"$PIDFILE"; echo STARTED; wait`}, Dir: t.TempDir(), EnvSet: map[string]string{"PIDFILE": pidfile}}, 80, 24)
		done <- code
	}()
	var output strings.Builder
	readUntil(t, frames, &output, "STARTED")
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the process")
	}
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range strings.Fields(string(data)) {
		pid, _ := strconv.Atoi(field)
		deadline := time.Now().Add(2 * time.Second)
		// Observe death only once: probing again can race with reaping between
		// kill(0) and the /proc read and conservatively report the process alive.
		for processAlive(pid) {
			if !time.Now().Before(deadline) {
				t.Fatalf("process %d survived cancel", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// processAlive treats a zombie waiting for its reaper as dead.
func processAlive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) == 0 || fields[0] != "Z"
}

func TestRunInStreamCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, err := RunInStream(ctx, NewStream[streamStatus](80, 24), agents.PreparedSession{Executable: "/bin/true"}, 80, 24)
	if code != -1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("%d %v", code, err)
	}
}

func TestStreamTakeSendsWriterFrames(t *testing.T) {
	s := NewStream[streamStatus](80, 24)
	if !s.LastOutput().IsZero() {
		t.Fatal("output time before any output")
	}
	_, _ = s.Write([]byte("x"))
	if s.LastOutput().IsZero() {
		t.Fatal("output time not tracked")
	}
	web, writer, _, webFrames := s.AttachAs(0, streamStatus{}, "web", ControlAuto)
	cli, observer, _, cliFrames := s.AttachAs(0, streamStatus{}, "cli", ControlAuto)
	if !writer || observer {
		t.Fatal("first attachment writes")
	}
	if got := s.Clients(); len(got) != 2 || got[0] != (StreamClient{"web", true}) || got[1] != (StreamClient{"cli", false}) {
		t.Fatalf("clients %+v", got)
	}
	if err := s.Take(cli); err != nil {
		t.Fatal(err)
	}
	if f := <-webFrames; f.Type != "writer" || *f.Writer {
		t.Fatalf("old writer frame %+v", f)
	}
	if f := <-cliFrames; f.Type != "writer" || !*f.Writer {
		t.Fatalf("new writer frame %+v", f)
	}
	if s.Resize(web, 90, 30) == nil || s.Resize(cli, 90, 30) != nil {
		t.Fatal("control did not move")
	}
	if err := s.Take(cli); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-cliFrames:
		t.Fatalf("taking held control sent %+v", f)
	default:
	}
	_, taken, _, _ := s.AttachAs(0, streamStatus{}, "cli", ControlTake)
	if !taken {
		t.Fatal("take on attach")
	}
	if f := <-cliFrames; f.Type != "writer" || *f.Writer {
		t.Fatalf("taken writer frame %+v", f)
	}
	if s.Take(999) == nil {
		t.Fatal("took control for an unknown subscriber")
	}
	watcher, writes, _, _ := s.AttachAs(0, streamStatus{}, "cli", ControlNever)
	if writes || s.Take(watcher) == nil {
		t.Fatal("a read-only attachment wrote")
	}
	s2 := NewStream[streamStatus](80, 24)
	if _, writes, _, _ := s2.AttachAs(0, streamStatus{}, "cli", ControlNever); writes {
		t.Fatal("a read-only first attachment wrote")
	}
	if _, writes, _, _ := s2.AttachAs(0, streamStatus{}, "web", ControlAuto); !writes {
		t.Fatal("the next attachment after a read-only one did not write")
	}
}

type hooksProvider struct {
	agents.Provider
	events *[]string
}

func (p hooksProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", `printf HELLO`}, Dir: r.Session.WorkDir}, nil
}

func (p hooksProvider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	*p.events = append(*p.events, "finalize")
	return nil
}

func (p hooksProvider) Label() string { return "Hooks" }

func TestRunSessionHooksSeeOutputAndEndBeforeFinalize(t *testing.T) {
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	account := agents.Account{ID: id, Provider: "hooks", Name: "fixture"}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	var seen strings.Builder
	stream := NewStream[Summary](80, 24)
	RunSession(context.Background(), stream, hooksProvider{events: &events}, account, runtime, sessions, 80, 24, agents.LaunchOptions{},
		func(change func(*Summary) error) error { return change(&Summary{}) },
		RunHooks{
			Output: func(p []byte) { seen.Write(p) },
			Ending: func() { events = append(events, "ending") },
		})
	if seen.String() != "HELLO" {
		t.Fatalf("output hook saw %q", seen.String())
	}
	if strings.Join(events, ",") != "ending,finalize" {
		t.Fatalf("order %v", events)
	}
	// The zero value is today's behavior.
	RunSession(context.Background(), NewStream[Summary](80, 24), hooksProvider{events: &events}, account, runtime, sessions, 80, 24, agents.LaunchOptions{}, func(change func(*Summary) error) error { return change(&Summary{}) }, RunHooks{})
}
