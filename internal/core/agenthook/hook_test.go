package agenthook

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type fakeReporter struct {
	out      string
	panics   bool
	payloads [][]byte
}

func (f *fakeReporter) ActivityProfile() agents.ActivityProfile { return agents.ActivityProfile{} }
func (f *fakeReporter) HookOutput(string) []byte                { return []byte(f.out) }
func (f *fakeReporter) TranslateHook(event string, payload []byte) []agentactivity.Signal {
	if f.panics {
		panic("translator bug")
	}
	f.payloads = append(f.payloads, payload)
	if string(payload) == "ignore" {
		return nil
	}
	return []agentactivity.Signal{{Kind: agentactivity.ToolStarted, Detail: event + "\x1b[31m"}}
}

var started = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type harness struct {
	t        *testing.T
	root     string
	spool    string
	reporter *fakeReporter
	env      map[string]string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "sess_abc")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dir, agents.ActivitySpoolName)
	if err := os.WriteFile(spool, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, root: root, spool: spool, reporter: &fakeReporter{out: "{}"}, env: map[string]string{agents.ActivityEnv: spool}}
}

// run runs a hook and returns exit code and stdout.
func (h *harness) run(stdin io.Reader, args ...string) (int, string) {
	h.t.Helper()
	var out bytes.Buffer
	code := Run(Options{
		Args: args, Stdin: stdin, Stdout: &out, SessionsRoot: h.root,
		Getenv: func(k string) string { return h.env[k] },
		Lookup: func(id string) (agents.ActivityReporter, bool) { return h.reporter, id == "fake" },
		Now:    func() time.Time { return started },
	})
	return code, out.String()
}

