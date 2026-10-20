// Package garden adapts the Garden ports to GitHub through the ghcli HTTP client.
package garden

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gitdomain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
)

const maxResponse = 4 << 20
const maxRequest = 1 << 20
const maxComment = 64 << 10
const maxPages = 100
const maxLinkHeader = 16 << 10
const graphqlPath = "/graphql"
const metadataPrefix = "<!-- bonsai:garden-metadata/v1 "
const defaultBase = "https://api.github.com"

// repoCacheTTL bounds how long a verified repository name is trusted before the numeric ID is re-checked.
const repoCacheTTL = time.Minute

// maxProjectPages bounds Project pagination; a longer list fails closed instead of being treated as complete.
const maxProjectPages = 10

// Adapter uses the authenticated ghcli client. Tests inject an HTTP client backed by a fake RoundTripper.
type Adapter struct {
	client  *http.Client
	baseURL string
	// now is a seam for cache expiry and Retry-After computation.
	now          func() time.Time
	mu           sync.RWMutex
	repositories map[core.RepoID]cachedRepo
	issueNumbers map[core.RepoID]map[core.IssueID]int64
}

// cachedRepo is a repository observation. verified is zero for a Seed entry whose numeric ID has not been
// confirmed yet.
type cachedRepo struct {
	repoJSON
	verified time.Time
}

// IssueReader and IssueWriter expose separate capabilities over the same authenticated transport. They hold
// the Adapter privately so a reader cannot reach setup or write methods.
type IssueReader struct{ a *Adapter }
type IssueWriter struct{ a *Adapter }

func (a *Adapter) Reader() *IssueReader { return &IssueReader{a: a} }
func (a *Adapter) Writer() *IssueWriter { return &IssueWriter{a: a} }

// New requires a non-nil HTTP client. Only an https origin without a path is supported: GitHub Enterprise
// API bases such as https://host/api/v3 fail closed in this phase.
func New(client *http.Client, baseURL string) (*Adapter, error) {
	if client == nil {
		return nil, ErrNilHTTPClient
	}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = defaultBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, ErrUnsupportedBase
	}
	return &Adapter{client: client, baseURL: base, now: time.Now}, nil
}

// FromService reuses ghcli's authenticated, bounded transport. The conditional read cache applies only
// through that shared transport: this adapter sends no If-None-Match itself.
func FromService(service *ghcli.Service) (*Adapter, error) {
	if service == nil || service.Client == nil {
		return nil, ErrNilHTTPClient
	}
	return New(service.Client.HTTP, service.Client.BaseURL)
}

// Seed records a previously known repository name so a warm start can read before any resolver ran. The
// entry is unverified: the first use re-fetches the repository and fails with ErrRepositoryIdentityChanged
// when the numeric ID differs. An existing observation is never replaced.
func (a *Adapter) Seed(id core.RepoID, owner, name string) error {
	if id == 0 {
		return core.ErrInvalidID
	}
	o, n, err := splitFullName(owner + "/" + name)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.repositories[id]; ok {
		return nil
	}
	if a.repositories == nil {
		a.repositories = map[core.RepoID]cachedRepo{}
	}
	a.repositories[id] = cachedRepo{repoJSON: repoJSON{ID: int64(id), FullName: o + "/" + n}}
	return nil
}

func (a *Adapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}
func (a *Adapter) endpoint(path string) string { return a.baseURL + path }

// splitFullName validates "owner/name". Dot segments are rejected so a name can never rewrite the path.
func splitFullName(fullName string) (string, string, error) {
	p := strings.Split(fullName, "/")
	if len(p) != 2 || p[0] == "" || p[1] == "" || p[0] == "." || p[0] == ".." || p[1] == "." || p[1] == ".." {
		return "", "", core.ErrInvalidID
	}
	return p[0], p[1], nil
}
func repoNamePath(fullName string) (string, error) {
	owner, name, err := splitFullName(fullName)
	if err != nil {
		return "", err
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}
func (a *Adapter) rememberRepository(r repoJSON) {
	if r.ID <= 0 || r.FullName == "" {
		return
	}
	a.mu.Lock()
	if a.repositories == nil {
		a.repositories = map[core.RepoID]cachedRepo{}
	}
	a.repositories[core.RepoID(r.ID)] = cachedRepo{repoJSON: r, verified: a.clock()}
	a.mu.Unlock()
}
func (a *Adapter) repository(id core.RepoID) (cachedRepo, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r, ok := a.repositories[id]
	return r, ok
}

// repositoryPath returns the API path for a repository this adapter has seen or been seeded with. A fresh
// verified entry is reused; otherwise the repository is re-fetched and its numeric ID must still match.
// A 404 or 403 on that re-fetch means a previously readable Garden is no longer readable.
func (a *Adapter) repositoryPath(ctx context.Context, id core.RepoID) (string, error) {
	r, ok := a.repository(id)
	if !ok {
		return "", connect.ErrNoGitHubRemote
	}
	path, err := repoNamePath(r.FullName)
	if err != nil {
		return "", err
	}
	if !r.verified.IsZero() && a.clock().Sub(r.verified) < repoCacheTTL {
		return path, nil
	}
	resp, body, err := a.request(ctx, "GET", path, nil)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrForbidden) {
			return "", safe(core.ErrPermissionDenied)
		}
		return "", err
	}
	var current repoJSON
	current.Scope = resp.Header.Get("X-OAuth-Scopes")
	if json.Unmarshal(body, &current) != nil || current.ID <= 0 {
		return "", safe(connect.ErrOffline)
	}
	if core.RepoID(current.ID) != id {
		return "", connect.ErrRepositoryIdentityChanged
	}
	a.rememberRepository(current)
	return repoNamePath(current.FullName)
}
func (a *Adapter) rememberIssue(repo core.RepoID, id core.IssueID, n int64) {
	a.mu.Lock()
	if a.issueNumbers == nil {
		a.issueNumbers = map[core.RepoID]map[core.IssueID]int64{}
	}
	if a.issueNumbers[repo] == nil {
		a.issueNumbers[repo] = map[core.IssueID]int64{}
	}
	a.issueNumbers[repo][id] = n
	a.mu.Unlock()
}
func (a *Adapter) forgetIssue(repo core.RepoID, id core.IssueID) {
	a.mu.Lock()
	delete(a.issueNumbers[repo], id)
	a.mu.Unlock()
}

