// Package ghcli uses the local gh login for GitHub while sharing the
// production domain normalization. Requests run in-process with the token from
// `gh auth token` (see Shared); the `gh api` subprocess transport remains as a
// fallback for when gh cannot hand out a token.
package ghcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Service struct {
	*app.Client
	shared *Shared
	host   string
}

// RateLimit returns the last core rate-limit window seen for this service's
// host. It reports false for the subprocess-only service.
func (s *Service) RateLimit() (Rate, bool) {
	if s.shared == nil {
		return Rate{}, false
	}
	return s.shared.RateLimit(s.host)
}

type tokens struct{}

func (tokens) Token(context.Context, string, bool) (string, error) { return "gh-cli", nil }

type transport struct{ Dir string }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	path := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	args := []string{"api", "--method", r.Method, "--include", "-H", "Accept: application/vnd.github+json", path}
	if r.Body != nil {
		args = append(args, "--input", "-")
	}
	trace.AddGH()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = t.Dir
	cmd.Stdin = r.Body
	// --include output is parsed as HTTP, so terminal styling must be disabled
	// even when Bonsai inherits an environment that forces CLI color.
	cmd.Env = ghEnv(cmd.Environ())
	// gh --include emits an HTTP status and headers even on API failures.
	out, e := cmd.Output()
	if len(out) == 0 && e != nil {
		return nil, e
	}
	return http.ReadResponse(bufioReader(string(out)), r)
}

// New returns a service that always spawns `gh api`. Production code uses
// Shared.Service, which only falls back to it.
func New(dir string) *Service {
	c := app.New(tokens{})
	c.HTTP = &http.Client{Transport: transport{Dir: dir}, Timeout: 30 * time.Second}
	return &Service{Client: c}
}

type RepositoryContext struct {
	FullName      string
	DefaultBranch string
}

func Discover(ctx context.Context, dir string) (RepositoryContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	trace.AddGH()
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "nameWithOwner,defaultBranchRef")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return RepositoryContext{}, fmt.Errorf("discover GitHub repository with gh: %w", err)
	}
	var payload struct {
		NameWithOwner    string `json:"nameWithOwner"`
		DefaultBranchRef *struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return RepositoryContext{}, fmt.Errorf("decode gh repo view: %w", err)
	}
	if payload.NameWithOwner == "" || payload.DefaultBranchRef == nil {
		return RepositoryContext{}, fmt.Errorf("gh repo view returned incomplete repository metadata")
	}
	return RepositoryContext{FullName: payload.NameWithOwner, DefaultBranch: payload.DefaultBranchRef.Name}, nil
}

// Kept separate to make the transport straightforward to exercise with fake gh.
func bufioReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }
