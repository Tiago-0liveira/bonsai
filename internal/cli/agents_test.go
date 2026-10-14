//go:build linux || darwin

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// syncBuffer is a bytes.Buffer an attach goroutine writes while a test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) await(t *testing.T, want string) {
	t.Helper()
	if !waitUntil(5*time.Second, func() bool { return strings.Contains(b.String(), want) }) {
		t.Fatalf("never saw %q in:\n%s", want, b.String())
	}
}

func testAgentPaths(t *testing.T) agenthost.Paths {
	t.Helper()
	// macOS's default temp directory can exceed the Unix socket path limit.
	sockets, err := os.MkdirTemp("/tmp", "bah")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	return agenthost.Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
}

// writeEndedRecord leaves the record of a holder that has exited.
func writeEndedRecord(t *testing.T, p agenthost.Paths, id, name, state string, code int, created time.Time) {
	t.Helper()
	if err := os.MkdirAll(p.Records, 0o700); err != nil {
		t.Fatal(err)
	}
	started, ended := created.Add(2*time.Second), created.Add(45*time.Minute)
	summary := agentterminal.Summary{ID: id, ProjectID: "bonsai", Name: name, Provider: "claude", ProfileName: "work", State: state, CreatedAt: created, StartedAt: &started}
	if state != "running" {
		summary.EndedAt, summary.ExitCode = &ended, &code
	}
	home, _ := os.UserHomeDir()
	raw, _ := json.Marshal(agenthost.Record{Version: agenthost.RecordVersion, ID: id, PID: 1, Socket: filepath.Join(p.Sockets, id+".sock"), Protocol: agenthost.Version, WorkDir: filepath.Join(home, "src", "bonsai-"+name), Summary: summary})
	if err := os.WriteFile(filepath.Join(p.Records, id+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentsListGolden(t *testing.T) {
	p := testAgentPaths(t)
	now := time.Now().UTC().Truncate(time.Second)
	writeEndedRecord(t, p, "sess_0a1b2c3d4e5f", "fix-login", "exited", 0, now.Add(-3*time.Hour))
	writeEndedRecord(t, p, "sess_9f8e7d6c5b4a", "refactor", "failed", 2, now.Add(-2*time.Hour))
	// A holder that died mid-session reads as failed.
	writeEndedRecord(t, p, "sess_77aa88bb99cc", "crashed", "running", 0, now.Add(-time.Hour))
	var out bytes.Buffer
	c := &agentsCLI{paths: p, out: &out, errOut: &out, now: func() time.Time { return now }}
	if err := c.run(nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "No agents running. `bonsai agents --all` includes ended ones.\n" {
		t.Fatalf("no running agents: %q", out.String())
	}
	out.Reset()
	if err := c.run([]string{"--all"}); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "agents-all.golden"))
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with core.autocrlf rewrites the golden with CRLF.
	if got := out.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("table differs from testdata/agents-all.golden:\n%s", got)
	}
	out.Reset()
	if err := c.run([]string{"ls", "--all", "--json"}); err != nil {
		t.Fatal(err)
	}
	var items []agentJSON
	if err := json.Unmarshal(out.Bytes(), &items); err != nil || len(items) != 3 || items[2].State != "failed" || items[2].Error != agenthost.LostError || items[0].Alive {
		t.Fatalf("json %v: %s", err, out.String())
	}
}

func TestAgentsResolveByPrefixAndName(t *testing.T) {
	p := testAgentPaths(t)
	now := time.Now().UTC()
	writeEndedRecord(t, p, "sess_0a1b2c3d", "same", "exited", 0, now.Add(-2*time.Hour))
	writeEndedRecord(t, p, "sess_0a1bffff", "same", "exited", 1, now.Add(-time.Hour))
	writeEndedRecord(t, p, "sess_12345678", "other", "exited", 0, now)
	c := &agentsCLI{paths: p, now: time.Now}
	for query, want := range map[string]string{"sess_12345678": "sess_12345678", "1234": "sess_12345678", "sess_0a1b2": "sess_0a1b2c3d", "OTHER": "sess_12345678"} {
		if row, err := c.resolve(query); err != nil || row.ID != want {
			t.Errorf("resolve %q = %s, %v", query, row.ID, err)
		}
	}
	if _, err := c.resolve("same"); err == nil || !strings.Contains(err.Error(), "matches 2 agent sessions") || !strings.Contains(err.Error(), "0a1bffff  same  exited (1)") {
		t.Fatalf("ambiguous name: %v", err)
	}
	if _, err := c.resolve("0a1b"); err == nil {
		t.Fatal("ambiguous prefix resolved")
	}
	if _, err := c.resolve("nope"); err == nil || !strings.Contains(err.Error(), "no agent session") {
		t.Fatalf("unknown: %v", err)
	}
	var out bytes.Buffer
	c.out = &out
	if err := c.run([]string{"attach", "other"}); err == nil || !strings.Contains(err.Error(), "has ended") {
		t.Fatalf("attach to an ended session: %v", err)
	}
}

func TestAgentsAttachTakeControlAndDetach(t *testing.T) {
	p := testAgentPaths(t)
	r := startFakeAgentAt(t, p, "worker")
	keys, typing := newKeyPipe()
	out, status := &syncBuffer{}, &syncBuffer{}
	size := func() (int, int, bool) { return 100, 30, true }
	done := make(chan error, 1)
	go func() { done <- attachTerminal(r, "worker", attachOptions{}, keys, out, status, size, nil, nil) }()
	status.await(t, "attached to worker (Ctrl-] d to detach)")
	out.await(t, "FAKE-READY")
	typing("hello\n")
	out.await(t, "ECHO:hello")

	// The browser takes control; the CLI hears who took it.
	web, _, err := agenthost.Dial(r, agenthost.Request{Mode: agenthost.ModeTerminal, Client: "web", Take: true})
	if err != nil {
		t.Fatal(err)
	}
	defer web.Close()
	status.await(t, "control taken by web")
	typing("ignored\n")
	status.await(t, "read-only — Ctrl-] t to take control")
	// Ctrl-] t takes it back; the size follows the new writer.
	typing("\x1dt")
	status.await(t, "you have control of worker")
	if !waitUntil(5*time.Second, func() bool {
		info, err := agenthost.Query(r)
		return err == nil && info.Cols == 100 && info.Rows == 30 && len(info.Clients) == 2 && info.Clients[0].Client == "cli" && info.Clients[0].Writer
	}) {
		t.Fatal("control and size did not move to the CLI")
	}
	typing("again\n")
	out.await(t, "ECHO:again")
	if strings.Contains(out.String(), "ECHO:ignored") {
		t.Fatal("input typed while read-only reached the agent")
	}
	typing("\x1dd")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-] d did not detach")
	}
	status.await(t, "detached from worker; it keeps running")
	if current, err := agenthost.Read(p, r.ID); err != nil || !current.Alive || current.Summary.State != "running" {
		t.Fatalf("after detach %+v %v", current.Summary, err)
	}
}

func TestAgentsAttachReadOnlyAndStop(t *testing.T) {
	p := testAgentPaths(t)
	r := startFakeAgentAt(t, p, "watched")
	keys, typing := newKeyPipe()
	out, status := &syncBuffer{}, &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- attachTerminal(r, "watched", attachOptions{readOnly: true}, keys, out, status, func() (int, int, bool) { return 0, 0, false }, nil, nil)
	}()
	status.await(t, "watching watched (read-only; Ctrl-] d to detach)")
	out.await(t, "FAKE-READY")
	typing("\x1dt")
	status.await(t, "started with --read-only")
	if info, err := agenthost.Query(r); err != nil || len(info.Clients) != 1 || info.Clients[0].Writer {
		t.Fatalf("a read-only first attachment writes: %+v %v", info, err)
	}

	var stopOut bytes.Buffer
	c := &agentsCLI{paths: p, out: &stopOut, errOut: &stopOut, now: time.Now}
	if err := c.run([]string{"stop", "watched"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stopOut.String(), "✓ stopped watched") {
		t.Fatalf("stop: %s", stopOut.String())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach did not end with the session")
	}
	status.await(t, "agent exited (code")
}

// newKeyPipe is an attach's keyboard: typing writes keys to it.
func newKeyPipe() (*os.File, func(string)) {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	return r, func(keys string) { _, _ = w.WriteString(keys) }
}