// issueNumber maps a stable Issue ID to its REST number. Pull requests share the number space and the
// list endpoint, so they are skipped: a PR ID must never resolve to a number this adapter acts on.
func (a *Adapter) issueNumber(ctx context.Context, repo core.RepoID, id core.IssueID) (int64, error) {
	a.mu.RLock()
	n := a.issueNumbers[repo][id]
	a.mu.RUnlock()
	if n > 0 {
		return n, nil
	}
	base, e := a.repositoryPath(ctx, repo)
	if e != nil {
		return 0, e
	}
	path := base + "/issues?state=all&per_page=100"
	for page := 1; page <= maxPages; page++ {
		resp, b, e := a.request(ctx, "GET", path, nil)
		if e != nil {
			return 0, e
		}
		var rows []issueJSON
		if json.Unmarshal(b, &rows) != nil {
			return 0, safe(connect.ErrOffline)
		}
		for _, v := range rows {
			if isPullRequest(v.PullRequest) {
				continue
			}
			if v.ID > 0 && v.Number > 0 {
				a.rememberIssue(repo, core.IssueID(v.ID), v.Number)
				if core.IssueID(v.ID) == id {
					return v.Number, nil
				}
			}
		}
		next, e := nextLinkPath(resp.Header.Get("Link"), a.baseURL, base+"/issues", page)
		if e != nil {
			return 0, core.ErrInvalidCursor
		}
		if next == "" {
			return 0, core.ErrNotFound
		}
		u, _ := url.Parse(next)
		path = u.RequestURI()
	}
	return 0, safe(ErrTruncated)
}

func (a *Adapter) request(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil || len(b) > maxRequest {
			return nil, nil, safe(connect.ErrOffline)
		}
		rd = strings.NewReader(string(b))
	}
	req, e := http.NewRequestWithContext(ctx, method, a.endpoint(path), rd)
	if e != nil {
		return nil, nil, safe(connect.ErrOffline)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := a.client.Do(req)
	if e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, nil, ctx.Err()
		}
		if gitdomain.Code(e) == "github_auth" || errors.Is(e, gitdomain.ErrAuth) {
			return nil, nil, authRequired()
		}
		return nil, nil, safe(connect.ErrOffline)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	resp.Body.Close()
	if e != nil || len(b) > maxResponse {
		return resp, nil, safe(connect.ErrOffline)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, b, a.statusError(resp)
	}
	return resp, b, nil
}
func safe(e error) error { return fmt.Errorf("Garden GitHub request failed: %w", e) }

// kinds is a fixed-text error that matches several sentinels, so callers of either the connect or the
// garden vocabulary can classify it. Response text is never included.
type kinds struct {
	msg  string
	errs []error
}

func (k kinds) Error() string   { return k.msg }
func (k kinds) Unwrap() []error { return k.errs }

func authRequired() error {
	return safe(kinds{connect.ErrAuthRequired.Error(), []error{connect.ErrAuthRequired, core.ErrAuthRequired}})
}
func rateLimited(after time.Duration) error {
	return safe(kinds{connect.ErrRateLimited.Error(), []error{connect.ErrRateLimited, core.RateLimitError{RetryAfter: after}}})
}

// statusError maps a non-2xx response. A 403 is a rate limit only when GitHub says so (Retry-After or an
// exhausted x-ratelimit-remaining); any other 403 is uncertain and must not be read as revoked access.
func (a *Adapter) statusError(r *http.Response) error {
	switch r.StatusCode {
	case 401:
		return authRequired()
	case 404, 410:
		// 410 means the Issue or repository is gone or transferred, not that the network failed.
		return safe(core.ErrNotFound)
	case 403:
		if r.Header.Get("Retry-After") != "" || r.Header.Get("X-RateLimit-Remaining") == "0" {
			return rateLimited(retryAfter(r.Header, a.clock()))
		}
		return safe(core.ErrForbidden)
	case 405:
		return safe(core.ErrForbidden)
	case 422:
		return safe(connect.ErrGitHubValidation)
	case 429:
		return rateLimited(retryAfter(r.Header, a.clock()))
	}
	return safe(connect.ErrOffline)
}

// retryAfter reads Retry-After (seconds or HTTP date), falling back to x-ratelimit-reset. Zero means unknown;
// consumers bound the value.
func retryAfter(h http.Header, now time.Time) time.Duration {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs > 0 {
			if secs > int64(math.MaxInt64/int64(time.Second)) {
				return 0
			}
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil && t.After(now) {
			return t.Sub(now)
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs > 0 && secs < math.MaxInt32*4 {
			if t := time.Unix(secs, 0); t.After(now) {
				return t.Sub(now)
			}
		}
	}
	return 0
}

// RepositoryResolver resolves the standard HTTPS or SCP GitHub remote form.
type RepositoryResolver struct{ Adapter *Adapter }

func (r RepositoryResolver) ResolveRepository(ctx context.Context, remote connect.Remote) (connect.Repository, error) {
	owner, name, e := remoteRepo(remote.URL)
	if e != nil {
		return connect.Repository{}, connect.ErrNoGitHubRemote
	}
	var x repoJSON
	resp, b, e := r.Adapter.request(ctx, "GET", "/repos/"+owner+"/"+name, nil)
	if e != nil {
		return connect.Repository{}, e
	}
	x.Scope = resp.Header.Get("X-OAuth-Scopes")
	if json.Unmarshal(b, &x) != nil || x.ID <= 0 || x.FullName == "" {
		return connect.Repository{}, safe(connect.ErrOffline)
	}
	r.Adapter.rememberRepository(x)
	access := permission(x.Permissions)
	parent := core.RepoID(0)
	parentName := ""
	if x.Parent != nil {
		parent = core.RepoID(x.Parent.ID)
		parentName = x.Parent.FullName
	}
	return connect.Repository{ID: core.RepoID(x.ID), FullName: x.FullName, Fork: x.Fork, ParentID: parent, ParentFullName: parentName, Access: access, ProjectScope: scopePresent(x.Scope), ProjectScopeKnown: x.Scope != ""}, nil
}
func (r RepositoryResolver) ResolveRepositoryID(ctx context.Context, id core.RepoID, fullName string) (connect.Repository, error) {
	path, e := repoNamePath(fullName)
	if e != nil {
		return connect.Repository{}, connect.ErrNoGitHubRemote
	}
	var x repoJSON
	resp, b, e := r.Adapter.request(ctx, "GET", path, nil)
	if e != nil {
		return connect.Repository{}, e
	}
	x.Scope = resp.Header.Get("X-OAuth-Scopes")
	if json.Unmarshal(b, &x) != nil || x.ID <= 0 || x.FullName == "" {
		return connect.Repository{}, safe(connect.ErrOffline)
	}
	if core.RepoID(x.ID) != id {
		return connect.Repository{}, connect.ErrRepositoryIdentityChanged
	}
	r.Adapter.rememberRepository(x)
	return connect.Repository{ID: core.RepoID(x.ID), FullName: x.FullName, Fork: x.Fork, Access: permission(x.Permissions), ProjectScope: scopePresent(x.Scope), ProjectScopeKnown: x.Scope != ""}, nil
}

// Viewer reads the authenticated account. The numeric ID is the stable identity (approval policy);
// the login is a mutable display label that also anchors Project ownership checks.
func (a *Adapter) Viewer(ctx context.Context) (connect.GitHubIdentity, error) {
	_, b, e := a.request(ctx, "GET", "/user", nil)
	if e != nil {
		return connect.GitHubIdentity{}, e
	}
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if json.Unmarshal(b, &u) != nil || u.ID <= 0 || u.Login == "" {
		return connect.GitHubIdentity{}, safe(connect.ErrOffline)
	}
	return connect.GitHubIdentity{ID: userID(u.ID), Login: u.Login}, nil
}

type repoJSON struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	NodeID   string `json:"node_id"`
	Owner    struct {
		Login  string `json:"login"`
		NodeID string `json:"node_id"`
	} `json:"owner"`
	Fork   bool `json:"fork"`
	Parent *struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	} `json:"parent"`
	Permissions map[string]bool `json:"permissions"`
	Scope       string          `json:"-"`
}

func permission(p map[string]bool) connect.Access {
	if p["admin"] {
		return connect.AccessAdmin
	}
	if p["push"] || p["maintain"] {
		return connect.AccessWrite
	}
	// The repository was fetched successfully, so it is readable. A missing, null or all-false
	// permissions object never grants more than observation.
	return connect.AccessRead
}
func scopePresent(s string) bool {
	for _, v := range strings.Split(s, ",") {
		if strings.TrimSpace(v) == "project" || strings.TrimSpace(v) == "read:project" {
			return true
		}
	}
	return false
}
func remoteRepo(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "git@github.com:") {
		s = strings.TrimPrefix(s, "git@github.com:")
	} else {
		u, e := url.Parse(s)
		if e != nil || !strings.EqualFold(u.Host, "github.com") || (u.Scheme != "https" && u.Scheme != "ssh") {
			return "", "", errors.New("invalid")
		}
		s = u.Path
	}
	s = strings.TrimSuffix(s, ".git")
	owner, name, err := splitFullName(strings.Trim(s, "/"))
	if err != nil {
		return "", "", errors.New("invalid")
	}
	return url.PathEscape(owner), url.PathEscape(name), nil
}

