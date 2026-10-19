// Package garden adapts the Garden ports to GitHub through the ghcli HTTP client.
package garden

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
const graphqlPath = "/graphql"
const metadataPrefix = "<!-- bonsai:garden-metadata/v1 "

// Adapter uses the authenticated ghcli client. Tests inject an HTTP client backed by a fake RoundTripper.
type Adapter struct {
	HTTP         *http.Client
	BaseURL      string
	mu           sync.RWMutex
	repositories map[core.RepoID]repoJSON
	issueNumbers map[core.RepoID]map[core.IssueID]int64
}

// IssueReader and IssueWriter expose separate capabilities over the same authenticated transport.
type IssueReader struct{ *Adapter }
type IssueWriter struct{ *Adapter }

func (a *Adapter) Reader() *IssueReader { return &IssueReader{Adapter: a} }
func (a *Adapter) Writer() *IssueWriter { return &IssueWriter{Adapter: a} }

func New(client *http.Client, baseURL string) *Adapter {
	return &Adapter{HTTP: client, BaseURL: strings.TrimRight(baseURL, "/")}
}

// FromService reuses ghcli's authenticated, bounded transport and conditional read cache.
func FromService(service *ghcli.Service) *Adapter {
	if service == nil || service.Client == nil || service.Client.HTTP == nil || service.Client.BaseURL == "" {
		return nil
	}
	return New(service.Client.HTTP, service.Client.BaseURL)
}

func (a *Adapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}
func (a *Adapter) base() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return "https://api.github.com"
}
func (a *Adapter) endpoint(path string) string {
	if path == graphqlPath && strings.HasSuffix(a.base(), "/api/v3") {
		return strings.TrimSuffix(a.base(), "/v3") + "/graphql"
	}
	return a.base() + path
}
func repoNamePath(fullName string) (string, error) {
	p := strings.Split(fullName, "/")
	if len(p) != 2 || p[0] == "" || p[1] == "" {
		return "", core.ErrInvalidID
	}
	return "/repos/" + url.PathEscape(p[0]) + "/" + url.PathEscape(p[1]), nil
}
func (a *Adapter) rememberRepository(r repoJSON) {
	if r.ID <= 0 || r.FullName == "" {
		return
	}
	a.mu.Lock()
	if a.repositories == nil {
		a.repositories = map[core.RepoID]repoJSON{}
	}
	a.repositories[core.RepoID(r.ID)] = r
	a.mu.Unlock()
}
func (a *Adapter) repositoryPath(ctx context.Context, id core.RepoID) (string, error) {
	a.mu.RLock()
	r, ok := a.repositories[id]
	a.mu.RUnlock()
	if !ok {
		return "", connect.ErrNoGitHubRemote
	}
	path, err := repoNamePath(r.FullName)
	if err != nil {
		return "", err
	}
	resp, body, err := a.request(ctx, "GET", path, nil)
	if err != nil {
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
	for i := 0; i < maxPages; i++ {
		resp, b, e := a.request(ctx, "GET", path, nil)
		if e != nil {
			return 0, e
		}
		var rows []issueJSON
		if json.Unmarshal(b, &rows) != nil {
			return 0, safe(connect.ErrOffline)
		}
		for _, v := range rows {
			if v.ID > 0 && v.Number > 0 {
				a.rememberIssue(repo, core.IssueID(v.ID), v.Number)
				if core.IssueID(v.ID) == id {
					return v.Number, nil
				}
			}
		}
		next, e := nextLinkPath(resp.Header.Get("Link"), a.base(), base+"/issues")
		if e != nil {
			return 0, core.ErrInvalidCursor
		}
		if next == "" {
			return 0, core.ErrNotFound
		}
		u, _ := url.Parse(next)
		path = u.RequestURI()
	}
	return 0, core.ErrTooManyComments
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
	resp, e := a.client().Do(req)
	if e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, nil, ctx.Err()
		}
		if gitdomain.Code(e) == "github_auth" || errors.Is(e, gitdomain.ErrAuth) {
			return nil, nil, safe(connect.ErrAuthRequired)
		}
		return nil, nil, safe(connect.ErrOffline)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	resp.Body.Close()
	if e != nil || len(b) > maxResponse {
		return resp, nil, safe(connect.ErrOffline)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, b, rate(resp)
	}
	return resp, b, nil
}
func safe(e error) error { return fmt.Errorf("Garden GitHub request failed: %w", e) }
func statusError(r *http.Response) error {
	switch r.StatusCode {
	case 401:
		return safe(connect.ErrAuthRequired)
	case 404:
		return safe(core.ErrNotFound)
	case 403, 405:
		return safe(connect.ErrPermissionDenied)
	case 422:
		return safe(connect.ErrGitHubValidation)
	case 429:
		return safe(connect.ErrRateLimited)
	}
	if r.StatusCode == 403 && (r.Header.Get("Retry-After") != "" || r.Header.Get("X-RateLimit-Remaining") == "0") {
		return safe(connect.ErrRateLimited)
	}
	return safe(connect.ErrOffline)
}
func rate(r *http.Response) error {
	if r.StatusCode == 403 && (r.Header.Get("Retry-After") != "" || r.Header.Get("X-RateLimit-Remaining") == "0") {
		return safe(connect.ErrRateLimited)
	}
	return statusError(r)
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
	owner, name, e := remoteRepo("https://github.com/" + fullName)
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
	if core.RepoID(x.ID) != id {
		return connect.Repository{}, connect.ErrRepositoryIdentityChanged
	}
	r.Adapter.rememberRepository(x)
	return connect.Repository{ID: core.RepoID(x.ID), FullName: x.FullName, Fork: x.Fork, Access: permission(x.Permissions), ProjectScope: scopePresent(x.Scope), ProjectScopeKnown: x.Scope != ""}, nil
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
	if p["pull"] || p["triage"] {
		return connect.AccessRead
	}
	return connect.AccessNone
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
	p := strings.Split(strings.Trim(s, "/"), "/")
	if len(p) != 2 || p[0] == "" || p[1] == "" {
		return "", "", errors.New("invalid")
	}
	return url.PathEscape(p[0]), url.PathEscape(p[1]), nil
}

