package checks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type result struct {
	stdout, stderr string
	code           int
	err            error
}

// fakeEnv answers tool invocations from a table keyed by the joined argv.
type fakeEnv struct {
	tools   map[string]bool
	results map[string]result
	files   map[string]string
	env     map[string]string

	mu    sync.Mutex // Run probes concurrently
	calls []string
}

func (f *fakeEnv) Env() Env {
	return Env{
		GOOS:    "linux",
		HomeDir: "/home/u",
		Getenv:  func(k string) string { return f.env[k] },
		LookPath: func(name string) (string, error) {
			if f.tools[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, name string, args ...string) (string, string, int, error) {
			key := strings.Join(append([]string{name}, args...), " ")
			f.mu.Lock()
			f.calls = append(f.calls, key)
			f.mu.Unlock()
			r, ok := f.results[key]
			if !ok {
				return "", "unknown command", 1, nil
			}
			return r.stdout, r.stderr, r.code, r.err
		},
		Exists: func(path string) bool { _, ok := f.files[path]; return ok },
		ReadFile: func(path string) ([]byte, error) {
			if s, ok := f.files[path]; ok {
				return []byte(s), nil
			}
			return nil, errors.New("missing")
		},
		Port: func(int) PortStatus { return PortStatus{Free: true} },
		Projects: func(context.Context) (ProjectsSummary, error) {
			return ProjectsSummary{Folders: 1, Found: 3, Shown: 3}, nil
		},
	}
}

const ghAuthJSON = "gh auth status --active --hostname github.com --json hosts"

func healthy() *fakeEnv {
	return &fakeEnv{
		tools: map[string]bool{"git": true, "gh": true},
		results: map[string]result{
			"git --version": {stdout: "git version 2.43.0\n"},
			"gh --version":  {stdout: "gh version 2.92.0 (2026-04-28)\nhttps://github.com/cli/cli/releases/tag/v2.92.0\n"},
			ghAuthJSON:      {stdout: `{"hosts":{"github.com":[{"state":"success","active":true,"host":"github.com","login":"octo","tokenSource":"keyring"}]}}`},
			"ngrok version": {stdout: "ngrok version 3.18.4\n"},
		},
		files: map[string]string{},
		env:   map[string]string{},
	}
}

func run(f *fakeEnv, opts Options) map[string]Check {
	if opts.APIPort == 0 {
		opts.APIPort = 7001
	}
	out := map[string]Check{}
	for _, c := range Run(context.Background(), f.Env(), opts) {
		out[c.ID] = c
	}
	return out
}

func TestHealthyMachine(t *testing.T) {
	got := run(healthy(), Options{UpdatesMode: "standard"})
	want := map[string]State{
		IDGit: OK, IDGHInstalled: OK, IDGHAuth: OK, IDProjects: OK, IDPort: OK,
		IDTunnelCloudflared: Skip, IDTunnelNgrok: Skip, IDTunnelTailscale: Skip,
	}
	for id, state := range want {
		if got[id].State != state {
			t.Errorf("%s = %s (%q), want %s", id, got[id].State, got[id].Detail, state)
		}
	}
	if got[IDGit].Detail != "2.43.0" || got[IDGHInstalled].Detail != "2.92.0" || got[IDGHAuth].Detail != "github.com · octo" {
		t.Fatalf("details: %q %q %q", got[IDGit].Detail, got[IDGHInstalled].Detail, got[IDGHAuth].Detail)
	}
	if _, ok := got[IDUpdatesLive]; ok {
		t.Fatal("standard mode must not report the live check")
	}
	if Failed(Run(context.Background(), healthy().Env(), Options{APIPort: 7001})) {
		t.Fatal("healthy machine reported a failure")
	}
}

func TestRunOrderIsStable(t *testing.T) {
	var ids []string
	for _, c := range Run(context.Background(), healthy().Env(), Options{APIPort: 7001, UpdatesMode: "live"}) {
		ids = append(ids, c.ID)
	}
	want := []string{IDGit, IDGHInstalled, IDGHAuth, IDProjects, IDPort, IDTunnelCloudflared, IDTunnelNgrok, IDTunnelTailscale, IDUpdatesLive}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v", ids)
	}
}

