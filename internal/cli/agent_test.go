package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestAgentAccountListWorksOutsideGitRepository(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	var out, errOut bytes.Buffer
	if err := RunWithIO([]string{"agent", "account", "list"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("agent list outside git repo: %v (stderr=%q)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "NAME") || !strings.Contains(out.String(), "PROVIDER") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestParseAccountAddArgsValid(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantProvider string
		wantName     string
		wantAuth     string
		wantSeed     *bool
		wantSeedFrom string
	}{
		{name: "plain", args: []string{"claude", "work"}, wantProvider: "claude", wantName: "work"},
		{name: "auth separate", args: []string{"claude", "work", "--auth", "token"}, wantProvider: "claude", wantName: "work", wantAuth: "token"},
		{name: "auth equals", args: []string{"claude", "work", "--auth=token"}, wantProvider: "claude", wantName: "work", wantAuth: "token"},
		{name: "no seed", args: []string{"claude", "work", "--no-seed"}, wantProvider: "claude", wantName: "work", wantSeed: boolPtr(false)},
		{name: "seed from", args: []string{"claude", "work", "--seed-from", "/x"}, wantProvider: "claude", wantName: "work", wantSeedFrom: "/x"},
		{name: "seed from equals", args: []string{"claude", "work", "--seed-from=/x"}, wantProvider: "claude", wantName: "work", wantSeedFrom: "/x"},
		{name: "flags before positionals", args: []string{"--auth", "token", "--no-seed", "claude", "work"}, wantProvider: "claude", wantName: "work", wantAuth: "token", wantSeed: boolPtr(false)},
		{name: "flags between positionals", args: []string{"claude", "--auth=oauth", "work"}, wantProvider: "claude", wantName: "work", wantAuth: "oauth"},
		{name: "flags after positionals", args: []string{"claude", "work", "--seed-from", "/x", "--auth", "token"}, wantProvider: "claude", wantName: "work", wantAuth: "token", wantSeedFrom: "/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, name, options, err := parseAccountAddArgs(tt.args, strings.NewReader(""))
			if err != nil {
				t.Fatal(err)
			}
			if string(provider) != tt.wantProvider || name != tt.wantName {
				t.Fatalf("provider/name = %q/%q, want %q/%q", provider, name, tt.wantProvider, tt.wantName)
			}
			if options.AuthMode != tt.wantAuth {
				t.Fatalf("AuthMode = %q, want %q", options.AuthMode, tt.wantAuth)
			}
			if options.SeedFrom != tt.wantSeedFrom {
				t.Fatalf("SeedFrom = %q, want %q", options.SeedFrom, tt.wantSeedFrom)
			}
			switch {
			case tt.wantSeed == nil && options.Seed != nil:
				t.Fatalf("Seed = %v, want nil", *options.Seed)
			case tt.wantSeed != nil && (options.Seed == nil || *options.Seed != *tt.wantSeed):
				t.Fatalf("Seed = %v, want %v", options.Seed, *tt.wantSeed)
			}
			if options.Secret != nil {
				t.Fatal("Secret set without --token-stdin")
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestParseAccountAddArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		in   io.Reader
	}{
		{name: "unknown flag", args: []string{"claude", "work", "--bogus"}, in: strings.NewReader("")},
		{name: "unknown short flag", args: []string{"claude", "work", "-x"}, in: strings.NewReader("")},
		{name: "auth missing value at end", args: []string{"claude", "work", "--auth"}, in: strings.NewReader("")},
		{name: "auth followed by flag", args: []string{"claude", "work", "--auth", "--no-seed"}, in: strings.NewReader("")},
		{name: "auth empty value", args: []string{"claude", "work", "--auth="}, in: strings.NewReader("")},
		{name: "seed-from missing value", args: []string{"claude", "work", "--seed-from"}, in: strings.NewReader("")},
		{name: "no-seed with value", args: []string{"claude", "work", "--no-seed=x"}, in: strings.NewReader("")},
		{name: "token-stdin with value", args: []string{"claude", "work", "--token-stdin=x"}, in: strings.NewReader("tok")},
		{name: "no-seed and seed-from", args: []string{"claude", "work", "--no-seed", "--seed-from", "/x"}, in: strings.NewReader("")},
		{name: "seed-from and no-seed", args: []string{"claude", "work", "--seed-from=/x", "--no-seed"}, in: strings.NewReader("")},
		{name: "no positionals", args: nil, in: strings.NewReader("")},
		{name: "one positional", args: []string{"claude"}, in: strings.NewReader("")},
		{name: "three positionals", args: []string{"claude", "work", "extra"}, in: strings.NewReader("")},
		{name: "token-stdin empty stdin", args: []string{"claude", "work", "--token-stdin"}, in: strings.NewReader("")},
		{name: "token-stdin whitespace stdin", args: []string{"claude", "work", "--token-stdin"}, in: strings.NewReader(" \t\n\r\n ")},
		{name: "token-stdin nil reader", args: []string{"claude", "work", "--token-stdin"}, in: nil},
		{name: "token-stdin read failure", args: []string{"claude", "work", "--token-stdin"}, in: failingReader{}},
		{name: "token-stdin too large", args: []string{"claude", "work", "--token-stdin"}, in: strings.NewReader(strings.Repeat("a", maxStdinToken+1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, name, options, err := parseAccountAddArgs(tt.args, tt.in)
			if err == nil {
				t.Fatalf("expected error, got provider=%q name=%q options=%+v", provider, name, options)
			}
			if provider != "" || name != "" || !options.Empty() {
				t.Fatalf("non-zero results on error: %q %q %+v", provider, name, options)
			}
		})
	}
}

func TestParseAccountAddArgsTokenStdin(t *testing.T) {
	provider, name, options, err := parseAccountAddArgs([]string{"claude", "work", "--token-stdin"}, strings.NewReader("sk-test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if provider != agents.ProviderID("claude") || name != "work" {
		t.Fatalf("provider/name = %q/%q", provider, name)
	}
	if options.Secret == nil {
		t.Fatal("Secret not set")
	}
	data, err := io.ReadAll(options.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sk-test" {
		t.Fatalf("secret = %q, want %q", data, "sk-test")
	}
}

func TestParseAccountAddArgsTokenStdinTrimsSurroundingWhitespace(t *testing.T) {
	_, _, options, err := parseAccountAddArgs([]string{"--token-stdin", "claude", "work", "--auth", "token"}, strings.NewReader("  sk-test \r\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(options.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sk-test" || options.AuthMode != "token" {
		t.Fatalf("secret = %q auth = %q", data, options.AuthMode)
	}
}

func TestParseAccountAddArgsErrorsNeverContainToken(t *testing.T) {
	const token = "sk-secret-token-value"
	tests := []struct {
		name string
		args []string
		in   io.Reader
	}{
		{name: "unknown flag", args: []string{"claude", "work", "--token-stdin", "--bogus"}},
		{name: "conflicting seed flags", args: []string{"claude", "work", "--token-stdin", "--no-seed", "--seed-from", "/x"}},
		{name: "wrong positional count", args: []string{"claude", "--token-stdin"}},
		{name: "too large", args: []string{"claude", "work", "--token-stdin"}, in: strings.NewReader(token + strings.Repeat("a", maxStdinToken))},
		{name: "read failure", args: []string{"claude", "work", "--token-stdin"}, in: io.MultiReader(strings.NewReader(token), failingReader{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			if in == nil {
				in = strings.NewReader(token)
			}
			_, _, _, err := parseAccountAddArgs(tt.args, in)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("error leaks token: %v", err)
			}
		})
	}
}