// ListManagedIssues reads a bounded page and only emits a cursor from a validated next Link.
func (a *IssueReader) ListManagedIssues(ctx context.Context, repo core.RepoID, cursor string) (core.IssuePage, error) {
	if repo <= 0 {
		return core.IssuePage{}, core.ErrInvalidID
	}
	basePath, err := a.repositoryPath(ctx, repo)
	if err != nil {
		return core.IssuePage{}, err
	}
	path := basePath + "/issues?state=all&per_page=100&labels=bonsai%3Amanaged"
	if cursor != "" {
		path, err = decodeCursor(cursor, a.base(), basePath+"/issues")
		if err != nil {
			return core.IssuePage{}, core.ErrInvalidCursor
		}
	}
	resp, b, e := a.request(ctx, "GET", path, nil)
	if e != nil {
		return core.IssuePage{}, e
	}
	var rows []issueJSON
	if json.Unmarshal(b, &rows) != nil {
		return core.IssuePage{}, safe(connect.ErrOffline)
	}
	out := core.IssuePage{Complete: true}
	for _, v := range rows {
		if v.PullRequest != nil {
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
		if v.Number <= 0 || v.ID <= 0 || v.User.ID <= 0 {
			return core.IssuePage{}, safe(connect.ErrOffline)
		}
		a.rememberIssue(repo, core.IssueID(v.ID), v.Number)
		labels := make([]string, 0, len(v.Labels))
		for _, l := range v.Labels {
			labels = append(labels, l.Name)
		}
		out.Items = append(out.Items, core.Issue{Repo: repo, ID: core.IssueID(v.ID), NodeID: v.NodeID, AuthorID: strconv.FormatInt(v.User.ID, 10), CreatedAt: v.CreatedAt, Labels: labels, Body: v.Body, Metadata: issueMetadata(v.Body)})
	}
	next, e := nextLinkPath(resp.Header.Get("Link"), a.base(), basePath+"/issues")
	if e != nil {
		return core.IssuePage{}, core.ErrInvalidCursor
	}
	if next != "" {
		if len(cursor) > 0 && next == path {
			return core.IssuePage{}, core.ErrInvalidCursor
		}
		out.NextCursor = encodeCursor(next)
		out.Complete = false
	}
	return out, nil
}

type issueJSON struct {
	ID        int64     `json:"id"`
	Number    int64     `json:"number"`
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

func (a *IssueReader) GetIssue(ctx context.Context, r core.RepoID, id core.IssueID) (core.Issue, error) {
	if id <= 0 {
		return core.Issue{}, core.ErrInvalidID
	}
	number, e := a.issueNumber(ctx, r, id)
	if e != nil {
		return core.Issue{}, e
	}
	base, e := a.repositoryPath(ctx, r)
	if e != nil {
		return core.Issue{}, e
	}
	resp, b, e := a.request(ctx, "GET", base+"/issues/"+strconv.FormatInt(number, 10), nil)
	_ = resp
	if e != nil {
		return core.Issue{}, e
	}
	var v issueJSON
	if json.Unmarshal(b, &v) != nil || v.ID <= 0 {
		return core.Issue{}, safe(connect.ErrOffline)
	}
	x := core.Issue{Repo: r, ID: core.IssueID(v.ID), NodeID: v.NodeID, AuthorID: strconv.FormatInt(v.User.ID, 10), CreatedAt: v.CreatedAt, Body: v.Body, Metadata: issueMetadata(v.Body)}
	a.rememberIssue(r, core.IssueID(v.ID), v.Number)
	for _, l := range v.Labels {
		x.Labels = append(x.Labels, l.Name)
	}
	return x, nil
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
func (a *IssueReader) ListComments(ctx context.Context, r core.RepoID, id core.IssueID) ([]core.IssueComment, error) {
	var all []core.IssueComment
	number, e := a.issueNumber(ctx, r, id)
	if e != nil {
		return nil, e
	}
	base, e := a.repositoryPath(ctx, r)
	if e != nil {
		return nil, e
	}
	path := base + "/issues/" + strconv.FormatInt(number, 10) + "/comments?per_page=100"
	for i := 0; i < maxPages; i++ {
		resp, b, e := a.request(ctx, "GET", path, nil)
		if e != nil {
			return nil, e
		}
		var rows []commentJSON
		if json.Unmarshal(b, &rows) != nil {
			return nil, safe(connect.ErrOffline)
		}
		for _, v := range rows {
			all = append(all, core.IssueComment{ID: v.ID, Author: v.User.Login, AuthorID: strconv.FormatInt(v.User.ID, 10), Body: v.Body, CreatedAt: v.CreatedAt})
		}
		next, e := nextLinkPath(resp.Header.Get("Link"), a.base(), base+"/issues/"+strconv.FormatInt(number, 10)+"/comments")
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

func (a *IssueWriter) AppendComment(ctx context.Context, r core.RepoID, id core.IssueID, body string) (core.IssueComment, error) {
	if len(body) > maxComment {
		return core.IssueComment{}, errors.New("Garden comment exceeds payload limit")
	}
	number, e := a.issueNumber(ctx, r, id)
	if e != nil {
		return core.IssueComment{}, e
	}
	base, e := a.repositoryPath(ctx, r)
	if e != nil {
		return core.IssueComment{}, e
	}
	resp, b, e := a.request(ctx, "POST", base+"/issues/"+strconv.FormatInt(number, 10)+"/comments", map[string]string{"body": body})
	_ = resp
	if e != nil {
		return core.IssueComment{}, e
	}
	var v commentJSON
	if json.Unmarshal(b, &v) != nil || v.ID <= 0 {
		return core.IssueComment{}, safe(connect.ErrOffline)
	}
	return core.IssueComment{ID: v.ID, Author: v.User.Login, AuthorID: strconv.FormatInt(v.User.ID, 10), Body: v.Body, CreatedAt: v.CreatedAt}, nil
}
func (a *IssueWriter) UpdateLabels(ctx context.Context, r core.RepoID, id core.IssueID, labels []string) error {
	desired := map[string]bool{}
	for _, label := range labels {
		if !strings.HasPrefix(strings.ToLower(label), "bonsai:") {
			return core.ErrInvalidMetadata
		}
		desired[label] = true
	}
	current, err := (&IssueReader{Adapter: a.Adapter}).GetIssue(ctx, r, id)
	if err != nil {
		return err
	}
	number, err := a.issueNumber(ctx, r, id)
	if err != nil {
		return err
	}
	base, err := a.repositoryPath(ctx, r)
	if err != nil {
		return err
	}
	for _, label := range current.Labels {
		if strings.HasPrefix(strings.ToLower(label), "bonsai:") && !desired[label] {
			_, _, err = a.request(ctx, "DELETE", base+"/issues/"+strconv.FormatInt(number, 10)+"/labels/"+url.PathEscape(label), nil)
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
			_, _, err = a.request(ctx, "POST", base+"/issues/"+strconv.FormatInt(number, 10)+"/labels", map[string][]string{"labels": {label}})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeCursor(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func decodeCursor(c, base, expectedPath string) (string, error) {
	b, e := base64.RawURLEncoding.DecodeString(c)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(string(b))
	if e != nil {
		return "", e
	}
	page, pe := strconv.Atoi(u.Query().Get("page"))
	if u.Scheme != "https" || !strings.EqualFold(u.Host, mustURL(base).Host) || u.User != nil || u.Path != expectedPath || !validIssueQuery(u.Query(), page) || pe != nil || page < 2 || page > maxPages {
		return "", errors.New("invalid")
	}
	return u.RequestURI(), nil
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
func nextLinkPath(h, base, expectedPath string) (string, error) {
	for _, part := range strings.Split(h, ",") {
		bits := strings.Split(part, ";")
		if len(bits) < 2 || !strings.Contains(bits[1], `rel="next"`) {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(bits[0]), "<>")
		u, e := url.Parse(raw)
		if e != nil {
			return "", e
		}
		b := mustURL(base)
		if !u.IsAbs() {
			u = b.ResolveReference(u)
		}
		page, pe := strconv.Atoi(u.Query().Get("page"))
		if u.Scheme != "https" || !strings.EqualFold(u.Host, b.Host) || u.User != nil || u.Path != expectedPath || u.RawQuery == "" || pe != nil || page < 2 || page > maxPages || (strings.HasSuffix(expectedPath, "/issues") && !validListQuery(u.Query(), page)) {
			return "", errors.New("hostile link")
		}
		return u.String(), nil
	}
	return "", nil
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
	for i := 0; i < maxPages; i++ {
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
		next, e := nextLinkPath(resp.Header.Get("Link"), a.base(), base+"/labels")
		if e != nil {
			return nil, core.ErrInvalidCursor
		}
		if next == "" {
			return out, nil
		}
		nextURL, _ := url.Parse(next)
		path = nextURL.RequestURI()
	}
	return nil, core.ErrTooManyComments
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

// ListProjects observes the viewer's Projects V2 with fields and creator identity.
func (a *Adapter) ListProjects(ctx context.Context, _ core.RepoID) ([]core.Project, error) {
	const q = `query GardenProjects { viewer { projectsV2(first:100) { nodes { id title number shortDescription creator { login } fields(first:100) { nodes { ... on ProjectV2Field { id name } ... on ProjectV2SingleSelectField { id name options { id name } } } } } } } }`
	var d struct {
		Viewer struct {
			Projects struct {
				Nodes []struct {
					ID, Title, ShortDescription string
					Number                      int
					Creator                     *struct {
						Login string `json:"login"`
					} `json:"creator"`
					Fields struct {
						Nodes []struct {
							ID, Name string
							Options  []struct{ Name string } `json:"options"`
						} `json:"nodes"`
					} `json:"fields"`
				} `json:"nodes"`
			} `json:"projectsV2"`
		} `json:"viewer"`
	}
	h, e := a.graphql(ctx, q, map[string]any{}, &d, "read:project")
	if e != nil {
		return nil, e
	}
	if known, ok := projectScope(h, "read:project"); known && !ok {
		return nil, connect.ErrScopeMissing{Scope: "read:project"}
	}
	out := []core.Project{}
	for _, p := range d.Viewer.Projects.Nodes {
		x := core.Project{ID: p.ID, Name: p.Title, Number: strconv.Itoa(p.Number), Description: p.ShortDescription}
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
	return out, nil
}

// EnsureProject creates a Project V2. Existing projects are only observed by ListProjects.
func (a *Adapter) EnsureProject(ctx context.Context, r core.RepoID, name string) (core.Project, error) {
	return a.ensureProject(ctx, r, name, "")
}

// EnsureProjectWithMarker creates a Project carrying the local operation marker in its description.
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
	base, ownerErr := a.repositoryPath(ctx, r)
	if ownerErr != nil {
		return core.Project{}, ownerErr
	}
	_ = base
	a.mu.RLock()
	ownerData := a.repositories[r]
	a.mu.RUnlock()
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
	out := core.Project{ID: p.ID, Name: p.Title, Number: strconv.Itoa(p.Number), Description: p.ShortDescription}
	if p.Creator != nil {
		out.CreatorLogin = p.Creator.Login
	}
	return out, nil
}

const createProjectQuery = `mutation GardenCreateProject($ownerId:ID!,$title:String!,$repositoryId:ID!){createProjectV2(input:{ownerId:$ownerId,title:$title,repositoryId:$repositoryId}){projectV2{id title number shortDescription creator{login}}}}`

// EnsureField creates only a missing single-select field; it does not change existing fields.
func (a *Adapter) EnsureField(ctx context.Context, projectID string, w core.ProjectField) (core.ProjectField, error) {
	const q = `mutation GardenCreateField($project:ID!,$name:String!,$type:ProjectV2FieldDataType!,$options:[ProjectV2SingleSelectFieldOptionInput!]){createProjectV2Field(input:{projectId:$project,name:$name,dataType:$type,singleSelectOptions:$options}){projectV2Field{... on ProjectV2Field{id name} ... on ProjectV2SingleSelectField{id name options{name}}}}}`
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
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(b, &env) != nil {
		return nil, safe(connect.ErrOffline)
	}
	if len(env.Errors) > 0 {
		known, ok := projectScope(resp.Header, required)
		if known && !ok {
			return resp.Header, connect.ErrScopeMissing{Scope: required}
		}
		return resp.Header, safe(connect.ErrOffline)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return resp.Header, safe(connect.ErrOffline)
	}
	if json.Unmarshal(env.Data, out) != nil {
		return resp.Header, safe(connect.ErrOffline)
	}
	return resp.Header, nil
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