func TestGit(t *testing.T) {
	cases := []struct {
		name    string
		tool    bool
		version string
		state   State
		detail  string
	}{
		{"missing", false, "", Fail, "not installed"},
		{"too old", true, "git version 2.34.1", Fail, "2.34.1 is too old; Bonsai needs 2.35 or newer"},
		{"minimum", true, "git version 2.35.0", OK, "2.35.0"},
		{"windows build", true, "git version 2.47.1.windows.2", OK, "2.47.1"},
		{"apple", true, "git version 2.39.5 (Apple Git-154)", OK, "2.39.5"},
		{"garbled", true, "who knows", Fail, "does not report its version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := healthy()
			f.tools["git"] = tc.tool
			f.results["git --version"] = result{stdout: tc.version}
			c := run(f, Options{})[IDGit]
			if c.State != tc.state || !strings.Contains(c.Detail, tc.detail) {
				t.Fatalf("got %s %q", c.State, c.Detail)
			}
			if c.State == Fail && (c.Fix == nil || c.Fix.Inline || strings.Contains(c.Fix.Command, "sudo")) {
				t.Fatalf("failure needs a shown, non-inline, sudo-free fix: %+v", c.Fix)
			}
		})
	}
}

func TestGitInstallFixPerOS(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "xcode-select --install", "windows": "winget install --id Git.Git", "linux": "https://git-scm.com"} {
		if fix := gitInstallFix(goos); !strings.Contains(fix.Command, want) {
			t.Errorf("%s: %q", goos, fix.Command)
		}
	}
}

func TestGHMissingIsAWarningAndSkipsAuth(t *testing.T) {
	f := healthy()
	f.tools["gh"] = false
	got := run(f, Options{})
	if got[IDGHInstalled].State != Warn || got[IDGHInstalled].Fix == nil {
		t.Fatalf("gh.installed = %+v", got[IDGHInstalled])
	}
	if got[IDGHAuth].State != Skip {
		t.Fatalf("gh.auth = %+v", got[IDGHAuth])
	}
	if Failed(Run(context.Background(), f.Env(), Options{APIPort: 7001})) {
		t.Fatal("GitHub is optional; a missing gh must not fail doctor")
	}
}

func TestGHAuth(t *testing.T) {
	t.Run("logged out", func(t *testing.T) {
		f := healthy()
		f.results[ghAuthJSON] = result{stdout: `{"hosts":{}}`}
		c := run(f, Options{})[IDGHAuth]
		if c.State != Warn || c.Fix == nil || !c.Fix.Inline || c.Fix.Command != "gh auth login --hostname github.com" {
			t.Fatalf("got %+v %+v", c, c.Fix)
		}
	})
	t.Run("broken token", func(t *testing.T) {
		f := healthy()
		f.results[ghAuthJSON] = result{stdout: `{"hosts":{"github.com":[{"state":"error","active":true,"login":"octo"}]}}`}
		c := run(f, Options{})[IDGHAuth]
		if c.State != Warn || !strings.Contains(c.Detail, "no longer works") {
			t.Fatalf("got %+v", c)
		}
	})
	t.Run("GH_HOST", func(t *testing.T) {
		f := healthy()
		f.env["GH_HOST"] = "ghe.example.com"
		f.results["gh auth status --active --hostname ghe.example.com --json hosts"] = result{stdout: `{"hosts":{"ghe.example.com":[{"state":"success","active":true,"login":"me"}]}}`}
		c := run(f, Options{})[IDGHAuth]
		if c.State != OK || c.Detail != "ghe.example.com · me" {
			t.Fatalf("got %+v", c)
		}
	})
	t.Run("old gh without --json", func(t *testing.T) {
		f := healthy()
		f.results[ghAuthJSON] = result{stderr: "unknown flag: --json", code: 1}
		f.results["gh auth status --hostname github.com"] = result{stderr: "github.com\n  ✓ Logged in to github.com as octo (keyring)\n"}
		c := run(f, Options{})[IDGHAuth]
		if c.State != OK || c.Detail != "github.com · octo" {
			t.Fatalf("got %+v", c)
		}
	})
	t.Run("old gh logged out", func(t *testing.T) {
		f := healthy()
		f.results[ghAuthJSON] = result{stderr: "unknown flag: --json", code: 1}
		f.results["gh auth status --hostname github.com"] = result{stderr: "You are not logged into any GitHub hosts.", code: 1}
		if c := run(f, Options{})[IDGHAuth]; c.State != Warn {
			t.Fatalf("got %+v", c)
		}
	})
}