func (h *harness) spoolText() string {
	h.t.Helper()
	data, err := os.ReadFile(h.spool)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func TestRunAppendsSignalWithHookStartTime(t *testing.T) {
	h := newHarness(t)
	code, out := h.run(strings.NewReader(`{"tool_name":"Bash"}`), "fake", "PreToolUse")
	if code != 0 || out != "{}" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	var got []agentactivity.Signal
	stop, err := agentactivity.Tail(t.Context(), h.spool, func(s agentactivity.Signal) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if len(got) != 1 || got[0].Kind != agentactivity.ToolStarted || !got[0].At.Equal(started) || got[0].Source != agentactivity.SourceHook {
		t.Fatalf("signals = %+v, want one tool_started at %s from a hook", got, started)
	}
	if got[0].Detail != "PreToolUse[31m" {
		t.Fatalf("detail = %q, want the escape byte dropped", got[0].Detail)
	}
}

func TestRunAppendsAndNeverCreates(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		h.run(strings.NewReader("x"), "fake", "Stop")
	}
	if n := strings.Count(h.spoolText(), "\n"); n != 3 {
		t.Fatalf("lines = %d, want 3:\n%s", n, h.spoolText())
	}
	// The session ended: the hook finds no file, must not create one, and succeeds.
	if err := os.RemoveAll(filepath.Dir(h.spool)); err != nil {
		t.Fatal(err)
	}
	code, out := h.run(strings.NewReader("x"), "fake", "Stop")
	if code != 0 || out != "{}" {
		t.Fatalf("late hook: code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Dir(h.spool)); !os.IsNotExist(err) {
		t.Fatalf("late hook recreated the session dir: %v", err)
	}
}

func TestRunNoOps(t *testing.T) {
	outside := t.TempDir()
	outsideSpool := filepath.Join(outside, "x", agents.ActivitySpoolName)
	if err := os.MkdirAll(filepath.Dir(outsideSpool), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideSpool, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		setup func(h *harness)
	}{
		{"env unset", func(h *harness) { delete(h.env, agents.ActivityEnv) }},
		{"env empty", func(h *harness) { h.env[agents.ActivityEnv] = "" }},
		{"relative path", func(h *harness) { h.env[agents.ActivityEnv] = "sess_abc/activity.jsonl" }},
		{"missing file", func(h *harness) { _ = os.Remove(h.spool) }},
		{"directory", func(h *harness) { _ = os.Remove(h.spool); _ = os.Mkdir(h.spool, 0o700) }},
		{"outside root", func(h *harness) { h.env[agents.ActivityEnv] = outsideSpool }},
		{"other file in a session", func(h *harness) {
			other := filepath.Join(filepath.Dir(h.spool), "session.json")
			_ = os.WriteFile(other, []byte("keep"), 0o600)
			h.env[agents.ActivityEnv] = other
		}},
		{"too deep", func(h *harness) {
			deep := filepath.Join(filepath.Dir(h.spool), "home", agents.ActivitySpoolName)
			_ = os.MkdirAll(filepath.Dir(deep), 0o700)
			_ = os.WriteFile(deep, nil, 0o600)
			h.env[agents.ActivityEnv] = deep
		}},
		{"symlink out of root", func(h *harness) {
			_ = os.Remove(h.spool)
			_ = os.Symlink(outsideSpool, h.spool)
		}},
		{"dotdot out of root", func(h *harness) {
			h.env[agents.ActivityEnv] = filepath.Join(h.root, "sess_abc", "..", "..", filepath.Base(outside), "x", agents.ActivitySpoolName)
		}},
		{"oversize spool", func(h *harness) {
			f, _ := os.OpenFile(h.spool, os.O_WRONLY, 0)
			_ = f.Truncate(MaxSpoolBytes + 1)
			_ = f.Close()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.setup(h)
			before := snapshot(t, h.root, outside)
			code, out := h.run(strings.NewReader("x"), "fake", "Stop")
			if code != 0 || out != "{}" {
				t.Fatalf("code=%d out=%q, want 0 and the provider's output", code, out)
			}
			if after := snapshot(t, h.root, outside); after != before {
				t.Fatalf("no-op hook changed files:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

// snapshot lists every file under dirs with its size.
func snapshot(t *testing.T, dirs ...string) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				b.WriteString(path + ":" + info.Mode().String() + ":" + strconv.FormatInt(info.Size(), 10) + "\n")
			}
			return nil
		})
	}
	return b.String()
}

func TestRunPayloadCaps(t *testing.T) {
	h := newHarness(t)
	h.run(strings.NewReader(strings.Repeat("a", MaxPayloadBytes+1)), "fake", "Stop")
	if h.spoolText() != "" || len(h.reporter.payloads) != 0 {
		t.Fatalf("oversize payload was translated: %d payloads, spool %q", len(h.reporter.payloads), h.spoolText())
	}
	h.run(strings.NewReader(strings.Repeat("a", MaxPayloadBytes)), "fake", "Stop")
	if len(h.reporter.payloads) != 1 || len(h.reporter.payloads[0]) != MaxPayloadBytes {
		t.Fatal("payload at the cap must be translated whole")
	}
}

func TestRunNoSignalsLeavesSpoolAlone(t *testing.T) {
	h := newHarness(t)
	code, out := h.run(strings.NewReader("ignore"), "fake", "Stop")
	if code != 0 || out != "{}" || h.spoolText() != "" {
		t.Fatalf("code=%d out=%q spool=%q", code, out, h.spoolText())
	}
}

func TestRunAlwaysExitsZero(t *testing.T) {
	h := newHarness(t)
	h.reporter.panics = true
	if code, out := h.run(strings.NewReader("x"), "fake", "Stop"); code != 0 || out != "{}" {
		t.Fatalf("panicking translator: code=%d out=%q", code, out)
	}
	h.reporter.panics = false
	for name, args := range map[string][]string{
		"no args": nil, "provider only": {"fake"}, "unknown provider": {"nope", "Stop"},
	} {
		if code, _ := h.run(strings.NewReader("x"), args...); code != 0 {
			t.Errorf("%s: code = %d", name, code)
		}
	}
	// A reader that fails is a failed hook, not a failing agent.
	if code, _ := h.run(failingReader{}, "fake", "Stop"); code != 0 {
		t.Errorf("failing stdin: code = %d", code)
	}
	if code, _ := h.run(nil, "fake", "Stop"); code != 0 {
		t.Errorf("nil stdin: code = %d", code)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRunRootFlag(t *testing.T) {
	h := newHarness(t)
	elsewhere := t.TempDir() // the default root a HOME-overridden hook would compute
	run := func(root string, args ...string) {
		t.Helper()
		var out bytes.Buffer
		Run(Options{
			Args: args, Stdin: strings.NewReader("x"), Stdout: &out, SessionsRoot: root,
			Getenv: func(k string) string { return h.env[k] },
			Lookup: func(id string) (agents.ActivityReporter, bool) { return h.reporter, true },
			Now:    func() time.Time { return started },
		})
	}

	// Without the flag the wrong default root rejects the spool.
	run(elsewhere, "fake", "Stop")
	if h.spoolText() != "" {
		t.Fatalf("spool outside the default root was written: %q", h.spoolText())
	}
	// The flag names the trusted root.
	run(elsewhere, "fake", "Stop", RootFlag+h.root)
	if n := strings.Count(h.spoolText(), "\n"); n != 1 {
		t.Fatalf("flagged root: %d lines, want 1:\n%s", n, h.spoolText())
	}
	// A relative or empty flag value is ignored, so the default root applies.
	run(elsewhere, "fake", "Stop", RootFlag+"sessions")
	run(elsewhere, "fake", "Stop", RootFlag)
	if n := strings.Count(h.spoolText(), "\n"); n != 1 {
		t.Fatalf("unusable flag changed the root: %d lines", n)
	}
	// The flag moves the root, not the layout: a spool outside the flagged root
	// is still rejected.
	run(h.root, "fake", "Stop", RootFlag+elsewhere)
	if n := strings.Count(h.spoolText(), "\n"); n != 1 {
		t.Fatalf("spool outside the flagged root was written: %d lines", n)
	}
}

// blockingReader parks the first Read until released, standing in for a stdin
// the agent keeps open.
type blockingReader struct {
	reading chan struct{}
	release chan struct{}
	once    sync.Once
	data    *strings.Reader
}

func (b *blockingReader) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.reading) })
	<-b.release
	return b.data.Read(p)
}

func TestRunSpoolSwappedForSymlinkWhileReadingStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	h := newHarness(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdin := &blockingReader{reading: make(chan struct{}), release: make(chan struct{}), data: strings.NewReader("x")}
	type result struct {
		code int
		out  string
	}
	done := make(chan result, 1)
	go func() {
		code, out := h.run(stdin, "fake", "Stop")
		done <- result{code, out}
	}()
	// Reading stdin starts after the spool was validated, so the swap lands in
	// the gap between check and use.
	<-stdin.reading
	if err := os.Remove(h.spool); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, h.spool); err != nil {
		t.Fatal(err)
	}
	close(stdin.release)
	res := <-done
	if res.code != 0 || res.out != "{}" {
		t.Fatalf("code=%d out=%q, want 0 and the provider's output", res.code, res.out)
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep\n" {
		t.Fatalf("swapped-in symlink target was written: %q", data)
	}
}
