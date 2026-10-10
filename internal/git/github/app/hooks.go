package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

// Repository webhooks (decision D5): live updates register one hook per
// repository with the gh login's token. GitHub documents repository hooks
// under the classic `repo` scope (gh's default); `admin:repo_hook` adds
// delete and ping for tokens without `repo`.

// Hook is a repository webhook as GitHub reports it. GitHub never returns
// the secret (it answers "********").
type Hook struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	Active       bool         `json:"active"`
	Events       []string     `json:"events"`
	Config       HookConfig   `json:"config"`
	LastResponse HookResponse `json:"last_response"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type HookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type,omitempty"`
	InsecureSSL string `json:"insecure_ssl,omitempty"`
	Secret      string `json:"secret,omitempty"`
}

// HookResponse is the hook's last delivery result. Code is nil (and Status
// "unused") before the first delivery.
type HookResponse struct {
	Code    *int   `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// HookSpec is what a Bonsai hook must look like.
type HookSpec struct {
	URL    string
	Secret string
	Events []string
}

func (s HookSpec) body(create bool) map[string]any {
	body := map[string]any{
		"active": true,
		"events": s.Events,
		"config": HookConfig{URL: s.URL, ContentType: "json", InsecureSSL: "0", Secret: s.Secret},
	}
	if create {
		body["name"] = "web"
	}
	return body
}

// ErrHookScope reports that the token's OAuth scopes do not cover repository
// hooks; ScopeFix is the command that adds them.
var ErrHookScope = domain.E("scope_missing", "the GitHub token cannot manage repository webhooks")

// ScopeFix is the command that grants hook access to the gh login of host.
func ScopeFix(host string) string {
	return "gh auth refresh -h " + host + " -s admin:repo_hook"
}

// hookError turns a 403/404 on a hook call into ErrHookScope when the
// classic token's scopes (X-OAuth-Scopes; absent for fine-grained tokens)
// grant no hook access. A 404 is what GitHub answers a token that may not see
// the hooks at all.
func hookError(header http.Header, err error, write bool) error {
	if err == nil {
		return nil
	}
	code := domain.Code(err)
	if (code != "protected" && code != "not_found") || header == nil {
		return err
	}
	raw, ok := header[http.CanonicalHeaderKey("X-OAuth-Scopes")]
	if !ok {
		return err
	}
	for _, scope := range strings.Split(strings.Join(raw, ","), ",") {
		switch strings.TrimSpace(scope) {
		case "repo", "admin:repo_hook", "write:repo_hook":
			return err
		case "read:repo_hook":
			if !write {
				return err
			}
		}
	}
	return ErrHookScope
}

// RepoAdmin reports whether the token's user administers repo, which
// creating a repository hook requires.
func (c *Client) RepoAdmin(ctx context.Context, repo string) (bool, error) {
	p, err := repoPath(repo)
	if err != nil {
		return false, err
	}
	var body struct {
		Permissions *struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if _, err := c.request(ctx, repo, http.MethodGet, p, nil, &body); err != nil {
		return false, err
	}
	return body.Permissions != nil && body.Permissions.Admin, nil
}

func (c *Client) ListHooks(ctx context.Context, repo string) ([]Hook, error) {
	p, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	// Paged by hand (not pages) to keep the error's headers for the scope
	// check.
	hooks := []Hook{}
	for page := 1; ; page++ {
		var batch []Hook
		header, err := c.request(ctx, repo, http.MethodGet, p+"/hooks?per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if err != nil {
			return nil, hookError(header, err, false)
		}
		hooks = append(hooks, batch...)
		if !strings.Contains(header.Get("Link"), `rel="next"`) {
			return hooks, nil
		}
	}
}

func (c *Client) GetHook(ctx context.Context, repo string, id int64) (Hook, error) {
	p, err := hookPath(repo, id)
	if err != nil {
		return Hook{}, err
	}
	var hook Hook
	header, err := c.request(ctx, repo, http.MethodGet, p, nil, &hook)
	return hook, hookError(header, err, false)
}

// CreateHook adds a Bonsai hook. GitHub pings it right away.
func (c *Client) CreateHook(ctx context.Context, repo string, spec HookSpec) (Hook, error) {
	p, err := repoPath(repo)
	if err != nil {
		return Hook{}, err
	}
	if err := spec.validate(); err != nil {
		return Hook{}, err
	}
	var hook Hook
	header, err := c.request(ctx, repo, http.MethodPost, p+"/hooks", spec.body(true), &hook)
	return hook, hookError(header, err, true)
}

// UpdateHook rewrites a hook's URL, secret and events, and turns it on.
func (c *Client) UpdateHook(ctx context.Context, repo string, id int64, spec HookSpec) (Hook, error) {
	p, err := hookPath(repo, id)
	if err != nil {
		return Hook{}, err
	}
	if err := spec.validate(); err != nil {
		return Hook{}, err
	}
	var hook Hook
	header, err := c.request(ctx, repo, http.MethodPatch, p, spec.body(false), &hook)
	return hook, hookError(header, err, true)
}

func (c *Client) DeleteHook(ctx context.Context, repo string, id int64) error {
	p, err := hookPath(repo, id)
	if err != nil {
		return err
	}
	header, err := c.request(ctx, repo, http.MethodDelete, p, nil, nil)
	return hookError(header, err, true)
}

// PingHook asks GitHub to send the hook a ping event.
func (c *Client) PingHook(ctx context.Context, repo string, id int64) error {
	p, err := hookPath(repo, id)
	if err != nil {
		return err
	}
	header, err := c.request(ctx, repo, http.MethodPost, p+"/pings", nil, nil)
	return hookError(header, err, true)
}

func hookPath(repo string, id int64) (string, error) {
	p, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if id <= 0 {
		return "", domain.ErrInvalid
	}
	return fmt.Sprintf("%s/hooks/%d", p, id), nil
}

func (s HookSpec) validate() error {
	if !strings.HasPrefix(s.URL, "https://") || s.Secret == "" || len(s.Events) == 0 {
		return domain.E("invalid", "a webhook needs an https URL, a secret and events")
	}
	return nil
}

// IsHookScope reports whether err is ErrHookScope.
func IsHookScope(err error) bool { return errors.Is(err, ErrHookScope) }