func TestGHAuthNeverAsksForTheToken(t *testing.T) {
	f := healthy()
	run(f, Options{})
	for _, call := range f.calls {
		if strings.Contains(call, "token") {
			t.Fatalf("a probe asked for a token: %q", call)
		}
	}
}

func TestProjects(t *testing.T) {
	cases := []struct {
		name    string
		summary ProjectsSummary
		err     error
		state   State
		detail  string
	}{
		{"none", ProjectsSummary{}, nil, Warn, "no project folders yet"},
		{"error", ProjectsSummary{}, errors.New("corrupt"), Warn, "corrupt"},
		{"unavailable", ProjectsSummary{Folders: 2, Unavailable: []string{"/gone"}, Found: 1, Shown: 1}, nil, Warn, "cannot read /gone"},
		{"empty folders", ProjectsSummary{Folders: 1}, nil, Warn, "no repositories found"},
		{"nothing shown", ProjectsSummary{Folders: 1, Found: 4}, nil, Warn, "Settings → Projects"},
		{"ok", ProjectsSummary{Folders: 2, Found: 15, Shown: 12}, nil, OK, "2 folders · 15 repos found · 12 shown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := healthy()
			env := f.Env()
			env.Projects = func(context.Context) (ProjectsSummary, error) { return tc.summary, tc.err }
			c, _ := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDProjects)
			if c.State != tc.state || !strings.Contains(c.Detail, tc.detail) {
				t.Fatalf("got %s %q", c.State, c.Detail)
			}
			if c.State == Warn && c.Fix == nil {
				t.Fatal("warning without a fix")
			}
		})
	}
}

func TestPort(t *testing.T) {
	cases := []struct {
		status PortStatus
		state  State
		detail string
	}{
		{PortStatus{Free: true}, OK, "free"},
		{PortStatus{Web: true}, OK, "bonsai web is running here"},
		{PortStatus{Owner: "pid 4242, node"}, Fail, "port 7001 is used by another program (pid 4242, node)"},
		{PortStatus{Bonsai: true}, Fail, "port 7001 is used by another bonsai local API"},
		{PortStatus{Replaceable: "the per-repo local API of /src/app"}, OK, "used by the per-repo local API of /src/app; bonsai web replaces it when it starts"},
	}
	for _, tc := range cases {
		env := healthy().Env()
		env.Port = func(int) PortStatus { return tc.status }
		c, _ := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDPort)
		if c.State != tc.state || c.Detail != tc.detail {
			t.Errorf("%+v: got %s %q", tc.status, c.State, c.Detail)
		}
		if c.State == Fail && (c.Fix == nil || !strings.Contains(c.Fix.Command, "bonsai web --port 7011")) {
			t.Errorf("fix = %+v", c.Fix)
		}
	}
}

func TestTunnelTools(t *testing.T) {
	f := healthy()
	f.tools["cloudflared"] = true
	f.tools["ngrok"] = true
	f.results["cloudflared --version"] = result{stdout: "cloudflared version 2026.9.3 (built 2026-09-24-16:07 UTC)\n"}
	f.results["ngrok config check"] = result{stdout: "Valid configuration file at /home/u/.config/ngrok/ngrok.yml\n"}
	f.files["/home/u/.config/ngrok/ngrok.yml"] = "version: \"3\"\nagent:\n    authtoken: s3cr3t-value\n"
	got := run(f, Options{})

	cf := got[IDTunnelCloudflared]
	if cf.State != Skip || !strings.Contains(cf.Detail, "installed (2026.9.3)") || !strings.Contains(cf.Detail, "not logged in") {
		t.Fatalf("cloudflared = %+v", cf)
	}
	if cf.Fix == nil || cf.Fix.Command != "cloudflared tunnel login" || !cf.Fix.Inline {
		t.Fatalf("cloudflared fix = %+v", cf.Fix)
	}
	ng := got[IDTunnelNgrok]
	if ng.State != Skip || !strings.Contains(ng.Detail, "3.18.4") || !strings.Contains(ng.Detail, "authtoken set") || ng.Fix != nil {
		t.Fatalf("ngrok = %+v", ng)
	}
	if strings.Contains(ng.Detail, "s3cr3t") {
		t.Fatal("ngrok authtoken leaked into the detail")
	}
	ts := got[IDTunnelTailscale]
	if ts.State != Skip || !strings.Contains(ts.Detail, "not installed") || ts.Fix == nil || strings.Contains(ts.Fix.Command, "sudo") {
		t.Fatalf("tailscale = %+v %+v", ts, ts.Fix)
	}

	f.files[filepath.Join("/home/u", ".cloudflared", "cert.pem")] = "cert"
	f.tools["tailscale"] = true
	f.results["tailscale version"] = result{stdout: "1.76.1\n  tailscale commit: abc\n"}
	f.results["tailscale status --json"] = result{stdout: `{"BackendState":"Running"}`}
	got = run(f, Options{})
	if d := got[IDTunnelCloudflared].Detail; !strings.Contains(d, "logged in to Cloudflare") {
		t.Fatalf("cloudflared = %q", d)
	}
	if d := got[IDTunnelTailscale].Detail; !strings.Contains(d, "installed (1.76.1) · connected") {
		t.Fatalf("tailscale = %q", d)
	}
}

