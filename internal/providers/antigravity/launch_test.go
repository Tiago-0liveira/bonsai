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
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := agents.Account{ID: "acct_golden", Provider: ProviderID}
			if tc.settings != "" {
				account.Settings = []byte(tc.settings)
			}
			provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
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
	provider := New(nil, nil, nil)
	account := agents.Account{ID: "acct_v", Provider: ProviderID}
	yes := true
	if err := provider.ValidateLaunch(account, agents.LaunchOptions{Model: "m", Prompt: "p", FullAccess: &yes, DisplayName: "n"}); err != nil {
		t.Fatal(err)
	}
	for _, launch := range []agents.LaunchOptions{{PermissionMode: "plan"}, {Effort: "high"}} {
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
			provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}, launcher: launcher}
			_, err := provider.SetupAccount(context.Background(), agents.SetupRequest{Account: agents.Account{ID: "acct_s"}, RuntimeDir: t.TempDir(), HomeDir: t.TempDir(), Options: options})
			if err == nil || launcher.calls != 0 {
				t.Fatalf("err = %v, launches = %d", err, launcher.calls)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	provider := New(nil, nil, nil)
	provider.binaryResolver = failingResolver{}
	if provider.Label() != "Antigravity" {
		t.Fatal(provider.Label())
	}
	if a := provider.Availability(context.Background()); a.Available || a.Reason != "Install agy and restart Bonsai" {
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
