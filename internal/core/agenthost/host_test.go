//go:build linux || darwin

package agenthost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// testPaths keeps sockets under a short directory: t.TempDir paths can pass
// the Unix socket path limit.
func testPaths(t *testing.T) Paths {
	t.Helper()
	sockets, err := os.MkdirTemp("", "bah")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	return Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
}

type fixture struct {
	deps    Deps
	account agents.Account
}

func newFixture(t *testing.T, script string) fixture {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	account := agents.Account{ID: id, Provider: testProviderID, Name: "fixture"}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(&testProvider{script: script, capabilities: agents.Capabilities{Interactive: true}}); err != nil {
		t.Fatal(err)
	}
	return fixture{deps: Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Paths: testPaths(t), Linger: 200 * time.Millisecond}, account: account}
}

// start serves one session in this process and returns its record.
func (f fixture) start(t *testing.T) (Record, <-chan error) {
	t.Helper()
	runtime, err := f.deps.Sessions.Create(f.account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{Summary: agentterminal.Summary{ID: string(runtime.ID), ProjectID: "project", WorktreeID: "tree", AccountID: f.account.ID, Provider: testProviderID, Name: "fixture", State: "starting", CreatedAt: time.Now().UTC()}, WorkDir: runtime.WorkDir, Cols: 80, Rows: 24}
	ready := make(chan error, 1)
	done := make(chan error, 1)
	exited := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(exited)
		done <- Serve(ctx, spec, f.deps, func(err error) { ready <- err })
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			t.Error("holder did not exit")
		}
	})
	r, err := Read(f.deps.Paths, spec.Summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r, done
}

// readUntil reads frames until match accepts one, collecting output.
func readUntil(t *testing.T, c *Conn, match func(Frame, string) bool) (Frame, string) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.SetDeadline(time.Time{})
	output := ""
	for {
		f, err := c.Recv()
		if err != nil {
			t.Fatalf("recv: %v (output %q)", err, output)
		}
		output += string(f.Data)
		if match(f, output) {
			return f, output
		}
	}
}

func TestHolderAttachInputTakeAndStop(t *testing.T) {
	f := newFixture(t, `printf 'READY\n'; while read line; do printf 'ECHO:%s\n' "$line"; done`)
	r, done := f.start(t)
	if !r.Alive || r.Socket == "" || r.Protocol != Version || r.Summary.ID == "" {
		t.Fatalf("record %+v", r)
	}
	if info, err := os.Stat(r.Socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v %v", info, err)
	}
	web, ready, err := Dial(r, Request{Mode: ModeTerminal, Client: "web"})
	if err != nil || ready.Writer == nil || !*ready.Writer || ready.Generation != r.Generation {
		t.Fatalf("first attach %+v %v", ready, err)
	}
	defer web.Close()
	readUntil(t, web, func(_ Frame, out string) bool { return strings.Contains(out, "READY") })
	if err := web.Send(Request{Type: "input", Data: []byte("one\n")}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, web, func(_ Frame, out string) bool { return strings.Contains(out, "ECHO:one") })

	cli, ready, err := Dial(r, Request{Mode: ModeTerminal, Client: "cli"})
	if err != nil || *ready.Writer {
		t.Fatalf("second attach should observe: %+v %v", ready, err)
	}
	defer cli.Close()
	readUntil(t, cli, func(_ Frame, out string) bool { return strings.Contains(out, "ECHO:one") })
	if err := cli.Send(Request{Type: "input", Data: []byte("ignored\n")}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, cli, func(f Frame, _ string) bool { return f.Type == "error" })

	info, err := Query(r)
	if err != nil || len(info.Clients) != 2 || !info.Clients[0].Writer || info.Clients[0].Client != "web" || info.Clients[1].Client != "cli" || info.LastOutputAt == nil {
		t.Fatalf("info %+v %v", info, err)
	}

	if err := cli.Send(Request{Type: "take"}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, cli, func(f Frame, _ string) bool { return f.Type == "writer" && *f.Writer })
	readUntil(t, web, func(f Frame, _ string) bool { return f.Type == "writer" && !*f.Writer })
	if err := cli.Send(Request{Type: "input", Data: []byte("two\n")}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, web, func(_ Frame, out string) bool { return strings.Contains(out, "ECHO:two") })

	// A disconnecting client leaves the session running.
	_ = cli.Close()
	time.Sleep(50 * time.Millisecond)
	if r, _ := Read(f.deps.Paths, r.ID); !r.Alive || r.Summary.State != "running" {
		t.Fatalf("after disconnect %+v", r.Summary)
	}

	monitor, ready, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil || ready.Status == nil || ready.Status.State != "running" || *ready.Writer {
		t.Fatalf("monitor %+v %v", ready, err)
	}
	defer monitor.Close()
	if err := Stop(r, ""); err != nil {
		t.Fatal(err)
	}
	final, _ := readUntil(t, monitor, func(f Frame, _ string) bool { return f.Type == "status" && !f.Status.Active() })
	if final.Status.State != "exited" || final.Status.EndedAt == nil {
		t.Fatalf("final %+v", final.Status)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not exit")
	}
	after, err := Read(f.deps.Paths, r.ID)
	if err != nil || after.Alive || after.Summary.State != "exited" {
		t.Fatalf("final record %+v %v", after, err)
	}
	if _, err := os.Stat(r.Socket); !os.IsNotExist(err) {
		t.Fatalf("socket left behind: %v", err)
	}
}

func TestHolderResumesFromOffsetWithItsGeneration(t *testing.T) {
	f := newFixture(t, `printf 'ABCDEF\n'; read line`)
	r, _ := f.start(t)
	first, _, err := Dial(r, Request{Mode: ModeTerminal})
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, func(_ Frame, out string) bool { return strings.Contains(out, "ABCDEF") })
	_ = first.Close()
	resumed, _, err := Dial(r, Request{Mode: ModeTerminal, Generation: r.Generation, Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	_, out := readUntil(t, resumed, func(f Frame, _ string) bool { return f.Type == "status" })
	if out != "DEF\r\n" {
		t.Fatalf("resumed replay %q", out)
	}
	stale, _, err := Dial(r, Request{Mode: ModeTerminal, Generation: "other", Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	if _, out := readUntil(t, stale, func(f Frame, _ string) bool { return f.Type == "status" }); out != "ABCDEF\r\n" {
		t.Fatalf("another generation replays everything: %q", out)
	}
}

func TestHolderRejectsOtherProtocolVersions(t *testing.T) {
	f := newFixture(t, `read line`)
	r, _ := f.start(t)
	old := r
	old.Protocol = Version + 1
	if _, _, err := Dial(old, Request{Mode: ModeStatus}); err != ErrVersion {
		t.Fatalf("record of another version: %v", err)
	}
}

func TestRecordsLivenessAndPrune(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	write := func(id, state string, ended *time.Time) {
		t.Helper()
		if err := writeRecord(p, Record{Version: RecordVersion, ID: id, Protocol: Version, Socket: p.socket(id), Summary: agentterminal.Summary{ID: id, State: state, CreatedAt: now, EndedAt: ended}}); err != nil {
			t.Fatal(err)
		}
	}
	write("sess_aa", "running", nil)
	write("sess_bb", "exited", &now)
	write("sess_cc", "exited", &old)
	write("sess_dd", "running", nil)
	lock, err := lockFile(p.lock("sess_dd"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	records, err := List(p)
	if err != nil || len(records) != 4 {
		t.Fatalf("list %d %v", len(records), err)
	}
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.ID] = r
	}
	if r := byID["sess_aa"]; r.Alive || r.Summary.State != "failed" || r.Summary.Error != LostError || r.Summary.EndedAt == nil {
		t.Fatalf("dead holder with a running session: %+v", r.Summary)
	}
	if r := byID["sess_dd"]; !r.Alive || r.Summary.State != "running" {
		t.Fatalf("live holder: %+v", r)
	}
	if err := Prune(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p, "sess_cc"); !os.IsNotExist(err) {
		t.Fatalf("day-old record kept: %v", err)
	}
	for _, id := range []string{"sess_aa", "sess_bb", "sess_dd"} {
		if _, err := Read(p, id); err != nil {
			t.Fatalf("%s pruned: %v", id, err)
		}
	}
	if err := Remove(p, "sess_dd"); err == nil {
		t.Fatal("removed a running holder's record")
	}
	if _, err := Read(p, "../x"); err == nil {
		t.Fatal("accepted a path as id")
	}
}

func TestLaunchReportsHolderStartFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the bonsai binary")
	}
	bin := filepath.Join(t.TempDir(), "bonsai")
	build := exec.Command("go", "build", "-o", bin, "github.com/Tiago-0liveira/bonsai")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Setenv("BONSAI_DAEMON_BIN", bin)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	p := testPaths(t)
	spec := Spec{Summary: agentterminal.Summary{ID: "sess_0123", AccountID: "acct_missing", State: "starting"}, Cols: 80, Rows: 24}
	err := Launch(p, spec)
	if err == nil || !strings.Contains(err.Error(), "profile unavailable") {
		t.Fatalf("launch: %v", err)
	}
	if held(p.lock("sess_0123")) {
		t.Fatal("failed holder kept its lock")
	}
}