func TestTunnelToolOfTheChosenTunnelCounts(t *testing.T) {
	live := func(tunnel, program string) Options {
		return Options{APIPort: 7001, UpdatesMode: "live", Tunnel: tunnel, TunnelProgram: program}
	}
	// Missing: the chosen tunnel fails, the others stay informational.
	got := run(healthy(), live("cloudflared-quick", "cloudflared"))
	if c := got[IDTunnelCloudflared]; c.State != Fail || !strings.Contains(c.Detail, "your live updates tunnel needs it") || c.Fix == nil {
		t.Fatalf("cloudflared = %+v", c)
	}
	if c := got[IDTunnelTailscale]; c.State != Skip || !strings.Contains(c.Detail, "not used by your tunnel") {
		t.Fatalf("tailscale = %+v", c)
	}
	if c := run(healthy(), Options{})[IDTunnelTailscale]; !strings.Contains(c.Detail, "only needed for live updates") {
		t.Fatalf("standard tailscale = %+v", c)
	}

	f := healthy()
	f.tools["cloudflared"] = true
	f.results["cloudflared --version"] = result{stdout: "cloudflared version 2026.9.3\n"}
	// A quick tunnel needs no login...
	if c := run(f, live("cloudflared-quick", "cloudflared"))[IDTunnelCloudflared]; c.State != OK {
		t.Fatalf("quick = %+v", c)
	}
	// ...a named one warns without it, and is fine with it.
	if c := run(f, live("cloudflared-named", "cloudflared"))[IDTunnelCloudflared]; c.State != Warn || c.Fix == nil || !c.Fix.Inline {
		t.Fatalf("named = %+v", c)
	}
	f.files[filepath.Join("/home/u", ".cloudflared", "cert.pem")] = "cert"
	if c := run(f, live("cloudflared-named", "cloudflared"))[IDTunnelCloudflared]; c.State != OK {
		t.Fatalf("named with cert = %+v", c)
	}

	// ngrok cannot run without its authtoken; tailscale without a connection.
	f.tools["ngrok"] = true
	if c := run(f, live("ngrok", "ngrok"))[IDTunnelNgrok]; c.State != Fail || c.Fix == nil {
		t.Fatalf("ngrok = %+v", c)
	}
	f.tools["tailscale"] = true
	f.results["tailscale version"] = result{stdout: "1.76.1\n"}
	f.results["tailscale status --json"] = result{stdout: `{"BackendState":"Stopped"}`}
	if c := run(f, live("tailscale", "tailscale"))[IDTunnelTailscale]; c.State != Fail || c.Fix == nil || c.Fix.Command != "tailscale up" {
		t.Fatalf("tailscale = %+v", c)
	}

	// A custom command's program is looked up on PATH.
	if c := run(f, live("custom", "mytunnel"))[IDTunnelCustom]; c.State != Fail || c.Fix == nil {
		t.Fatalf("custom = %+v", c)
	}
	f.tools["mytunnel"] = true
	if c := run(f, live("custom", "mytunnel"))[IDTunnelCustom]; c.State != OK {
		t.Fatalf("custom found = %+v", c)
	}
	if _, ok := run(f, live("external-url", ""))[IDTunnelCustom]; ok {
		t.Fatal("no tunnel program, no tunnel command check")
	}
}

