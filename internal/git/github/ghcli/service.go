// Package ghcli retains local gh authentication while sharing the production
// domain normalization. It is never constructed by the web server.
package ghcli

import (
	"bufio"
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Service struct{ *app.Client }
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
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = t.Dir
	cmd.Stdin = r.Body
	// gh --include emits an HTTP status and headers even on API failures.
	out, e := cmd.Output()
	if len(out) == 0 && e != nil {
		return nil, e
	}
	return http.ReadResponse(bufioReader(string(out)), r)
}
func New(dir string) *Service {
	c := app.New(tokens{})
	c.HTTP = &http.Client{Transport: transport{Dir: dir}, Timeout: 30 * time.Second}
	return &Service{c}
}

// Kept separate to make the transport straightforward to exercise with fake gh.
func bufioReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }
