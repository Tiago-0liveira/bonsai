package antigravity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// TestLaunchArgsGolden pins the argv the web launch produced before launch
// handling moved out of the terminal manager. Every case must stay byte-identical.
func TestLaunchArgsGolden(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name     string
		settings string
		explicit []string
		launch   agents.LaunchOptions
		want     []string
	}{
		{name: "empty", want: []string{}},
		{name: "profile model", settings: `{"model":"p"}`, want: []string{"--model", "p"}},
		{name: "profile full access", settings: `{"model":"p","dangerously_skip_permissions":true}`, want: []string{"--model", "p", "--dangerously-skip-permissions"}},
		{name: "launch model wins", settings: `{"model":"p"}`, launch: agents.LaunchOptions{Model: "c"}, want: []string{"--model", "c"}},
		{name: "full access off overrides profile", settings: `{"model":"p","dangerously_skip_permissions":true}`, launch: agents.LaunchOptions{Model: "c", FullAccess: &no}, want: []string{"--model", "c"}},
		{name: "full access on", launch: agents.LaunchOptions{FullAccess: &yes}, want: []string{"--dangerously-skip-permissions"}},
		{name: "full access on, profile model", settings: `{"model":"p"}`, launch: agents.LaunchOptions{FullAccess: &yes}, want: []string{"--model", "p", "--dangerously-skip-permissions"}},
		{name: "prompt", launch: agents.LaunchOptions{Prompt: "Make a report"}, want: []string{"--prompt-interactive=Make a report"}},
		{name: "dash prompt", launch: agents.LaunchOptions{Prompt: "--literal task"}, want: []string{"--prompt-interactive=--literal task"}},
		{name: "multiline prompt", launch: agents.LaunchOptions{Prompt: "a \"b\"\n$(c) `d`"}, want: []string{"--prompt-interactive=a \"b\"\n$(c) `d`"}},
		{name: "everything", settings: `{"model":"p"}`, launch: agents.LaunchOptions{Model: "c", FullAccess: &yes, Prompt: "go"}, want: []string{"--model", "c", "--dangerously-skip-permissions", "--prompt-interactive=go"}},
		{name: "display name ignored", launch: agents.LaunchOptions{DisplayName: "named"}, want: []string{}},
		{name: "cli explicit model wins", settings: `{"model":"p","dangerously_skip_permissions":true}`, explicit: []string{"--model", "e"}, want: []string{"--model", "e", "--dangerously-skip-permissions"}},
		{name: "cli explicit args first", explicit: []string{"--help"}, settings: `{"model":"p"}`, want: []string{"--help", "--model", "p"}},
		// Efforts are composed into the model ID; agy never gets --effort.
		{name: "grouped model takes its default effort", launch: agents.LaunchOptions{Model: "gemini-3.8-flash"}, want: []string{"--model", "gemini-3.8-flash-high"}},
		{name: "model and effort", launch: agents.LaunchOptions{Model: "gemini-3.8-flash", Effort: "low"}, want: []string{"--model", "gemini-3.8-flash-low"}},
		{name: "full id passes through", settings: `{"effort":"medium"}`, launch: agents.LaunchOptions{Model: "gemini-3.8-flash-low"}, want: []string{"--model", "gemini-3.8-flash-low"}},
		{name: "full id with an explicit effort swaps it", launch: agents.LaunchOptions{Model: "gemini-3.8-flash-low", Effort: "medium"}, want: []string{"--model", "gemini-3.8-flash-medium"}},
		{name: "profile model and effort", settings: `{"model":"gemini-3.8-flash","effort":"medium"}`, want: []string{"--model", "gemini-3.8-flash-medium"}},
		{name: "profile effort skipped for a model without it", settings: `{"model":"claude-sonnet-4-6","effort":"low"}`, want: []string{"--model", "claude-sonnet-4-6"}},
		{name: "lone medium model stays whole", launch: agents.LaunchOptions{Model: "gpt-oss-120b-medium"}, want: []string{"--model", "gpt-oss-120b-medium"}},
		{name: "effort dropped for a model without efforts", launch: agents.LaunchOptions{Model: "claude-opus-4-6-thinking", Effort: "high"}, want: []string{"--model", "claude-opus-4-6-thinking"}},
		{name: "unlisted model with effort", launch: agents.LaunchOptions{Model: "gemini-9", Effort: "high"}, want: []string{"--model", "gemini-9-high"}},
		{name: "explicit --model in args wins over effort", explicit: []string{"--model", "x-low"}, launch: agents.LaunchOptions{Model: "gemini-3.8-flash", Effort: "high"}, want: []string{"--model", "x-low"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := agents.Account{ID: "acct_golden", Provider: ProviderID}
			if tc.settings != "" {
				account.Settings = []byte(tc.settings)
			}
			provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
			seedModels(t, provider, account.ID)
			prepared, err := provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
				Account: account, Session: agents.Session{ID: "sess_golden", WorkDir: t.TempDir(), HomeDir: t.TempDir()},
				Args: tc.explicit, Launch: tc.launch,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := prepared.Args
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("args = %q, want %q", got, tc.want)
			}
			if prepared.Executable != "agy-test" || prepared.ProviderSessionID != "" || len(prepared.EnvUnsetPrefixes) != 0 {
				t.Fatalf("prepared = %+v", prepared)
			}
		})
	}
}