func TestLiveUpdates(t *testing.T) {
	cases := []struct {
		name    string
		summary LiveSummary
		err     error
		state   State
		detail  string
		fix     string
	}{
		{"unreadable", LiveSummary{}, errors.New("corrupt"), Warn, "corrupt", "bonsai web status"},
		{"no repos", LiveSummary{Running: true}, nil, Warn, "no repositories picked", "bonsai web setup"},
		{"not running", LiveSummary{Repos: []LiveRepo{{Name: "o/a"}}}, nil, Skip, "1 repo picked · starts with bonsai web", ""},
		{"no tunnel", LiveSummary{Running: true, TunnelError: "the tunnel is not running", Repos: []LiveRepo{{Name: "o/a"}}}, nil, Warn, "no public address: the tunnel is not running", "bonsai web logs tunnel"},
		{"all live", LiveSummary{Running: true, PublicURL: "https://x.trycloudflare.com", Repos: []LiveRepo{{Name: "o/a", State: "live"}, {Name: "o/b", State: "live"}}}, nil, OK, "2 repos live · https://x.trycloudflare.com", ""},
		{"needs admin", LiveSummary{Running: true, PublicURL: "https://x", Repos: []LiveRepo{{Name: "o/a", State: "live"}, {Name: "o/b", State: "needs_admin"}}}, nil, Warn, "1 of 2 repos live · o/b needs admin", "ask an admin of o/b"},
		{"scope", LiveSummary{Running: true, PublicURL: "https://x", Repos: []LiveRepo{{Name: "o/a", State: "scope_missing"}}}, nil, Warn, "cannot be managed with your gh login", "gh auth refresh -h github.com -s admin:repo_hook"},
		{"failing", LiveSummary{Running: true, PublicURL: "https://x", Repos: []LiveRepo{{Name: "o/a", State: "failing", Error: "last delivery got HTTP 502"}}}, nil, Warn, "o/a is failing (last delivery got HTTP 502)", "bonsai web logs tunnel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := healthy().Env()
			env.Live = func(context.Context) (LiveSummary, error) { return tc.summary, tc.err }
			c, ok := Find(Run(context.Background(), env, Options{APIPort: 7001, UpdatesMode: "live"}), IDUpdatesLive)
			if !ok || c.State != tc.state || !strings.Contains(c.Detail, tc.detail) {
				t.Fatalf("got %+v", c)
			}
			if tc.fix == "" && c.State != OK && c.State != Skip {
				t.Fatal("a problem needs a fix")
			}
			if tc.fix != "" && (c.Fix == nil || !strings.Contains(c.Fix.Command, tc.fix)) {
				t.Fatalf("fix = %+v", c.Fix)
			}
		})
	}
	if _, ok := Find(Run(context.Background(), healthy().Env(), Options{APIPort: 7001}), IDUpdatesLive); ok {
		t.Fatal("standard updates have no live check")
	}
}

func TestLeftoverHooks(t *testing.T) {
	env := healthy().Env()
	if _, ok := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDLiveHooks); ok {
		t.Fatal("nowhere to look, no row")
	}
	env.LeftoverHooks = func(context.Context) ([]LeftoverHook, error) { return nil, nil }
	if c, _ := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDLiveHooks); c.State != OK {
		t.Fatalf("none = %+v", c)
	}
	env.LeftoverHooks = func(context.Context) ([]LeftoverHook, error) {
		return []LeftoverHook{{Repo: "o/a", HookID: 7}, {Repo: "o/b", HookID: 9}}, nil
	}
	c, _ := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDLiveHooks)
	if c.State != Warn || !strings.Contains(c.Detail, "o/a, o/b still have a Bonsai webhook") || c.Fix == nil ||
		c.Fix.Command != "gh api -X DELETE repos/o/a/hooks/7 && gh api -X DELETE repos/o/b/hooks/9" || c.Fix.Inline {
		t.Fatalf("got %+v %+v", c, c.Fix)
	}
	env.LeftoverHooks = func(context.Context) ([]LeftoverHook, error) { return nil, errors.New("offline") }
	if c, _ := Find(Run(context.Background(), env, Options{APIPort: 7001}), IDLiveHooks); c.State != Skip || !strings.Contains(c.Detail, "offline") {
		t.Fatalf("error = %+v", c)
	}
}

func TestToolErrorsBecomeStates(t *testing.T) {
	f := healthy()
	f.results["gh --version"] = result{err: errors.New("gh did not answer within 5s"), code: -1}
	c := run(f, Options{})[IDGHInstalled]
	if c.State != Warn || !strings.Contains(c.Detail, "did not answer") {
		t.Fatalf("got %+v", c)
	}
}
