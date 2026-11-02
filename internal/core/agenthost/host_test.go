//go:build linux || darwin

package agenthost

import (
	"context"
	"encoding/json"
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
	// macOS's default temp directory can exceed the Unix socket path limit.
	sockets, err := os.MkdirTemp("/tmp", "bah")
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

func TestPublishedStatusHasPersistedRecord(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	lock, err := lockFile(p.lock("sess_a1"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	summary := Summary{ID: "sess_a1", State: "starting", CreatedAt: time.Now().UTC()}
	h := newHost(Deps{Paths: p}, "", agentterminal.NewStream[Summary](80, 24), "", nil, summary)
	_, _, _, frames := h.stream.Attach(0, summary)
	if err := h.update(func(s *Summary) error { s.State = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-frames:
		if frame.Status == nil || frame.Status.State != "running" {
			t.Fatalf("status frame: %+v", frame)
		}
		r, err := Read(p, summary.ID)
		if err != nil || r.Summary.State != frame.Status.State {
			t.Fatalf("record after status: %+v, %v", r, err)
		}
	case <-time.After(time.Second):
		t.Fatal("no status frame")
	}
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
	if err := Prune(p, nil); err != nil {
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
	err := Launch(context.Background(), p, spec)
	if err == nil || !strings.Contains(err.Error(), "profile unavailable") {
		t.Fatalf("launch: %v", err)
	}
	if held(p.lock("sess_0123")) {
		t.Fatal("failed holder kept its lock")
	}
}

type discoveringProvider struct {
	*testProvider
	calls   chan agents.ResumeRequest
	answers []string
}

// PrepareSession reports no id, like a provider that only learns it later.
func (p *discoveringProvider) PrepareSession(ctx context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	prepared, err := p.testProvider.PrepareSession(ctx, r)
	prepared.ProviderSessionID = ""
	return prepared, err
}

func (p *discoveringProvider) DiscoverSessionID(_ context.Context, r agents.ResumeRequest) (string, error) {
	p.calls <- r
	if len(p.answers) == 0 {
		return "", nil
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a, nil
}

func TestHolderDiscoversProviderSessionID(t *testing.T) {
	old := [3]time.Duration{discoverFast, discoverFastFor, discoverSlow}
	discoverFast, discoverFastFor, discoverSlow = time.Millisecond, time.Hour, time.Millisecond
	t.Cleanup(func() { discoverFast, discoverFastFor, discoverSlow = old[0], old[1], old[2] })
	f := newFixture(t, "sleep 30")
	other := Record{Version: RecordVersion, ID: "sess_ee", Protocol: Version, Summary: agentterminal.Summary{ID: "sess_ee", AccountID: f.account.ID, ProviderSessionID: "taken", State: "exited", CreatedAt: time.Now().UTC()}}
	if err := f.deps.Paths.ensure(); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(f.deps.Paths, other); err != nil {
		t.Fatal(err)
	}
	provider := &discoveringProvider{testProvider: &testProvider{script: "sleep 30", capabilities: agents.Capabilities{Interactive: true}}, calls: make(chan agents.ResumeRequest, 16), answers: []string{"", "", "abc"}}
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	f.deps.Registry = registry
	r, _ := f.start(t)
	var last agents.ResumeRequest
	for i := 0; i < 3; i++ {
		select {
		case last = <-provider.calls:
		case <-time.After(10 * time.Second):
			t.Fatal("discovery did not poll")
		}
	}
	if len(last.Claimed) != 1 || last.Claimed[0] != "taken" || last.StartedAt.IsZero() {
		t.Fatalf("request %+v", last)
	}
	conn, ready, err := Dial(r, Request{Mode: ModeTerminal, Client: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if ready.Status == nil || ready.Status.ProviderSessionID != "abc" {
		readUntil(t, conn, func(frame Frame, _ string) bool {
			return frame.Status != nil && frame.Status.ProviderSessionID == "abc"
		})
	}
	got, err := Read(f.deps.Paths, r.ID)
	if err != nil || got.Summary.ProviderSessionID != "abc" {
		t.Fatalf("record %+v %v", got.Summary, err)
	}
}

func TestPruneRemovesRuntime(t *testing.T) {
	f := newFixture(t, "true")
	account := f.account
	runtime, err := f.deps.Sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := string(runtime.ID)
	p := f.deps.Paths
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := writeRecord(p, Record{Version: RecordVersion, ID: id, Protocol: Version, Socket: p.socket(id), Summary: agentterminal.Summary{ID: id, State: "exited", CreatedAt: old, EndedAt: &old}}); err != nil {
		t.Fatal(err)
	}
	if err := Prune(p, f.deps.Sessions); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtime.RuntimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime kept: %v", err)
	}
}

func TestReapRuntimesKeepsYoungAndRecorded(t *testing.T) {
	f := newFixture(t, "true")
	store := f.deps.Sessions.(*agents.FileSessionStore)
	mk := func(age time.Duration) agents.Session {
		t.Helper()
		s, err := store.Create(f.account, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		// Backdate the metadata so the runtime looks old.
		path := filepath.Join(s.RuntimeDir, "session.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		m["created_at"] = time.Now().UTC().Add(-age)
		raw, _ = json.Marshal(m)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return s
	}
	young, orphan, recorded := mk(time.Hour), mk(48*time.Hour), mk(48*time.Hour)
	p := f.deps.Paths
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	id := string(recorded.ID)
	if err := writeRecord(p, Record{Version: RecordVersion, ID: id, Protocol: Version, Summary: agentterminal.Summary{ID: id, State: "exited"}}); err != nil {
		t.Fatal(err)
	}
	if err := ReapRuntimes(p, store); err != nil {
		t.Fatal(err)
	}
	for s, want := range map[agents.Session]bool{young: true, orphan: false, recorded: true} {
		if _, err := os.Stat(s.RuntimeDir); (err == nil) != want {
			t.Fatalf("%s kept=%v want %v", s.ID, err == nil, want)
		}
	}
}

func TestPruneRuntimeRemovalFailures(t *testing.T) {
	for _, tc := range []struct {
		name          string
		recordFailure bool
	}{
		{name: "record removal failure keeps runtime", recordFailure: true},
		{name: "ancillary removal failure cleans runtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.recordFailure && os.Geteuid() == 0 {
				t.Skip("root bypasses directory write permissions")
			}
			f := newFixture(t, "true")
			runtime, err := f.deps.Sessions.Create(f.account, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			p, id := f.deps.Paths, string(runtime.ID)
			if err := p.ensure(); err != nil {
				t.Fatal(err)
			}
			old := time.Now().UTC().Add(-48 * time.Hour)
			if err := writeRecord(p, Record{Version: RecordVersion, ID: id, Protocol: Version, Summary: Summary{ID: id, State: "exited", CreatedAt: old, EndedAt: &old}}); err != nil {
				t.Fatal(err)
			}
			if tc.recordFailure {
				if err := os.Chmod(p.Records, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(p.Records, 0o700); err != nil {
						t.Error(err)
					}
				})
			} else {
				// A nonempty socket-path directory cannot be removed with os.Remove.
				if err := os.Mkdir(p.socket(id), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(p.socket(id), "child"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Prune(p, f.deps.Sessions); err == nil {
				t.Fatal("expected removal failure")
			}
			for _, path := range []string{p.record(id), runtime.RuntimeDir} {
				_, err := os.Stat(path)
				if tc.recordFailure && err != nil || !tc.recordFailure && !os.IsNotExist(err) {
					t.Fatalf("%s: want kept=%v, stat error=%v", path, tc.recordFailure, err)
				}
			}
		})
	}
}
