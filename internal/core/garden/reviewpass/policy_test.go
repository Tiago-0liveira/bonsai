package reviewpass

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type accountFunc func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error)

func (f accountFunc) ResolveLaunch(ctx context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
	return f(ctx, id, l)
}
func ptr[T any](v T) *T { return &v }
func testPolicy() Policy {
	return Policy{Passes: 1, Reviewer: Reviewer{Provider: "codex", AccountID: "acc", AccountName: "Reviewer", Model: "model", Effort: "high", Native: Native{Sandbox: "workspace-write", Approval: "on-request"}}, Checks: []Check{CheckFrom(config.GardenCheck{Name: "Go tests", Executable: "go", Args: []string{"test", "./..."}})}}
}
func goodResolution() agents.LaunchResolution {
	p := testPolicy()
	r := p.Reviewer
	return agents.LaunchResolution{AccountID: r.AccountID, AccountName: r.AccountName, Provider: r.Provider, Launch: r.Native.launch(r.Model, r.Effort)}
}
func testPrefs() *config.GardenAutoReviewPrefs {
	return &config.GardenAutoReviewPrefs{Enabled: true, Account: "acc", Sandbox: "workspace-write", Approval: "on-request"}
}

func TestResolveLayers(t *testing.T) {
	prefs := testPrefs()
	prefs.Model = "personal-model"
	prefs.Effort = "personal-effort"
	prefs.Checks = []config.GardenCheck{{Name: "Personal", Executable: "go", Args: []string{"vet"}}}
	checks := []config.GardenCheck{{Name: "Run", Executable: "go", Args: []string{"test"}}}
	override := &Override{AccountID: ptr(agents.AccountID("run-acc")), Model: ptr("run-model"), Native: &Native{Sandbox: "read-only", Approval: "never"}, Checks: &checks}
	var requested agents.LaunchOptions
	a := accountFunc(func(_ context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
		if id != "run-acc" {
			t.Fatalf("requested account=%s", id)
		}
		requested = l
		return agents.LaunchResolution{AccountID: id, Provider: "codex", Launch: l}, nil
	})
	out, err := Resolve(context.Background(), prefs, override, a)
	if err != nil || !out.Available || !out.Enabled || out.Reason != "" {
		t.Fatalf("Resolve = %+v, %v", out, err)
	}
	want := agents.LaunchOptions{Model: "run-model", Effort: "personal-effort", Sandbox: "read-only", Approval: "never"}
	if !reflect.DeepEqual(requested, want) {
		t.Fatalf("requested = %+v", requested)
	}
	if out.Sources["enabled"] != SourcePersonal || out.Sources["account"] != SourceRun || out.Sources["model"] != SourceRun || out.Sources["effort"] != SourcePersonal || out.Sources["native"] != SourceRun || out.Sources["checks"] != SourceRun {
		t.Fatalf("sources = %+v", out.Sources)
	}
	if out.Policy.Checks[0].ID != CheckFrom(checks[0]).ID || out.Policy.Validate() != nil {
		t.Fatal("invalid resolved policy")
	}
	// All returned mutable data belongs to the resolution.
	checks[0].Args[0] = "changed"
	if out.Policy.Checks[0].Args[0] != "test" {
		t.Fatal("checks aliased")
	}
	// A run may explicitly disable personal defaults.
	out, err = Resolve(context.Background(), prefs, &Override{Enabled: ptr(false)}, nil)
	if err != nil || out.Enabled || out.Available || out.Sources["enabled"] != SourceRun {
		t.Fatalf("disable = %+v, %v", out, err)
	}
	// A run may also enable built-in defaults with its own account and choices.
	out, err = Resolve(context.Background(), nil, &Override{Enabled: ptr(true), AccountID: ptr(agents.AccountID("acc")), Native: &Native{Sandbox: "workspace-write", Approval: "on-request"}}, accountFunc(func(_ context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
		return agents.LaunchResolution{AccountID: id, Provider: "codex", Launch: l}, nil
	}))
	if err != nil || !out.Available || out.Sources["checks"] != SourceBuiltIn {
		t.Fatalf("enable built-in = %+v, %v", out, err)
	}
}

func TestResolveAvailability(t *testing.T) {
	cases := []struct {
		name       string
		prefs      *config.GardenAutoReviewPrefs
		accounts   bool
		edit       func(*agents.LaunchResolution)
		resolveErr error
		reason     string
	}{
		{name: "built-in", reason: "Automatic review is off."},
		{name: "invalid prefs fail off", prefs: &config.GardenAutoReviewPrefs{Version: 2, Enabled: true, Account: "acc"}, reason: "Automatic review is off."},
		{name: "missing account", prefs: &config.GardenAutoReviewPrefs{}, reason: "Choose a reviewer account."},
		{name: "no service", prefs: testPrefs(), reason: "Agent accounts are unavailable here."},
		{name: "missing account locally", prefs: testPrefs(), accounts: true, resolveErr: errors.Join(errors.New("private"), agents.ErrAccountNotFound), reason: "The reviewer account is not set up on this machine."},
		{name: "provider failed", prefs: testPrefs(), accounts: true, resolveErr: errors.New("private token text"), reason: "The reviewer account cannot launch."},
		{name: "invalid launch", prefs: testPrefs(), accounts: true, resolveErr: agents.ErrInvalidLaunch, reason: "The reviewer account cannot launch."},
		{name: "wrong account", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.AccountID = "wrong" }, reason: "The reviewer account cannot launch."},
		{name: "prompt injection", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.Launch.Prompt = "private" }, reason: "The reviewer account cannot launch."},
		{name: "unsupported", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.Launch.PermissionMode = "private" }, reason: "The reviewer's permission mode is not supported by codex."},
		{name: "unknown provider", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.Provider = "private" }, reason: "The reviewer's permission mode is not supported by unknown provider."},
		{name: "unresolved", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.Launch.Sandbox = "" }, reason: "Choose the reviewer's permissions; its profile leaves them undecided."},
		{name: "bad model", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) { r.Launch.Model = "private bad" }, reason: "The reviewer account cannot launch."},
		{name: "inherited full access", prefs: testPrefs(), accounts: true, edit: func(r *agents.LaunchResolution) {
			r.Launch.Sandbox = "danger-full-access"
			r.Inherited = map[string]bool{"sandbox": true}
		}, reason: "The reviewer profile defaults to full access; choose its permissions explicitly."},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var a Accounts
			if tt.accounts {
				a = accountFunc(func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
					r := goodResolution()
					if tt.edit != nil {
						tt.edit(&r)
					}
					return r, tt.resolveErr
				})
			}
			var o *Override
			if tt.name == "missing account" {
				o = &Override{Enabled: ptr(true)}
			}
			out, err := Resolve(context.Background(), tt.prefs, o, a)
			if err != nil || out.Available || out.Reason != tt.reason || strings.Contains(out.Reason, "private") {
				t.Fatalf("Resolve = %+v, %v", out, err)
			}
			if tt.name == "built-in" {
				for k, s := range out.Sources {
					if s != SourceBuiltIn {
						t.Errorf("%s source=%s", k, s)
					}
				}
			}
		})
	}
}

func TestResolveProfileAndEscalation(t *testing.T) {
	for _, provider := range []agents.ProviderID{"codex", "claude", "antigravity"} {
		for _, inherited := range []bool{false, true} {
			t.Run(string(provider)+"/"+map[bool]string{false: "explicit", true: "inherited"}[inherited], func(t *testing.T) {
				n := Native{Sandbox: "bypass"}
				key := "sandbox"
				if provider == "claude" {
					n = Native{PermissionMode: "auto"}
					key = "permission_mode"
				}
				if provider == "antigravity" {
					n = Native{FullAccess: ptr(true)}
					key = "full_access"
				}
				a := accountFunc(func(_ context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
					return agents.LaunchResolution{AccountID: id, Provider: provider, Launch: n.launch("profile-model", "profile-effort"), Inherited: map[string]bool{key: inherited, "model": true, "effort": true}}, nil
				})
				out, err := Resolve(context.Background(), &config.GardenAutoReviewPrefs{Enabled: true, Account: "acc"}, &Override{Native: &n}, a)
				if err != nil || out.Available == inherited {
					t.Fatalf("Resolve = %+v, %v", out, err)
				}
				if !inherited && (!out.Policy.Reviewer.Escalated || out.Sources["native"] != SourceRun || out.Sources["model"] != SourceProfile || out.Sources["effort"] != SourceProfile || out.Policy.Reviewer.Model != "profile-model" || out.Policy.Reviewer.Effort != "profile-effort") {
					t.Fatalf("profile = %+v", out)
				}
			})
		}
	}
	for _, key := range []string{"sandbox", "approval", "permission_mode", "full_access"} {
		a := accountFunc(func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
			r := goodResolution()
			r.Launch.Sandbox = "danger-full-access"
			r.Inherited = map[string]bool{key: true}
			return r, nil
		})
		out, err := Resolve(context.Background(), testPrefs(), nil, a)
		if err != nil || out.Available || out.Reason != "The reviewer profile defaults to full access; choose its permissions explicitly." {
			t.Fatalf("%s = %+v, %v", key, out, err)
		}
	}
	a := accountFunc(func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
		r := goodResolution()
		r.Inherited = map[string]bool{"sandbox": true, "model": true}
		return r, nil
	})
	out, err := Resolve(context.Background(), testPrefs(), nil, a)
	if err != nil || !out.Available || out.Sources["native"] != SourceProfile || out.Sources["model"] != SourceProfile {
		t.Fatalf("safe profile = %+v, %v", out, err)
	}
}