// isPullRequest reports whether an issue row is a pull request. The key must be present and not the literal
// JSON null.
func isPullRequest(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

// userID renders a numeric author ID, or "" when GitHub reported no usable user (deleted account). An empty
// ID is never trusted as an approver.
func userID(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// issueFromRow converts a managed, non-PR row. A row without a usable ID or number cannot be addressed, so it
// is emitted with ID 0 and the reader reports it as an Attention item instead of failing the page. A row
// without a usable author is emitted with a problem marker.
func (a *Adapter) issueFromRow(repo core.RepoID, v issueJSON) core.Issue {
	labels := make([]string, 0, len(v.Labels))
	for _, l := range v.Labels {
		labels = append(labels, l.Name)
	}
	x := core.Issue{Repo: repo, Number: v.Number, Title: v.Title, NodeID: v.NodeID, AuthorID: userID(v.User.ID), CreatedAt: v.CreatedAt, Labels: labels, Body: v.Body, Metadata: issueMetadata(v.Body)}
	if v.ID > 0 && v.Number > 0 {
		x.ID = core.IssueID(v.ID)
		a.rememberIssue(repo, x.ID, v.Number)
	}
	if v.User.ID <= 0 {
		x.Problems = append(x.Problems, core.IssueProblemAuthor)
	}
	return x
}

// ListManagedIssues reads a bounded page and only emits a cursor from a validated next Link.
func (r *IssueReader) ListManagedIssues(ctx context.Context, repo core.RepoID, cursor string) (core.IssuePage, error) {
	if repo <= 0 {
		return core.IssuePage{}, core.ErrInvalidID
	}
	basePath, err := r.a.repositoryPath(ctx, repo)
	if err != nil {
		return core.IssuePage{}, err
	}
	path := basePath + "/issues?state=all&per_page=100&labels=bonsai%3Amanaged"
	current := 1
	if cursor != "" {
		path, current, err = decodeCursor(cursor, r.a.baseURL, basePath+"/issues")
		if err != nil {
			return core.IssuePage{}, core.ErrInvalidCursor
		}
	}
	resp, b, e := r.a.request(ctx, "GET", path, nil)
	if e != nil {
		return core.IssuePage{}, e
	}
	var rows []issueJSON
	if json.Unmarshal(b, &rows) != nil {
		return core.IssuePage{}, safe(connect.ErrOffline)
	}
	out := core.IssuePage{Complete: true}
	for _, v := range rows {
		if isPullRequest(v.PullRequest) {
			continue
		}
		managed := false
		for _, label := range v.Labels {
			if label.Name == core.ManagedLabel {
				managed = true
				break
			}
		}
		if !managed {
			continue
		}
		out.Items = append(out.Items, r.a.issueFromRow(repo, v))
	}
	next, e := nextLinkPath(resp.Header.Get("Link"), r.a.baseURL, basePath+"/issues", current)
	if e != nil {
		return core.IssuePage{}, core.ErrInvalidCursor
	}
	if next != "" {
		out.NextCursor = encodeCursor(next)
		out.Complete = false
	}
	return out, nil
}

type issueJSON struct {
	ID        int64     `json:"id"`
	Number    int64     `json:"number"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	NodeID    string    `json:"node_id"`
	Body      string    `json:"body"`
	User      struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// GetIssue reads one Issue. A pull request, or a response whose identity differs from the request, is never
// returned: the cached number is evicted so a stale mapping cannot keep addressing the wrong object.
func (r *IssueReader) GetIssue(ctx context.Context, repo core.RepoID, id core.IssueID) (core.Issue, error) {
	if id <= 0 {
		return core.Issue{}, core.ErrInvalidID
	}
	number, e := r.a.issueNumber(ctx, repo, id)
	if e != nil {
		return core.Issue{}, e
	}
	base, e := r.a.repositoryPath(ctx, repo)
	if e != nil {
		return core.Issue{}, e
	}
	_, b, e := r.a.request(ctx, "GET", base+"/issues/"+strconv.FormatInt(number, 10), nil)
	if e != nil {
		return core.Issue{}, e
	}
	var v issueJSON
	if json.Unmarshal(b, &v) != nil || v.ID <= 0 {
		return core.Issue{}, safe(connect.ErrOffline)
	}
	if isPullRequest(v.PullRequest) {
		r.a.forgetIssue(repo, id)
		return core.Issue{}, safe(core.ErrNotFound)
	}
	if core.IssueID(v.ID) != id || v.Number != number {
		r.a.forgetIssue(repo, id)
		return core.Issue{}, safe(core.ErrNotFound)
	}
	return r.a.issueFromRow(repo, v), nil
}

// issueMetadata reads only the bounded, first-line canonical projection; arbitrary body text is never decoded as metadata.
func issueMetadata(body string) []byte {
	line := body
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSuffix(line[:i], "\r")
	}
	if !strings.HasPrefix(line, metadataPrefix) || !strings.HasSuffix(line, " -->") || len(line) > core.MaxMetadataBytes+len(metadataPrefix)+len(" -->") {
		return nil
	}
	b := []byte(strings.TrimSuffix(strings.TrimPrefix(line, metadataPrefix), " -->"))
	if len(b) == 0 || len(b) > core.MaxMetadataBytes || !json.Valid(b) {
		return nil
	}
	return b
}

// ListComments reads the full comment log. More than core.MaxComments comments fails with
// core.ErrTooManyComments; the log is never silently truncated into a verifiable one.
func (r *IssueReader) ListComments(ctx context.Context, repo core.RepoID, id core.IssueID) ([]core.IssueComment, error) {
	var all []core.IssueComment
	number, e := r.a.issueNumber(ctx, repo, id)
	if e != nil {
		return nil, e
	}
	base, e := r.a.repositoryPath(ctx, repo)
	if e != nil {
		return nil, e
	}
	commentsPath := base + "/issues/" + strconv.FormatInt(number, 10) + "/comments"
	path := commentsPath + "?per_page=100"
	for page := 1; page <= maxPages; page++ {
		resp, b, e := r.a.request(ctx, "GET", path, nil)
		if e != nil {
			return nil, e
		}
		var rows []commentJSON
		if json.Unmarshal(b, &rows) != nil {
			return nil, safe(connect.ErrOffline)
		}
		for _, v := range rows {
			all = append(all, v.comment())
			if len(all) > core.MaxComments {
				return all, core.ErrTooManyComments
			}
		}
		next, e := nextLinkPath(resp.Header.Get("Link"), r.a.baseURL, commentsPath, page)
		if e != nil {
			return nil, core.ErrInvalidCursor
		}
		if next == "" {
			return all, nil
		}
		nextURL, _ := url.Parse(next)
		path = nextURL.RequestURI()
	}
	return all, core.ErrTooManyComments
}

type commentJSON struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func (c commentJSON) comment() core.IssueComment {
	return core.IssueComment{ID: c.ID, Author: c.User.Login, AuthorID: userID(c.User.ID), Body: c.Body, CreatedAt: c.CreatedAt}
}

func (w *IssueWriter) AppendComment(ctx context.Context, repo core.RepoID, id core.IssueID, body string) (core.IssueComment, error) {
	if len(body) > maxComment {
		return core.IssueComment{}, core.ErrCommentTooLarge
	}
	number, e := w.a.issueNumber(ctx, repo, id)
	if e != nil {
		return core.IssueComment{}, e
	}
	base, e := w.a.repositoryPath(ctx, repo)
	if e != nil {
		return core.IssueComment{}, e
	}
	_, b, e := w.a.request(ctx, "POST", base+"/issues/"+strconv.FormatInt(number, 10)+"/comments", map[string]string{"body": body})
	if e != nil {
		return core.IssueComment{}, e
	}
	var v commentJSON
	if json.Unmarshal(b, &v) != nil || v.ID <= 0 {
		return core.IssueComment{}, safe(connect.ErrOffline)
	}
	return v.comment(), nil
}

// UpdateLabels reconciles only bonsai: labels. It reads the Issue first through GetIssue, which rejects
// pull requests, so no label write can reach a PR.
func (w *IssueWriter) UpdateLabels(ctx context.Context, repo core.RepoID, id core.IssueID, labels []string) error {
	desired := map[string]bool{}
	for _, label := range labels {
		if !strings.HasPrefix(strings.ToLower(label), "bonsai:") {
			return core.ErrInvalidMetadata
		}
		desired[label] = true
	}
	current, err := w.a.Reader().GetIssue(ctx, repo, id)
	if err != nil {
		return err
	}
	number, err := w.a.issueNumber(ctx, repo, id)
	if err != nil {
		return err
	}
	base, err := w.a.repositoryPath(ctx, repo)
	if err != nil {
		return err
	}
	for _, label := range current.Labels {
		if strings.HasPrefix(strings.ToLower(label), "bonsai:") && !desired[label] {
			_, _, err = w.a.request(ctx, "DELETE", base+"/issues/"+strconv.FormatInt(number, 10)+"/labels/"+url.PathEscape(label), nil)
			if err != nil {
				return err
			}
		}
	}
	for label := range desired {
		found := false
		for _, existing := range current.Labels {
			if existing == label {
				found = true
				break
			}
		}
		if !found {
			_, _, err = w.a.request(ctx, "POST", base+"/issues/"+strconv.FormatInt(number, 10)+"/labels", map[string][]string{"labels": {label}})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeCursor(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// decodeCursor validates a cursor this adapter minted and returns the request path plus its page number.
func decodeCursor(c, base, expectedPath string) (string, int, error) {
	b, e := base64.RawURLEncoding.DecodeString(c)
	if e != nil {
		return "", 0, e
	}
	u, e := url.Parse(string(b))
	if e != nil {
		return "", 0, e
	}
	page, pe := strconv.Atoi(u.Query().Get("page"))
	if u.Scheme != "https" || !strings.EqualFold(u.Host, mustURL(base).Host) || u.User != nil || u.Path != expectedPath || !validIssueQuery(u.Query(), page) || pe != nil || page < 2 || page > maxPages {
		return "", 0, errors.New("invalid")
	}
	return u.RequestURI(), page, nil
}
func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
func validListQuery(q url.Values, page int) bool {
	if q.Get("labels") != "" {
		return validIssueQuery(q, page)
	}
	if q.Get("state") != "all" || q.Get("per_page") != "100" || q.Get("page") != strconv.Itoa(page) {
		return false
	}
	for k := range q {
		if k != "state" && k != "per_page" && k != "page" {
			return false
		}
	}
	return true
}
func validIssueQuery(q url.Values, page int) bool {
	if q.Get("state") != "all" || q.Get("per_page") != "100" || q.Get("labels") != "bonsai:managed" || q.Get("page") != strconv.Itoa(page) {
		return false
	}
	for k := range q {
		if k != "state" && k != "per_page" && k != "labels" && k != "page" {
			return false
		}
	}
	return true
}

var errBadLink = errors.New("invalid link header")

// link is one parsed Link header entry.
type link struct {
	target string
	rels   []string
}

// parseLinks parses an RFC 8288 Link header. Commas and semicolons inside <...> or quoted strings do not
// split entries. Any malformed input fails closed, so a corrupt header can never be read as "no next page".
func parseLinks(h string) ([]link, error) {
	if len(h) > maxLinkHeader {
		return nil, errBadLink
	}
	var out []link
	s := strings.TrimSpace(h)
	for s != "" {
		if s[0] != '<' {
			return nil, errBadLink
		}
		end := strings.IndexByte(s, '>')
		if end < 0 {
			return nil, errBadLink
		}
		l := link{target: s[1:end]}
		s = s[end+1:]
		for {
			s = strings.TrimLeft(s, " \t")
			if s == "" {
				break
			}
			if s[0] == ',' {
				s = strings.TrimLeft(s[1:], " \t")
				break
			}
			if s[0] != ';' {
				return nil, errBadLink
			}
			s = strings.TrimLeft(s[1:], " \t")
			var key, val string
			i := strings.IndexAny(s, "=;,")
			if i < 0 {
				key, s = s, ""
			} else {
				key, s = s[:i], s[i:]
			}
			if strings.HasPrefix(s, "=") {
				s = s[1:]
				if strings.HasPrefix(s, `"`) {
					j := 1
					for ; j < len(s) && s[j] != '"'; j++ {
						if s[j] == '\\' {
							j++
						}
					}
					if j >= len(s) {
						return nil, errBadLink
					}
					val, s = s[1:j], s[j+1:]
				} else {
					j := strings.IndexAny(s, ";,")
					if j < 0 {
						j = len(s)
					}
					val, s = strings.TrimSpace(s[:j]), s[j:]
				}
			}
			if strings.EqualFold(strings.TrimSpace(key), "rel") {
				for _, rel := range strings.Fields(val) {
					l.rels = append(l.rels, strings.ToLower(rel))
				}
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// nextLinkPath returns the validated rel="next" URL, or "" when the header has none. The target must stay on
// the API origin and expected path, and its page must be exactly current+1, so a server can neither loop
// the reader nor skip pages. More than one next link fails closed.
func nextLinkPath(h, base, expectedPath string, current int) (string, error) {
	links, err := parseLinks(h)
	if err != nil {
		return "", err
	}
	raw := ""
	for _, l := range links {
		for _, rel := range l.rels {
			if rel == "next" {
				if raw != "" {
					return "", errBadLink
				}
				raw = l.target
			}
		}
	}
	if raw == "" {
		return "", nil
	}
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	b := mustURL(base)
	if !u.IsAbs() {
		u = b.ResolveReference(u)
	}
	page, pe := strconv.Atoi(u.Query().Get("page"))
	if u.Scheme != "https" || !strings.EqualFold(u.Host, b.Host) || u.User != nil || u.Path != expectedPath || u.RawQuery == "" || pe != nil || page != current+1 || page > maxPages || (strings.HasSuffix(expectedPath, "/issues") && !validListQuery(u.Query(), page)) {
		return "", errors.New("hostile link")
	}
	return u.String(), nil
}

var _ core.IssueReader = (*IssueReader)(nil)
var _ core.IssueWriter = (*IssueWriter)(nil)
var _ connect.RepositoryResolver = RepositoryResolver{}
var _ connect.RepositoryByIDResolver = RepositoryResolver{}

// ListLabels observes repository labels.
func (a *Adapter) ListLabels(ctx context.Context, r core.RepoID) ([]connect.Label, error) {
	var out []connect.Label
	base, err := a.repositoryPath(ctx, r)
	if err != nil {
		return nil, err
	}
	path := base + "/labels?per_page=100"
	for page := 1; page <= maxPages; page++ {
		resp, b, e := a.request(ctx, "GET", path, nil)
		if e != nil {
			return nil, e
		}
		var rows []struct{ Name, Color, Description string }
		if json.Unmarshal(b, &rows) != nil {
			return nil, safe(connect.ErrOffline)
		}
		for _, x := range rows {
			out = append(out, connect.Label{Name: x.Name, Color: x.Color, Description: x.Description})
		}
		next, e := nextLinkPath(resp.Header.Get("Link"), a.baseURL, base+"/labels", page)
		if e != nil {
			return nil, core.ErrInvalidCursor
		}
		if next == "" {
			return out, nil
		}
		nextURL, _ := url.Parse(next)
		path = nextURL.RequestURI()
	}
	return nil, safe(ErrTruncated)
}

// CreateLabel creates a label and never modifies or deletes an existing label.
func (a *Adapter) CreateLabel(ctx context.Context, r core.RepoID, l connect.Label) error {
	base, e := a.repositoryPath(ctx, r)
	if e != nil {
		return e
	}
	_, _, e = a.request(ctx, "POST", base+"/labels", map[string]string{"name": l.Name, "color": l.Color, "description": l.Description})
	if errors.Is(e, connect.ErrGitHubValidation) {
		// A duplicate may race with observation; only the observed state decides whether it exists.
		labels, observeErr := a.ListLabels(ctx, r)
		if observeErr != nil {
			return observeErr
		}
		for _, old := range labels {
			if old.Name == l.Name && old.Color == l.Color && old.Description == l.Description {
				return nil
			}
		}
	}
	return e
}

// projectsQuery pages the repository owner's Projects (user or organization); pageInfo lets the caller
// refuse a truncated list. Field names are UNVERIFIED against primary GitHub documentation (see the contract).
const projectsQuery = `query GardenProjects($owner:String!,$after:String) { repositoryOwner(login:$owner) { login ... on ProjectV2Owner { projectsV2(first:100, after:$after) { pageInfo { hasNextPage endCursor } nodes { id title number shortDescription creator { login } fields(first:100) { pageInfo { hasNextPage } nodes { ... on ProjectV2Field { id name } ... on ProjectV2SingleSelectField { id name options { id name } } } } } } } } }`

// projectOwnerLogin returns the verified repository owner whose Projects are observed. The owner always
// comes from the resolved repository, never from caller input.
func (a *Adapter) projectOwnerLogin(ctx context.Context, id core.RepoID) (string, error) {
	if _, err := a.repositoryPath(ctx, id); err != nil {
		return "", err
	}
	r, _ := a.repository(id)
	if r.Owner.Login != "" {
		return r.Owner.Login, nil
	}
	owner, _, err := splitFullName(r.FullName)
	return owner, err
}

// ListProjects observes the Projects V2 of the repository's owner with fields and creator identity, setting
// Project.Owner to that owner login. It pages up to maxProjectPages and fails closed with ErrTruncated when
// Projects or a Project's fields remain unread, so an absent same-name Project is never concluded from a
// partial list. Item values (IssueFields) are not read by this phase.
func (a *Adapter) ListProjects(ctx context.Context, repo core.RepoID) ([]core.Project, error) {
	owner, err := a.projectOwnerLogin(ctx, repo)
	if err != nil {
		return nil, err
	}
	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	var d struct {
		Owner *struct {
			Login    string `json:"login"`
			Projects struct {
				PageInfo pageInfo `json:"pageInfo"`
				Nodes    []struct {
					ID, Title, ShortDescription string
					Number                      int
					Creator                     *struct {
						Login string `json:"login"`
					} `json:"creator"`
					Fields struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							ID, Name string
							Options  []struct{ Name string } `json:"options"`
						} `json:"nodes"`
					} `json:"fields"`
				} `json:"nodes"`
			} `json:"projectsV2"`
		} `json:"repositoryOwner"`
	}
	out := []core.Project{}
	var after any
	for i := 0; i < maxProjectPages; i++ {
		d.Owner = nil
		h, e := a.graphql(ctx, projectsQuery, map[string]any{"owner": owner, "after": after}, &d, "read:project")
		if e != nil {
			return nil, e
		}
		if known, ok := projectScope(h, "read:project"); known && !ok {
			return nil, connect.ErrScopeMissing{Scope: "read:project"}
		}
		if d.Owner == nil {
			return nil, safe(core.ErrNotFound)
		}
		// A different login would attribute foreign Projects to this repository's owner.
		if !strings.EqualFold(d.Owner.Login, owner) {
			return nil, safe(core.ErrGraphQL)
		}
		for _, p := range d.Owner.Projects.Nodes {
			if p.Fields.PageInfo.HasNextPage {
				return nil, safe(ErrTruncated)
			}
			x := core.Project{ID: p.ID, Name: p.Title, Number: strconv.Itoa(p.Number), Owner: owner, Description: p.ShortDescription}
			if p.Creator != nil {
				x.CreatorLogin = p.Creator.Login
			}
			for _, f := range p.Fields.Nodes {
				pf := core.ProjectField{ID: f.ID, Name: f.Name}
				for _, o := range f.Options {
					pf.Values = append(pf.Values, o.Name)
				}
				x.Fields = append(x.Fields, pf)
			}
			out = append(out, x)
		}
		if !d.Owner.Projects.PageInfo.HasNextPage {
			return out, nil
		}
		if d.Owner.Projects.PageInfo.EndCursor == "" {
			return nil, safe(ErrTruncated)
		}
		after = d.Owner.Projects.PageInfo.EndCursor
	}
	return nil, safe(ErrTruncated)
}

