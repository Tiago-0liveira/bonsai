// Package app implements GitHub's domain API without a gh executable.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Tokens must return a repository-scoped installation token for reads and a
// signed-in user's GitHub App access token for mutations. User IDs come from
// trusted server sessions, not request bodies.
type Tokens interface {
	Token(context.Context, string, bool) (string, error)
}
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Tokens  Tokens
}

var _ gh.GitHubService = (*Client)(nil)

func New(tokens Tokens) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, BaseURL: "https://api.github.com", Tokens: tokens}
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func repoPath(repo string) (string, error) {
	if !repoPattern.MatchString(repo) || strings.Contains(repo, "..") {
		return "", domain.ErrInvalid
	}
	return "/repos/" + repo, nil
}
func (c *Client) request(ctx context.Context, repo, method, path string, body, out any) (http.Header, error) {
	return c.do(ctx, repo, method, path, body, out, method != "GET")
}

// do is request with the token scope chosen by the caller. GraphQL queries are
// POSTs that only read, so they ask for the read token.
func (c *Client) do(ctx context.Context, repo, method, path string, body, out any, write bool) (http.Header, error) {
	if _, e := repoPath(repo); e != nil {
		return nil, e
	}
	if c.Tokens == nil {
		return nil, domain.ErrAuth
	}
	token, e := c.Tokens.Token(ctx, repo, write)
	if e != nil {
		return nil, e
	}
	var b []byte
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return nil, e
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := "github_error"
		switch resp.StatusCode {
		case 401:
			code = "unauthorized"
		case 403, 405:
			code = "protected"
		case 404:
			code = "not_found"
		case 409:
			code = "conflict"
		case 422:
			code = "invalid"
		case 429:
			code = "rate_limited"
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			code = "rate_limited"
		}
		var message struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &message)
		if message.Message == "" {
			message.Message = http.StatusText(resp.StatusCode)
		}
		return resp.Header, domain.E(code, message.Message)
	}
	if out != nil && len(raw) > 0 {
		e = json.Unmarshal(raw, out)
	}
	return resp.Header, e
}
func pages[T any](ctx context.Context, c *Client, repo, path string) ([]T, error) {
	items := []T{}
	join := "?"
	if strings.Contains(path, "?") {
		join = "&"
	}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var batch []T
		h, e := c.request(ctx, repo, "GET", path+join+"per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if e != nil {
			return nil, e
		}
		items = append(items, batch...)
		if !strings.Contains(h.Get("Link"), `rel="next"`) {
			return items, nil
		}
	}
}
func pullPath(repo string, n int) (string, error) {
	p, e := repoPath(repo)
	if e != nil {
		return "", e
	}
	if n <= 0 {
		return "", domain.ErrInvalid
	}
	return fmt.Sprintf("%s/pulls/%d", p, n), nil
}
func (c *Client) Repository(ctx context.Context, repo string) (gh.RemoteRepository, error) {
	p, e := repoPath(repo)
	if e != nil {
		return gh.RemoteRepository{}, e
	}
	var v gh.RemoteRepository
	_, e = c.request(ctx, repo, "GET", p, nil, &v)
	return v, e
}
func (c *Client) Branches(ctx context.Context, repo string) ([]gh.RemoteBranch, error) {
	p, e := repoPath(repo)
	if e != nil {
		return nil, e
	}
	rows, e := pages[struct {
		Name      string
		Commit    struct{ SHA string }
		Protected bool
	}](ctx, c, repo, p+"/branches")
	if e != nil {
		return nil, e
	}
	out := []gh.RemoteBranch{}
	for _, b := range rows {
		out = append(out, gh.RemoteBranch{Name: b.Name, RemoteHeadSHA: b.Commit.SHA, Protected: b.Protected})
	}
	return out, nil
}
func escaped(v string) string { return url.PathEscape(v) }
