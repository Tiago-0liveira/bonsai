//go:build linux || darwin

package localapi

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentsetup"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/gorilla/websocket"
)

// setupFixture is an API with real Codex and Claude providers over their fake
// binaries (HOME is a temp dir, nothing reaches the network) and setup jobs.
type setupFixture struct {
	t       *testing.T
	s       *Server
	http    *httptest.Server
	token   string
	log     *bytes.Buffer
	pidfile string
	// responses collects every response body, for the secret leak checks.
	responses bytes.Buffer
	// profiles is the number of profile directories before the test.
	profiles int
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	s, _, _ := terminalTestServer(t)
	bin := t.TempDir()
	codexTestdata, _ := filepath.Abs(filepath.Join("..", "..", "providers", "codex", "testdata"))
	claudeTestdata, _ := filepath.Abs(filepath.Join("..", "..", "providers", "claude", "testdata"))
	for name, target := range map[string]string{"codex": filepath.Join(codexTestdata, "fakecodex.sh"), "claude": filepath.Join(claudeTestdata, "fakeclaude.sh")} {
		if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CODEX_FIXTURES", filepath.Join(codexTestdata, "phase0"))
	t.Setenv("FAKE_CLAUDE_FIXTURES", claudeTestdata)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	models, usage := claude.ModelsEndpoint, claude.UsageEndpoint
	claude.ModelsEndpoint, claude.UsageEndpoint = "http://127.0.0.1:1/v1/models", "http://127.0.0.1:1/usage"
	t.Cleanup(func() { claude.ModelsEndpoint, claude.UsageEndpoint = models, usage })
	runtime := s.agents.runtime
	if err := runtime.Registry.Register(codex.New(runtime.Accounts, runtime.Sessions)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Registry.Register(claude.New(runtime.Accounts, runtime.Sessions)); err != nil {
		t.Fatal(err)
	}
	runtime.AccountService = &agents.AccountService{Store: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry}
	s.agents.setup = agentsetup.New(runtime.AccountService, s.publishAgentsChanged)
	t.Cleanup(s.agents.setup.Close)
	f := &setupFixture{t: t, s: s, log: &bytes.Buffer{}, pidfile: filepath.Join(t.TempDir(), "login-pids")}
	t.Setenv("FAKE_CODEX_LOGIN_PIDFILE", f.pidfile)
	previous := log.Writer()
	log.SetOutput(f.log)
	t.Cleanup(func() { log.SetOutput(previous) })
	f.http = httptest.NewServer(s.Handler())
	t.Cleanup(f.http.Close)
	s.expectedHost = strings.TrimPrefix(f.http.URL, "http://")
	capability, _ := s.sessions.create()
	f.token = capability.Token
	f.profiles = len(f.profileDirs())
	return f
}

func (f *setupFixture) request(method, path, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, f.http.URL+path, strings.NewReader(body))
	r.Host = f.s.expectedHost
	r.Header.Set("Origin", "http://"+f.s.expectedHost)
	r.Header.Set("X-Bonsai-Session", f.token)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	f.responses.Write(w.Body.Bytes())
	return w
}

type setupJobJSON struct {
	agentsetup.Job
	Account map[string]any `json:"account"`
}

func (f *setupFixture) job(w *httptest.ResponseRecorder, status int) setupJobJSON {
	f.t.Helper()
	if w.Code != status {
		f.t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var job setupJobJSON
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		f.t.Fatal(err)
	}
	return job
}

func (f *setupFixture) await(id string, done func(setupJobJSON) bool) setupJobJSON {
	f.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job := f.job(f.request("GET", "/api/agents/setup/"+id, "", ""), 200)
		if done(job) {
			return job
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("job stuck: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func jobEnded(job setupJobJSON) bool { return !job.Active() }

func jobIn(state agentsetup.State) func(setupJobJSON) bool {
	return func(job setupJobJSON) bool { return job.State == state }
}

func (f *setupFixture) start(key, body string) setupJobJSON {
	f.t.Helper()
	return f.job(f.request("POST", "/api/agents/setup", key, body), http.StatusAccepted)
}

func (f *setupFixture) accountDir(id agents.AccountID) string {
	return f.s.agents.runtime.Accounts.AccountDir(id)
}

// profileDirs lists the account directories on disk.
func (f *setupFixture) profileDirs() []string {
	root := filepath.Dir(f.accountDir("acct_probe"))
	entries, _ := os.ReadDir(root)
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

// assertNoLoginProcess fails when a fake login process or its child is alive.
func (f *setupFixture) assertNoLoginProcess() {
	f.t.Helper()
	data, _ := os.ReadFile(f.pidfile)
	for _, field := range strings.Fields(string(data)) {
		pid, _ := strconv.Atoi(field)
		deadline := time.Now().Add(3 * time.Second)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if alive(pid) {
			f.t.Fatalf("login process %d survived", pid)
		}
	}
}

// alive treats a zombie waiting for its reaper as dead.
func alive(pid int) bool {
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

// awaitLoginWaiting waits until a hanging fake login has recorded itself and
// its child. Call f.resetPids before starting that login.
func (f *setupFixture) awaitLoginWaiting() {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(f.pidfile); len(strings.Fields(string(data))) >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("fake login never started")
}

func (f *setupFixture) resetPids() {
	f.t.Helper()
	if err := os.WriteFile(f.pidfile, nil, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func TestSetupJobCreatesCodexProfile(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_EMAIL", "work@example.com")
	subscription, events := f.s.eventHub.subscribe()
	defer f.s.eventHub.unsubscribe(subscription)
	body := `{"provider":"codex","name":"Work","auth_mode":"chatgpt"}`
	job := f.start("create-1", body)
	if job.Kind != agentsetup.KindCreate || job.Provider != codex.ProviderID || job.Name != "Work" || job.AuthMode != "chatgpt" || !job.Active() {
		t.Fatalf("started %+v", job)
	}
	// The same key and body is the same job; another body under it is refused.
	if again := f.job(f.request("POST", "/api/agents/setup", "create-1", body), 200); again.ID != job.ID {
		t.Fatal("retry started a second job")
	}
	if w := f.request("POST", "/api/agents/setup", "create-1", `{"provider":"codex","name":"Other"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "mutation_conflict") {
		t.Fatalf("key reuse: %d %s", w.Code, w.Body.String())
	}
	job = f.await(job.ID, jobEnded)
	if job.State != agentsetup.StateSucceeded || job.AccountID == "" || job.Account["identity"] == nil || !strings.Contains(fmt.Sprint(job.Account["identity"]), "work@example.com") {
		t.Fatalf("result %+v", job)
	}
	if _, err := os.Stat(filepath.Join(f.accountDir(job.AccountID), "home", "auth.json")); err != nil {
		t.Fatal("login did not write auth.json in the profile home")
	}
	// Job changes reach open pages as payload-free agents_changed events.
	changes := 0
	for len(events) > 0 {
		if event := <-events; event.Type == agentsChangedEvent {
			changes++
			if event.ProjectID != "" || event.Projects != nil {
				t.Fatalf("agents_changed carries a payload: %+v", event)
			}
		}
	}
	if changes < 3 {
		t.Fatalf("%d agents_changed events for start, run and end", changes)
	}
	if w := f.request("POST", "/api/agents/setup", "create-2", body); w.Code != 409 || !strings.Contains(w.Body.String(), "name_taken") {
		t.Fatalf("existing name: %d %s", w.Code, w.Body.String())
	}
}

func TestSetupJobRequestValidation(t *testing.T) {
	f := newSetupFixture(t)
	for _, tc := range []struct{ body, code string }{
		{`{"provider":"codex","name":"bad name"}`, "invalid_name"},
		{`{"provider":"codex","name":"` + strings.Repeat("a", 65) + `"}`, "invalid_name"},
		{`{"provider":"codex","name":"ok","auth_mode":"` + strings.Repeat("a", 33) + `"}`, "invalid"},
		{`{"provider":"codex","name":"ok","secret":"` + strings.Repeat("a", 8<<10+1) + `"}`, "invalid"},
		{`{"provider":"codex","name":"ok","extra":1}`, "invalid_json"},
		{`{"provider":"nope","name":"ok"}`, "not_found"},
	} {
		if w := f.request("POST", "/api/agents/setup", "validate", tc.body); w.Code/100 != 4 || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) {
			t.Errorf("%.60s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	if w := f.request("POST", "/api/agents/setup", "", `{"provider":"codex","name":"ok"}`); w.Code != 400 {
		t.Fatal("missing Idempotency-Key accepted")
	}
	if w := f.request("GET", "/api/agents/setup/missing", "", ""); w.Code != 404 {
		t.Fatal("unknown job")
	}
	if len(f.profileDirs()) != f.profiles {
		t.Fatalf("rejected requests left profiles: %v", f.profileDirs())
	}
}

func randomSecret(t *testing.T, prefix string) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return prefix + hex.EncodeToString(b)
}

func TestSetupJobAPIKeyNeverLeaksTheSecret(t *testing.T) {
	f := newSetupFixture(t)
	var secrets []string
	// Pasted in the start body.
	for i := 0; i < 3; i++ {
		secret := randomSecret(t, "sk-proj-")
		secrets = append(secrets, secret)
		job := f.start(fmt.Sprintf("key-body-%d", i), fmt.Sprintf(`{"provider":"codex","name":"Key%d","auth_mode":"api-key","secret":%q}`, i, secret))
		if job = f.await(job.ID, jobEnded); job.State != agentsetup.StateSucceeded {
			t.Fatalf("api-key via body: %+v", job)
		}
		auth, _ := os.ReadFile(filepath.Join(f.accountDir(job.AccountID), "home", "auth.json"))
		if !strings.Contains(string(auth), secret) {
			t.Fatal("the key did not reach codex login --with-api-key")
		}
		// A retry with the same key but another secret is the same request.
		if again := f.job(f.request("POST", "/api/agents/setup", fmt.Sprintf("key-body-%d", i), fmt.Sprintf(`{"provider":"codex","name":"Key%d","auth_mode":"api-key","secret":%q}`, i, randomSecret(t, "sk-"))), 200); again.ID != job.ID {
			t.Fatal("secret is part of the fingerprint")
		}
	}
	// Asked for by the job and answered with POST …/secret.
	secret := randomSecret(t, "sk-svcacct-")
	secrets = append(secrets, secret)
	job := f.start("key-prompt", `{"provider":"codex","name":"Prompted","auth_mode":"api-key","seed":false}`)
	waiting := f.await(job.ID, jobIn(agentsetup.StateWaitingSecret))
	if waiting.Prompt == nil || waiting.Prompt.Label != "OpenAI API key" {
		t.Fatalf("prompt %+v", waiting)
	}
	if w := f.request("POST", "/api/agents/setup/"+job.ID+"/secret", "", fmt.Sprintf(`{"secret":%q}`, secret)); w.Code != 204 {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/agents/setup/"+job.ID+"/secret", "", `{"secret":"again"}`); w.Code != 409 {
		t.Fatalf("second answer: %d", w.Code)
	}
	if job = f.await(job.ID, jobEnded); job.State != agentsetup.StateSucceeded {
		t.Fatalf("api-key via prompt: %+v", job)
	}
	// A bad key fails with the provider's message, which never echoes it.
	bad := "sk-" + randomSecret(t, "bad key ")
	secrets = append(secrets, bad)
	job = f.start("key-bad", fmt.Sprintf(`{"provider":"codex","name":"Bad","auth_mode":"api-key","secret":%q}`, bad))
	if job = f.await(job.ID, jobEnded); job.State != agentsetup.StateFailed || job.Error == "" {
		t.Fatalf("bad key: %+v", job)
	}

	var journal []byte
	_ = f.s.registry.Default().state.View(func(data gitstore.Data) error {
		journal, _ = json.Marshal(data)
		return nil
	})
	index, _ := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(f.accountDir("acct_probe"))), "accounts.json"))
	if len(index) == 0 {
		t.Fatal("account index not found")
	}
	jobsJSON, _ := json.Marshal(f.s.agents.setup.List())
	for _, secret := range secrets {
		for name, haystack := range map[string]string{"responses": f.responses.String(), "server log": f.log.String(), "journal": string(journal), "account index": string(index), "jobs": string(jobsJSON), "fingerprints": fmt.Sprint(f.s.agents.setupKeys.keys)} {
			if strings.Contains(haystack, secret) {
				t.Errorf("secret found in %s", name)
			}
		}
	}
}

func TestSetupJobFailureAndCancelRemoveTheProfile(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_LOGIN_FAIL", "1")
	job := f.start("fail", `{"provider":"codex","name":"Fails","seed":false}`)
	if job = f.await(job.ID, jobEnded); job.State != agentsetup.StateFailed || !strings.Contains(job.Error, "codex login failed") {
		t.Fatalf("failure %+v", job)
	}
	if dirs := f.profileDirs(); len(dirs) != f.profiles {
		t.Fatalf("failed setup left a profile: %v", dirs)
	}
	t.Setenv("FAKE_CODEX_LOGIN_FAIL", "")
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "1")
	f.resetPids()
	job = f.start("cancel", `{"provider":"codex","name":"Canceled","seed":false}`)
	f.awaitLoginWaiting()
	if w := f.request("DELETE", "/api/agents/setup/"+job.ID, "", ""); w.Code != 200 || f.job(w, 200).State != agentsetup.StateCanceled {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	f.assertNoLoginProcess()
	if dirs := f.profileDirs(); len(dirs) != f.profiles {
		t.Fatalf("canceled setup left a profile: %v", dirs)
	}
	if w := f.request("DELETE", "/api/agents/setup/missing", "", ""); w.Code != 404 {
		t.Fatal("cancel of an unknown job")
	}
}

func TestSetupJobTimeoutLimitsAndShutdown(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "1")
	f.s.agents.setup.Timeout = 500 * time.Millisecond
	job := f.start("timeout", `{"provider":"codex","name":"Slow","seed":false}`)
	if job = f.await(job.ID, jobEnded); job.State != agentsetup.StateFailed || !strings.Contains(job.Error, "timed out") {
		t.Fatalf("timeout %+v", job)
	}
	f.assertNoLoginProcess()
	f.s.agents.setup.Timeout = agentsetup.DefaultTimeout

	first := f.start("one", `{"provider":"codex","name":"One","seed":false}`)
	if w := f.request("POST", "/api/agents/setup", "one-again", `{"provider":"codex","name":"One","seed":false}`); w.Code != 409 || !strings.Contains(w.Body.String(), "setup_busy") {
		t.Fatalf("same profile twice: %d %s", w.Code, w.Body.String())
	}
	f.start("two", `{"provider":"codex","name":"Two","seed":false}`)
	if w := f.request("POST", "/api/agents/setup", "three", `{"provider":"codex","name":"Three","seed":false}`); w.Code != 409 || !strings.Contains(w.Body.String(), "setup_limit") {
		t.Fatalf("third job: %d %s", w.Code, w.Body.String())
	}
	f.await(first.ID, jobIn(agentsetup.StateRunning))
	f.s.Close()
	if job, _ := f.s.agents.setup.Get(first.ID); job.State != agentsetup.StateCanceled {
		t.Fatalf("shutdown left %+v", job)
	}
	f.assertNoLoginProcess()
	if dirs := f.profileDirs(); len(dirs) != f.profiles {
		t.Fatalf("shutdown left profiles: %v", dirs)
	}
}

type terminalClient struct {
	t    *testing.T
	conn *websocket.Conn
	out  strings.Builder
	last *agentsetup.Job
}

func (f *setupFixture) attach(id string, offset uint64) (*terminalClient, map[string]any) {
	f.t.Helper()
	url := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/agents/setup/" + id + "/terminal"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"http://" + f.s.expectedHost}})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { conn.Close() })
	_ = conn.WriteJSON(map[string]any{"type": "authenticate", "token": f.token})
	_ = conn.WriteJSON(map[string]any{"type": "attach", "offset": offset})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
		f.t.Fatalf("ready %v %v", ready, err)
	}
	return &terminalClient{t: f.t, conn: conn}, ready
}

func (c *terminalClient) readUntil(done func() bool) {
	c.t.Helper()
	for !done() {
		var frame agentsetup.Frame
		if err := c.conn.ReadJSON(&frame); err != nil {
			c.t.Fatalf("read: %v; output %q", err, c.out.String())
		}
		c.out.Write(frame.Data)
		if frame.Status != nil {
			c.last = frame.Status
		}
	}
}

func (c *terminalClient) readOutput(want string) {
	c.t.Helper()
	c.readUntil(func() bool { return strings.Contains(c.out.String(), want) })
}

func TestSetupTerminalStreamsTheLogin(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_DEVICE_CODE", "ABCD-1234")
	t.Setenv("FAKE_CODEX_LOGIN_PROMPT", "1")
	job := f.start("device", `{"provider":"codex","name":"Device","auth_mode":"device"}`)
	writer, ready := f.attach(job.ID, 0)
	if ready["writer"] != true || ready["version"] != float64(1) {
		t.Fatalf("ready %v", ready)
	}
	observer, ready := f.attach(job.ID, 0)
	if ready["writer"] != false {
		t.Fatal("second attachment writes")
	}
	// The seed report and the device code reach the browser.
	writer.readOutput("Enter code: ")
	if !strings.Contains(writer.out.String(), "nothing to seed: ") || !strings.Contains(writer.out.String(), "\r\n") || !strings.Contains(writer.out.String(), "enter ABCD-1234") || !strings.Contains(writer.out.String(), "SIZE:24 80") {
		t.Fatalf("output %q", writer.out.String())
	}
	if err := writer.conn.WriteJSON(map[string]any{"type": "resize", "cols": 120, "rows": 40}); err != nil {
		t.Fatal(err)
	}
	// Give the resize a moment to land before the login prints its size again.
	time.Sleep(100 * time.Millisecond)
	if err := writer.conn.WriteJSON(map[string]any{"type": "input", "data": []byte("typed-code\r")}); err != nil {
		t.Fatal(err)
	}
	writer.readOutput("SIZE:40 120")
	if !strings.Contains(writer.out.String(), "GOT:typed-code") {
		t.Fatalf("input did not reach the login: %q", writer.out.String())
	}
	writer.readUntil(func() bool { return writer.last != nil && writer.last.State == agentsetup.StateSucceeded })
	observer.readUntil(func() bool { return observer.last != nil && observer.last.State == agentsetup.StateSucceeded })
	if observer.out.String() != writer.out.String() {
		t.Fatal("observer saw different output")
	}

	// Reattaching from the middle replays the rest; a cursor past the end is a gap.
	half := uint64(len(writer.out.String()) / 2)
	resumed, _ := f.attach(job.ID, half)
	resumed.readUntil(func() bool { return resumed.last != nil })
	if resumed.out.String() != writer.out.String()[half:] {
		t.Fatalf("replay from %d: %q", half, resumed.out.String())
	}
	gap, _ := f.attach(job.ID, 1<<40)
	var frame agentsetup.Frame
	if err := gap.conn.ReadJSON(&frame); err != nil || frame.Type != "gap" || frame.Offset != 0 {
		t.Fatalf("gap frame %+v %v", frame, err)
	}

	// An observer's input closes only its own connection.
	_ = observer.conn.WriteJSON(map[string]any{"type": "input", "data": []byte("x")})
	_ = observer.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := observer.conn.ReadJSON(&frame); err == nil {
		t.Fatal("observer input accepted")
	}
}

func TestSetupTerminalRequiresAuthentication(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "1")
	job := f.start("auth", `{"provider":"codex","name":"Auth","seed":false}`)
	url := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/agents/setup/" + job.ID + "/terminal"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"http://" + f.s.expectedHost}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "authenticate", "token": "wrong"})
	_ = conn.WriteJSON(map[string]any{"type": "attach", "offset": 0})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("unauthenticated attach got %v", frame)
	}
	if !isSetupTerminalRoute("/api/agents/setup/"+job.ID+"/terminal") || isSetupTerminalRoute("/api/agents/setup//terminal") || isSetupTerminalRoute("/api/agents/setup/x/terminal/extra") || isSetupTerminalRoute("/api/agents/setup/x") {
		t.Fatal("setup terminal route exception is not exact")
	}
}

func TestSetupJobClaudeTokenRunsSetupTokenThenPrompts(t *testing.T) {
	f := newSetupFixture(t)
	var mu sync.Mutex
	now := time.Now()
	var timers []func()
	f.s.agents.setup.SetClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}, func(_ time.Duration, fire func()) {
		mu.Lock()
		defer mu.Unlock()
		timers = append(timers, fire)
	})
	job := f.start("claude-token", `{"provider":"claude","name":"Token","auth_mode":"token","seed":false}`)
	waiting := f.await(job.ID, jobIn(agentsetup.StateWaitingSecret))
	if waiting.Prompt == nil || waiting.Prompt.Label != "token" {
		t.Fatalf("prompt %+v", waiting)
	}
	client, _ := f.attach(job.ID, 0)
	const printed = "sk-ant-oat01-FAKEFAKEFAKEFAKEFAKEFAKEFAKE"
	client.readOutput(printed)
	if w := f.request("POST", "/api/agents/setup/"+job.ID+"/secret", "", fmt.Sprintf(`{"secret":%q}`, printed)); w.Code != 204 {
		t.Fatalf("answer %d %s", w.Code, w.Body.String())
	}
	job = f.await(job.ID, jobEnded)
	if job.State != agentsetup.StateSucceeded || job.Account["auth_mode"] == nil {
		t.Fatalf("token setup %+v", job)
	}
	// The printed token stays in memory for two minutes only.
	mu.Lock()
	now = now.Add(agentsetup.DefaultRetention)
	fire := timers
	mu.Unlock()
	for _, fire := range fire {
		fire()
	}
	if w := f.request("GET", "/api/agents/setup/"+job.ID, "", ""); w.Code != 404 {
		t.Fatal("job retained after two minutes")
	}
	if strings.Contains(fmt.Sprint(f.s.agents.setup.List()), printed) {
		t.Fatal("token retained")
	}
}

func TestSetupLoginJobSignsAnExistingProfileInAgain(t *testing.T) {
	f := newSetupFixture(t)
	t.Setenv("FAKE_CODEX_EMAIL", "first@example.com")
	job := f.start("create", `{"provider":"codex","name":"Again","seed":false}`)
	created := f.await(job.ID, jobEnded)
	if created.State != agentsetup.StateSucceeded {
		t.Fatalf("create %+v", created)
	}
	path := "/api/agents/accounts/" + string(created.AccountID) + "/login"
	// One sign-in per profile at a time; canceling one keeps the profile.
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "1")
	f.resetPids()
	hanging := f.job(f.request("POST", path, "login-hang", `{}`), http.StatusAccepted)
	if w := f.request("POST", path, "login-busy", `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), "setup_busy") {
		t.Fatalf("second login: %d %s", w.Code, w.Body.String())
	}
	f.awaitLoginWaiting()
	if canceled := f.job(f.request("DELETE", "/api/agents/setup/"+hanging.ID, "", ""), 200); canceled.State != agentsetup.StateCanceled {
		t.Fatalf("cancel %+v", canceled)
	}
	f.assertNoLoginProcess()
	if _, err := f.s.agents.runtime.Accounts.Get(created.AccountID); err != nil {
		t.Fatal("canceled sign-in removed the profile")
	}
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "")
	t.Setenv("FAKE_CODEX_EMAIL", "second@example.com")
	login := f.job(f.request("POST", path, "login-1", `{"auth_mode":"device"}`), http.StatusAccepted)
	if login.Kind != agentsetup.KindLogin || login.AccountID != created.AccountID || login.Name != "Again" || login.AuthMode != "device" {
		t.Fatalf("login job %+v", login)
	}
	login = f.await(login.ID, jobEnded)
	if login.State != agentsetup.StateSucceeded || !strings.Contains(fmt.Sprint(login.Account["identity"]), "second@example.com") || login.Account["auth_mode"] == nil {
		t.Fatalf("login result %+v", login)
	}
	if w := f.request("POST", "/api/agents/accounts/acct_missing/login", "login-3", `{}`); w.Code != 404 {
		t.Fatalf("unknown profile: %d", w.Code)
	}
}

// A profile with a sign-in running is not removed: the login would write its
// credentials into the deleted profile directory and leave them orphaned.
func TestRemoveRefusesAProfileWithARunningSignIn(t *testing.T) {
	f := newSetupFixture(t)
	created := f.await(f.start("create", `{"provider":"codex","name":"Busy","seed":false}`).ID, jobEnded)
	if created.State != agentsetup.StateSucceeded {
		t.Fatalf("create %+v", created)
	}
	t.Setenv("FAKE_CODEX_LOGIN_HANG", "1")
	f.resetPids()
	login := f.job(f.request("POST", "/api/agents/accounts/"+string(created.AccountID)+"/login", "login", `{}`), http.StatusAccepted)
	f.awaitLoginWaiting()
	w := f.request("DELETE", "/api/agents/accounts/"+string(created.AccountID), "", "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "setup_busy") {
		t.Fatalf("remove during sign-in: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(f.accountDir(created.AccountID)); err != nil {
		t.Fatal("refused remove deleted the profile directory")
	}

	f.job(f.request("DELETE", "/api/agents/setup/"+login.ID, "", ""), 200)
	f.assertNoLoginProcess()
	if w := f.request("DELETE", "/api/agents/accounts/"+string(created.AccountID), "", ""); w.Code != 200 {
		t.Fatalf("remove after cancel: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(f.accountDir(created.AccountID)); !os.IsNotExist(err) {
		t.Fatalf("profile directory left behind: %v", err)
	}
}

func TestSetupRoutesNeedTerminalSupport(t *testing.T) {
	f := newSetupFixture(t)
	setupTerminalSupported = false
	t.Cleanup(func() { setupTerminalSupported = true })
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/agents/setup", `{"provider":"codex","name":"x"}`},
		{"POST", "/api/agents/accounts/acct_x/login", `{}`},
		{"GET", "/api/agents/setup/job", ""},
		{"POST", "/api/agents/setup/job/secret", `{"secret":"x"}`},
		{"DELETE", "/api/agents/setup/job", ""},
	} {
		if w := f.request(route.method, route.path, "unsupported", route.body); w.Code != 501 || !strings.Contains(w.Body.String(), "terminal_unsupported") {
			t.Errorf("%s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
		}
	}
}