// EnsureProject creates a Project V2. Existing projects are only observed by ListProjects.
func (a *Adapter) EnsureProject(ctx context.Context, r core.RepoID, name string) (core.Project, error) {
	return a.ensureProject(ctx, r, name, "")
}

// EnsureProjectWithMarker creates a Project carrying the local operation marker in its description. It is
// part of connect's OperationProjects port.
func (a *Adapter) EnsureProjectWithMarker(ctx context.Context, r core.RepoID, name, marker string) (core.Project, error) {
	return a.ensureProject(ctx, r, name, marker)
}
func (a *Adapter) ensureProject(ctx context.Context, r core.RepoID, name, marker string) (core.Project, error) {
	projects, e := a.ListProjects(ctx, r)
	if e != nil {
		return core.Project{}, e
	}
	for _, p := range projects {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	if _, ownerErr := a.repositoryPath(ctx, r); ownerErr != nil {
		return core.Project{}, ownerErr
	}
	ownerData, _ := a.repository(r)
	if ownerData.Owner.NodeID == "" || ownerData.NodeID == "" {
		return core.Project{}, safe(connect.ErrOffline)
	}
	var d struct {
		Create struct {
			Project struct {
				ID, Title, ShortDescription string
				Number                      int
				Creator                     *struct {
					Login string `json:"login"`
				} `json:"creator"`
			} `json:"projectV2"`
		} `json:"createProjectV2"`
	}
	h, e := a.graphql(ctx, createProjectQuery, map[string]any{"ownerId": ownerData.Owner.NodeID, "title": name, "repositoryId": ownerData.NodeID}, &d, "project")
	if e != nil {
		return core.Project{}, e
	}
	if known, ok := projectScope(h, "project"); known && !ok {
		return core.Project{}, connect.ErrScopeMissing{Scope: "project"}
	}
	p := d.Create.Project
	if p.ID == "" {
		return core.Project{}, safe(connect.ErrOffline)
	}
	if marker != "" {
		const update = `mutation GardenMarkProject($project:ID!,$description:String!){updateProjectV2(input:{projectId:$project,shortDescription:$description}){projectV2{id shortDescription}}}`
		var updated struct {
			Update struct {
				Project struct{ ID, ShortDescription string } `json:"projectV2"`
			} `json:"updateProjectV2"`
		}
		_, ue := a.graphql(ctx, update, map[string]any{"project": p.ID, "description": marker}, &updated, "project")
		if ue != nil {
			return core.Project{}, ue
		}
		p.ShortDescription = updated.Update.Project.ShortDescription
	}
	out := core.Project{ID: p.ID, Name: p.Title, Number: strconv.Itoa(p.Number), Owner: ownerData.Owner.Login, Description: p.ShortDescription}
	if p.Creator != nil {
		out.CreatorLogin = p.Creator.Login
	}
	return out, nil
}

const createProjectQuery = `mutation GardenCreateProject($ownerId:ID!,$title:String!,$repositoryId:ID!){createProjectV2(input:{ownerId:$ownerId,title:$title,repositoryId:$repositoryId}){projectV2{id title number shortDescription creator{login}}}}`

// EnsureField creates only a missing single-select field; it does not change existing fields.
func (a *Adapter) EnsureField(ctx context.Context, projectID string, w core.ProjectField) (core.ProjectField, error) {
	w = core.CanonicalSetupField(w)
	const q = `mutation GardenCreateField($project:ID!,$name:String!,$type:ProjectV2CustomFieldType!,$options:[ProjectV2SingleSelectFieldOptionInput!]){createProjectV2Field(input:{projectId:$project,name:$name,dataType:$type,singleSelectOptions:$options}){projectV2Field{... on ProjectV2Field{id name} ... on ProjectV2SingleSelectField{id name options{name}}}}}`
	opts := []map[string]string{}
	for _, v := range w.Values {
		opts = append(opts, map[string]string{"name": v, "color": "GRAY", "description": ""})
	}
	var d struct {
		Create struct {
			Field struct {
				ID, Name string
				Options  []struct{ Name string } `json:"options"`
			} `json:"projectV2Field"`
		} `json:"createProjectV2Field"`
	}
	typ := "TEXT"
	var options any
	if len(w.Values) > 0 {
		typ = "SINGLE_SELECT"
		options = opts
	}
	h, e := a.graphql(ctx, q, map[string]any{"project": projectID, "name": w.Name, "type": typ, "options": options}, &d, "project")
	if e != nil {
		return core.ProjectField{}, e
	}
	if known, ok := projectScope(h, "project"); known && !ok {
		return core.ProjectField{}, connect.ErrScopeMissing{Scope: "project"}
	}
	x := core.ProjectField{ID: d.Create.Field.ID, Name: d.Create.Field.Name}
	for _, o := range d.Create.Field.Options {
		x.Values = append(x.Values, o.Name)
	}
	if x.ID == "" {
		return core.ProjectField{}, safe(connect.ErrOffline)
	}
	return x, nil
}
func (a *Adapter) graphql(ctx context.Context, q string, vars any, out any, required string) (http.Header, error) {
	resp, b, e := a.request(ctx, "POST", graphqlPath, map[string]any{"query": q, "variables": vars})
	if e != nil {
		return nil, e
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlErr    `json:"errors"`
	}
	if json.Unmarshal(b, &env) != nil {
		return nil, safe(connect.ErrOffline)
	}
	if len(env.Errors) > 0 {
		known, ok := projectScope(resp.Header, required)
		if known && !ok {
			return resp.Header, connect.ErrScopeMissing{Scope: required}
		}
		return resp.Header, a.graphqlError(resp.Header, env.Errors, required)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return resp.Header, safe(core.ErrGraphQL)
	}
	if json.Unmarshal(env.Data, out) != nil {
		return resp.Header, safe(connect.ErrOffline)
	}
	return resp.Header, nil
}

type graphqlErr struct {
	Type string `json:"type"`
}

// graphqlError maps errors[].type by exact string; unknown or missing types become
// the generic core.ErrGraphQL, never ErrOffline. An absent scopes header alone never implies a missing scope.
// When entries disagree, scope, rate limit, forbidden and not found take precedence over validation.
func (a *Adapter) graphqlError(h http.Header, errs []graphqlErr, required string) error {
	var scope, rate, forbidden, notFound, validation bool
	for _, ge := range errs {
		switch ge.Type {
		case "INSUFFICIENT_SCOPES":
			scope = true
		case "RATE_LIMITED":
			rate = true
		case "FORBIDDEN":
			forbidden = true
		case "NOT_FOUND":
			notFound = true
		case "UNPROCESSABLE":
			validation = true
		}
	}
	switch {
	case scope:
		return connect.ErrScopeMissing{Scope: required}
	case rate:
		return rateLimited(retryAfter(h, a.clock()))
	case forbidden:
		return safe(core.ErrForbidden)
	case notFound:
		return safe(core.ErrNotFound)
	case validation:
		return safe(connect.ErrGitHubValidation)
	}
	return safe(core.ErrGraphQL)
}
func projectScope(h http.Header, required string) (bool, bool) {
	raw := h.Get("X-OAuth-Scopes")
	if raw == "" {
		return false, false
	}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "project" || (required == "read:project" && s == "read:project") {
			return true, true
		}
	}
	return true, false
}