func TestLaunchDoesNotChangeProfileSettings(t *testing.T) {
	yes := true
	account := agents.Account{ID: "acct_golden", Provider: ProviderID, Settings: []byte(`{"model":"p"}`)}
	provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
	seedModels(t, provider, account.ID)
	if _, err := provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
		Account: account, Session: agents.Session{WorkDir: t.TempDir(), HomeDir: t.TempDir()},
		Launch: agents.LaunchOptions{FullAccess: &yes},
	}); err != nil {
		t.Fatal(err)
	}
	if string(account.Settings) != `{"model":"p"}` {
		t.Fatalf("settings = %s", account.Settings)
	}
}

func TestValidateLaunch(t *testing.T) {
	provider := New(nil, nil)
	account := agents.Account{ID: "acct_v", Provider: ProviderID}
	yes := true
	if err := provider.ValidateLaunch(account, agents.LaunchOptions{Model: "m", Prompt: "p", FullAccess: &yes, DisplayName: "n"}); err != nil {
		t.Fatal(err)
	}
	for _, launch := range []agents.LaunchOptions{{PermissionMode: "plan"}, {Sandbox: "read-only"}, {Approval: "never"}, {Effort: "-x"}, {Effort: "HIGH"}} {
		if err := provider.ValidateLaunch(account, launch); err == nil {
			t.Fatalf("accepted %+v", launch)
		}
	}
	// Without a cached list any well-formed effort is accepted.
	if err := provider.ValidateLaunch(account, agents.LaunchOptions{Model: "gemini-3.8-flash", Effort: "ultra"}); err != nil {
		t.Fatal(err)
	}
	// With one, the effort must be the model's.
	seedModels(t, provider, account.ID)
	// A model without efforts launches, its leftover effort dropped.
	for _, launch := range []agents.LaunchOptions{{Model: "gemini-3.8-flash", Effort: "low"}, {Model: "gemini-3.8-flash-low", Effort: "high"}, {Model: "unlisted", Effort: "high"}, {Model: "claude-sonnet-4-6", Effort: "high"}, {Model: "gpt-oss-120b-medium", Effort: "medium"}} {
		if err := provider.ValidateLaunch(account, launch); err != nil {
			t.Fatalf("%+v: %v", launch, err)
		}
	}
	for _, launch := range []agents.LaunchOptions{{Model: "gemini-3.8-flash", Effort: "ultra"}} {
		if err := provider.ValidateLaunch(account, launch); err == nil {
			t.Fatalf("accepted %+v", launch)
		}
	}
	account.Settings = []byte(`{`)
	if err := provider.ValidateLaunch(account, agents.LaunchOptions{}); err == nil {
		t.Fatal("accepted unreadable settings")
	}
}

func TestSetupRejectsUnsupportedOptions(t *testing.T) {
	no := false
	for name, options := range map[string]agents.SetupOptions{
		"auth":      {AuthMode: "token"},
		"secret":    {Secret: strings.NewReader("x")},
		"no seed":   {Seed: &no},
		"seed from": {SeedFrom: "/x"},
	} {
		t.Run(name, func(t *testing.T) {
			launcher := &providerTestLauncher{}
			provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
			_, err := provider.SetupAccount(context.Background(), agents.SetupRequest{Account: agents.Account{ID: "acct_s"}, RuntimeDir: t.TempDir(), HomeDir: t.TempDir(), Options: options, IO: agents.SetupIO{Launcher: launcher}})
			if err == nil || launcher.calls != 0 {
				t.Fatalf("err = %v, launches = %d", err, launcher.calls)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	provider := New(nil, nil)
	provider.binaryResolver = failingResolver{}
	if provider.Label() != "Antigravity" {
		t.Fatal(provider.Label())
	}
	if a := provider.Availability(context.Background()); a.Available || a.Reason != "Install agy and restart Bonsai" || a.Hint == "" {
		t.Fatalf("availability = %+v", a)
	}
	provider.binaryResolver = &providerTestBinaryResolver{path: "agy"}
	if a := provider.Availability(context.Background()); !a.Available {
		t.Fatalf("availability = %+v", a)
	}
	info := provider.DescribeAccount(context.Background(), agents.Account{Settings: []byte(`{"dangerously_skip_permissions":true}`)})
	if info.Options["full_access"] != true || info.AuthMode != "" || info.Identity != "" {
		t.Fatalf("info = %+v", info)
	}
}

type failingResolver struct{}

func (failingResolver) Resolve() (string, error) { return "", errors.New("missing") }

func TestLaunchFieldsDeclareEffortAndFullAccess(t *testing.T) {
	provider := New(nil, nil)
	fields := provider.LaunchFields()
	if len(fields) != 2 || fields[0].ID != "effort" || !fields[0].PerModel || fields[1].ID != "full_access" || fields[1].Kind != agents.LaunchFieldToggle || fields[1].Help == "" {
		t.Fatalf("fields = %+v", fields)
	}
	if got := agents.FieldValues(fields[0]); !reflect.DeepEqual(got, []string{"low", "medium", "high"}) {
		t.Fatalf("efforts = %v", got)
	}
	options := fields[1].Options
	if len(options) != 2 || options[0].Value != "false" || options[1].Value != "true" || options[1].Tone != agents.ToneDanger || options[0].Tone != "" {
		t.Fatalf("options = %+v", options)
	}
	account := agents.Account{ID: "acct_f", Provider: ProviderID}
	for _, option := range options {
		on := option.Value == "true"
		if err := provider.ValidateLaunch(account, agents.LaunchOptions{FullAccess: &on}); err != nil {
			t.Fatalf("declared %q rejected: %v", option.Value, err)
		}
	}
}