func TestResolveErrors(t *testing.T) {
	for _, o := range []*Override{{Checks: ptr([]config.GardenCheck{{Name: "shell", Executable: "sh"}})}, {Model: ptr("two words")}, {AccountID: ptr(agents.AccountID(" padded"))}, {Native: &Native{Approval: "\x00"}}} {
		if _, err := Resolve(context.Background(), nil, o, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid override = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		a := accountFunc(func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
			return agents.LaunchResolution{}, errors.Join(errors.New("private"), want)
		})
		if _, err := Resolve(context.Background(), testPrefs(), nil, a); !errors.Is(err, want) {
			t.Fatalf("context = %v", err)
		}
	}
}

func TestPolicyHashAndValidation(t *testing.T) {
	p := testPolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Hash() != testPolicy().Hash() || !hashRE.MatchString(p.Hash()) {
		t.Fatal("unstable hash")
	}
	changes := []func(*Policy){
		func(p *Policy) { p.Reviewer.AccountID = "other" }, func(p *Policy) { p.Reviewer.AccountName = "Other" }, func(p *Policy) { p.Reviewer.Model = "other" }, func(p *Policy) { p.Reviewer.Effort = "low" },
		func(p *Policy) { p.Reviewer.Native.Approval = "never" }, func(p *Policy) { p.Reviewer.Native.Sandbox = "read-only" }, func(p *Policy) { p.Reviewer.Provider = "claude"; p.Reviewer.Native = Native{PermissionMode: "default"} },
		func(p *Policy) { p.Checks[0].Name = "Other" }, func(p *Policy) {
			p.Checks[0] = CheckFrom(config.GardenCheck{Name: "Test", Executable: "go", Args: []string{"test", "./..."}, Dir: "web"})
		},
		func(p *Policy) {
			p.Checks = append(p.Checks, CheckFrom(config.GardenCheck{Name: "Vet", Executable: "go", Args: []string{"vet"}}))
		},
		func(p *Policy) {
			p.Reviewer.Provider = "antigravity"
			p.Reviewer.Native = Native{FullAccess: ptr(false)}
		},
	}
	for i, change := range changes {
		q := testPolicy()
		change(&q)
		if q.Validate() != nil || q.Hash() == p.Hash() {
			t.Fatalf("change %d: hash=%s err=%v", i, q.Hash(), q.Validate())
		}
	}
	q := testPolicy()
	q.Checks = append(q.Checks, CheckFrom(config.GardenCheck{Name: "Vet", Executable: "go", Args: []string{"vet"}}))
	before := q.Hash()
	q.Checks[0], q.Checks[1] = q.Checks[1], q.Checks[0]
	if q.Hash() == before {
		t.Fatal("check order not hashed")
	}
	for _, change := range []func(*Policy){func(p *Policy) { p.Passes = 2 }, func(p *Policy) { p.Reviewer.AccountID = "" }, func(p *Policy) { p.Reviewer.Provider = "" }, func(p *Policy) { p.Reviewer.Native.Sandbox = "" }, func(p *Policy) { p.Reviewer.Escalated = true }, func(p *Policy) { p.Checks[0].ID = "ck-bad" }, func(p *Policy) { p.Checks = append(p.Checks, p.Checks[0]) }, func(p *Policy) { p.Checks[0].Executable = "sh"; p.Checks[0].ID = CheckFrom(p.Checks[0].config()).ID }} {
		q := testPolicy()
		change(&q)
		if !errors.Is(q.Validate(), ErrInvalid) {
			t.Fatalf("invalid policy accepted: %+v", q)
		}
	}
}

func TestResolveCancellationBeforeBackendErrorClassification(t *testing.T) {
	for _, backendErr := range []error{errors.New("provider failed"), agents.ErrAccountNotFound} {
		ctx, cancel := context.WithCancel(context.Background())
		a := accountFunc(func(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
			cancel()
			return agents.LaunchResolution{}, backendErr
		})
		if out, err := Resolve(ctx, testPrefs(), nil, a); !errors.Is(err, context.Canceled) || out.Available || out.Reason != "" {
			t.Fatalf("cancellation classified as unavailable: %+v, %v", out, err)
		}
	}
}
