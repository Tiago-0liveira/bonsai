package garden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

type fakeTransport struct {
	mu         sync.Mutex
	fn         func(*http.Request) (*http.Response, error)
	requests   []*http.Request
	violations []string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Record before validation so a rejected forbidden request cannot disappear from the audit log.
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	f.mu.Unlock()
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if strings.Contains(r.URL.Path, "/issues") && !strings.HasSuffix(r.URL.Path, "/comments") {
			txt := string(raw)
			if strings.Contains(txt, `"body"`) || strings.Contains(txt, `"title"`) {
				f.mu.Lock()
				f.violations = append(f.violations, r.Method+" "+r.URL.Path+" issue body/title field")
				f.mu.Unlock()
				return nil, errors.New("forbidden issue body/title payload")
			}
		}
	}
	if strings.Contains(r.URL.Path, "/issues") && (r.Method == http.MethodPatch || r.Method == http.MethodPut) {
		f.mu.Lock()
		f.violations = append(f.violations, r.Method+" "+r.URL.Path+" issue replacement")
		f.mu.Unlock()
		return nil, errors.New("forbidden issue replacement method")
	}
	return f.fn(r)
}
func response(code int, body, link string) *http.Response {
	h := make(http.Header)
	if link != "" {
		h.Set("Link", link)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}
func fixture(t *testing.T, fn func(*http.Request) (*http.Response, error)) (*Adapter, *fakeTransport) {
	t.Helper()
	f := &fakeTransport{}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, v := range f.violations {
			t.Errorf("forbidden issue mutation request: %s", v)
		}
	})
	f.fn = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/repos/acme/repo" {
			return response(200, `{"id":9,"full_name":"acme/repo","node_id":"R_9","owner":{"login":"acme","node_id":"O_9"},"permissions":{"pull":true,"push":true}}`, ""), nil
		}
		return fn(r)
	}
	a, err := New(&http.Client{Transport: f}, "https://api.github.test")
	if err != nil {
		t.Fatal(err)
	}
	a.rememberRepository(repoJSON{ID: 9, FullName: "acme/repo", NodeID: "R_9", Owner: struct {
		Login  string `json:"login"`
		NodeID string `json:"node_id"`
	}{Login: "acme", NodeID: "O_9"}})
	a.rememberIssue(9, 1, 1)
	a.rememberIssue(9, 3, 3)
	return a, f
}
func TestPaginationAndPullRequestFiltering(t *testing.T) {
	calls := 0
	a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(200, `[{"id":11,"number":1,"node_id":"N1","body":"a","created_at":"2024-01-01T00:00:00Z","user":{"id":7},"labels":[{"name":"bonsai:managed"}]},{"id":12,"number":2,"pull_request":{},"user":{"id":8},"labels":[{"name":"bonsai:managed"}]},{"id":14,"number":4,"user":{"id":10},"labels":[{"name":"ordinary"}]}]`, `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`), nil
		}
		return response(200, `[{"id":13,"number":3,"user":{"id":9},"labels":[{"name":"bonsai:managed"}]}]`, ""), nil
	})
	p, e := a.Reader().ListManagedIssues(context.Background(), 9, "")
	if e != nil || p.Complete || len(p.Items) != 1 || p.Items[0].AuthorID != "7" || p.Items[0].CreatedAt.IsZero() {
		t.Fatalf("page=%+v err=%v", p, e)
	}
	p, e = a.Reader().ListManagedIssues(context.Background(), 9, p.NextCursor)
	if e != nil || !p.Complete || len(p.Items) != 1 {
		t.Fatalf("page=%+v err=%v", p, e)
	}
}
func TestRejectHostileCursorLinks(t *testing.T) {
	for _, link := range []string{`<https://evil.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`, `<https://api.github.test/users/9/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`, `<file:///etc/passwd>; rel="next"`} {
		t.Run(link, func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, `[]`, link), nil })
			_, e := a.Reader().ListManagedIssues(context.Background(), 9, "")
			if e == nil {
				t.Fatal("hostile link accepted")
			}
		})
	}
}
func TestStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		h      http.Header
		want   error
	}{{401, nil, connect.ErrAuthRequired}, {403, nil, core.ErrForbidden}, {403, http.Header{"Retry-After": []string{"30"}}, connect.ErrRateLimited}, {403, http.Header{"X-RateLimit-Remaining": []string{"0"}}, connect.ErrRateLimited}, {429, nil, connect.ErrRateLimited}, {404, nil, core.ErrNotFound}, {422, nil, connect.ErrGitHubValidation}, {500, nil, connect.ErrOffline}} {
		t.Run(fmt.Sprint(tc.status, tc.want), func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
				h := make(http.Header)
				for k, v := range tc.h {
					h.Set(k, strings.Join(v, ","))
				}
				return &http.Response{StatusCode: tc.status, Header: h, Body: io.NopCloser(strings.NewReader("secret-token"))}, nil
			})
			_, e := a.Reader().GetIssue(context.Background(), 9, 1)
			if e == nil || !strings.Contains(fmt.Sprintf("%v", e), tc.want.Error()) {
				t.Fatalf("error=%v want %v", e, tc.want)
			}
		})
	}
}
func Test304WithoutCachedRepresentationIsNotTreatedAsARead(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(304, "", ""), nil })
	_, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
	if !errors.Is(err, connect.ErrOffline) {
		t.Fatalf("raw 304 without ghcli cached body: %v", err)
	}
}
func TestNetworkFailureMapsOfflineWithoutLeakingTransportError(t *testing.T) {
	secret := "seeded-network-token-99"
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("https://user:%s@example.invalid: %s", secret, secret)
	})
	_, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
	for _, s := range []string{fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(s, secret) || strings.Contains(s, "example.invalid") {
			t.Fatalf("leaked: %s", s)
		}
	}
	if !errors.Is(err, connect.ErrOffline) {
		t.Fatalf("err=%v", err)
	}
}

func TestNoIssueBodyOrTitleMutation(t *testing.T) {
	a, f := fixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			return response(201, `{"id":3,"body":"comment","user":{"id":1,"login":"x"},"created_at":"2024-01-01T00:00:00Z"}`, ""), nil
		}
		return response(200, `[]`, ""), nil
	})
	_, _ = a.Writer().AppendComment(context.Background(), 9, 3, "note")
	_ = a.Writer().UpdateLabels(context.Background(), 9, 3, []string{"bonsai:managed"})
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if (r.Method == "PATCH" || r.Method == "PUT") && strings.Contains(r.URL.Path, "/issues/") {
			t.Fatalf("forbidden mutation: %s %s", r.Method, r.URL.Path)
		}
		if r.Method != "GET" {
			b, _ := io.ReadAll(r.Body)
			if strings.Contains(string(b), `"title"`) || strings.Contains(string(b), `"body"`) && strings.Contains(r.URL.Path, "/issues/") && !strings.Contains(r.URL.Path, "/comments") {
				t.Fatalf("forbidden issue mutation body: %s", b)
			}
		}
	}
}
func TestCancelledContextDoesNotContinuePagination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		n++
		cancel()
		return response(200, `[]`, `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`), nil
	})
	p, e := a.Reader().ListManagedIssues(ctx, 9, "")
	if e != nil || p.Complete || n != 1 {
		t.Fatalf("page=%+v err=%v calls=%d", p, e, n)
	}
}
func TestCommentTimestampDecoding(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		return response(200, `[{"id":4,"body":"x","user":{"id":7,"login":"alice"},"created_at":"2024-01-01T00:00:00Z"}]`, ""), nil
	})
	cs, e := a.Reader().ListComments(context.Background(), 9, 3)
	if e != nil || len(cs) != 1 || cs[0].AuthorID != "7" || cs[0].CreatedAt.IsZero() {
		t.Fatalf("%+v %v", cs, e)
	}
	_ = time.UTC
}

func TestProjectScopeAndPartialGraphQL(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		wantErr     bool
	}{{"missing", "", false}, {"odd", " projectx ", true}, {"present", "project, read:org", false}} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
				h := make(http.Header)
				if tc.scope != "" {
					h.Set("X-OAuth-Scopes", tc.scope)
				}
				body := `{"data":{"repositoryOwner":{"login":"acme","projectsV2":{"nodes":[]}}}}`
				if tc.scope == "project, read:org" {
					body = `{"data":{"repositoryOwner":{"login":"acme","projectsV2":{"nodes":[]}}},"errors":[{"message":"partial"}]}`
				}
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			ps, e := a.ListProjects(context.Background(), 9)
			if e == nil && tc.scope == "project, read:org" {
				t.Fatal("partial GraphQL data accepted")
			}
			if tc.wantErr {
				var scope connect.ErrScopeMissing
				if !errors.As(e, &scope) || scope.Scope != "read:project" {
					t.Fatalf("projects=%v err=%v", ps, e)
				}
			} else if tc.scope == "project, read:org" && !errors.Is(e, core.ErrGraphQL) {
				t.Fatalf("partial result error=%v", e)
			} else if tc.scope == "" && e != nil {
				t.Fatalf("absent scopes should remain unknown: %v", e)
			}
		})
	}
}
func TestErrorsNeverEchoResponseOrToken(t *testing.T) {
	secret := "garden-seeded-token-4e9f"
	for _, tc := range []struct {
		name    string
		code    int
		headers http.Header
	}{
		{"auth", 401, nil}, {"permission", 403, nil}, {"secondary-rate", 403, http.Header{"Retry-After": []string{"5"}}}, {"primary-rate", 403, http.Header{"X-Ratelimit-Remaining": []string{"0"}}}, {"rate-429", 429, nil}, {"deleted", 404, nil}, {"validation", 422, nil}, {"server", 500, nil}, {"not-modified", 304, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
				h := make(http.Header)
				for k, v := range tc.headers {
					h[k] = v
				}
				return &http.Response{StatusCode: tc.code, Header: h, Body: io.NopCloser(strings.NewReader(secret + " Authorization: Bearer " + secret + " https://user:" + secret + "@example.invalid/private"))}, nil
			})
			_, err := a.Reader().GetIssue(context.Background(), 9, 1)
			for _, rendered := range []string{fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				if strings.Contains(rendered, secret) || strings.Contains(rendered, "example.invalid") {
					t.Fatalf("unsafe error rendering: %s", rendered)
				}
			}
		})
	}
}

func TestLabelCreate422ReobservesBeforeDeciding(t *testing.T) {
	calls := 0
	a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			return response(422, "duplicate", ""), nil
		}
		calls++
		return response(200, `[{"name":"bonsai:managed","color":"00aa00","description":"managed"}]`, ""), nil
	})
	err := a.CreateLabel(context.Background(), 9, connect.Label{Name: "bonsai:managed", Color: "00aa00", Description: "managed"})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v observation calls=%d", err, calls)
	}
}

func TestCommentPayloadLimit(t *testing.T) {
	a, f := fixture(t, func(*http.Request) (*http.Response, error) { return response(201, `{"id":1}`, ""), nil })
	_, err := a.Writer().AppendComment(context.Background(), 9, 1, strings.Repeat("x", maxComment+1))
	if err == nil {
		t.Fatal("oversized comment accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 0 {
		t.Fatalf("oversized body reached transport: %d requests", len(f.requests))
	}
}

func TestRepositoryByIDUsesCallerNameAndVerifiesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, full string
		returned   int64
		wantErr    error
	}{{"renamed same ID", "new-owner/repo", 9, nil}, {"reused name different ID", "new-owner/repo", 90, connect.ErrRepositoryIdentityChanged}} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			a, f := fixture(t, func(r *http.Request) (*http.Response, error) {
				path = r.URL.Path
				return response(200, fmt.Sprintf(`{"id":%d,"full_name":%q,"node_id":"NODE","owner":{"login":"new-owner","node_id":"OWNER"},"permissions":{"pull":true}}`, tc.returned, tc.full), ""), nil
			})
			got, err := (RepositoryResolver{Adapter: a}).ResolveRepositoryID(context.Background(), 9, tc.full)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("repo=%+v err=%v", got, err)
				}
			} else if err != nil || got.ID != 9 || got.FullName != tc.full {
				t.Fatalf("repo=%+v err=%v", got, err)
			}
			if path != "/repos/new-owner/repo" {
				t.Fatalf("wrong lookup path %q", path)
			}
			for _, req := range f.requests {
				if strings.HasPrefix(req.URL.Path, "/repositories/") {
					t.Fatalf("unsupported repository-by-id endpoint used: %s", req.URL.Path)
				}
			}
		})
	}
}

func TestConnectApplyResumeAndSyncLoadEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var labelExists, projectExists, fieldExists bool
	var projectDescription string
	const issueBody = "<!-- bonsai:garden-metadata/v1 {\"schema_version\":1,\"provenance\":\"test\",\"owner\":\"alice\",\"accepted\":true,\"accepted_spec_hash\":\"HASH\",\"spec_fingerprint\":\"HASH\"} -->\nsummary"
	spec := "A verified feature"
	hash := core.ContentHash(spec)
	body := strings.ReplaceAll(issueBody, "HASH", hash)
	meta, err := core.EncodeMetadata(core.Metadata{SchemaVersion: 1, Provenance: "test", Owner: "alice", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash})
	if err != nil {
		t.Fatal(err)
	}
	body = "<!-- bonsai:garden-metadata/v1 " + string(meta) + " -->\nsummary"
	revision, err := core.EncodeCommentEnvelope(core.CommentEnvelope{Kind: core.CommentRevision, RepoID: 9, IssueID: 1001, RevisionKind: core.RevisionSpec, ContentHash: hash}, spec)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := core.EncodeCommentEnvelope(core.CommentEnvelope{Kind: core.CommentApproval, RepoID: 9, IssueID: 1001, RevisionHash: hash, RevisionCommentID: 2001, SourceSpecFingerprint: hash}, "approved")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTransport{}
	t.Cleanup(func() {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, v := range fake.violations {
			t.Errorf("forbidden issue mutation request: %s", v)
		}
	})
	fake.fn = func(r *http.Request) (*http.Response, error) {
		// org-owned: the viewer (alice) is the Project creator, not the owner.
		if r.URL.Path == "/repos/acme/repo" {
			return response(200, `{"id":9,"full_name":"acme/repo","node_id":"R_9","owner":{"login":"acme","node_id":"O_9"},"permissions":{"push":true}}`, ""), nil
		}
		if r.URL.Path == "/repos/acme/repo/labels" {
			mu.Lock()
			defer mu.Unlock()
			if r.Method == "POST" {
				labelExists = true
				return response(201, `{"name":"bonsai:managed","color":"00aa00","description":"garden"}`, ""), nil
			}
			if labelExists {
				return response(200, `[{"name":"bonsai:managed","color":"00aa00","description":"garden"}]`, ""), nil
			}
			return response(200, `[]`, ""), nil
		}
		if r.URL.Path == "/repos/acme/repo/issues" {
			return response(200, `[{"id":1001,"number":7,"node_id":"I_1001","body":`+mustJSON(t, body)+`,"created_at":"2024-01-01T00:00:00Z","user":{"id":4},"labels":[{"name":"bonsai:managed"},{"name":"bonsai:type/feature"},{"name":"bonsai:stage/ready"}]}]`, ""), nil
		}
		if r.URL.Path == "/repos/acme/repo/issues/7/comments" {
			return response(200, `[{"id":2001,"body":`+mustJSON(t, revision)+`,"user":{"id":4,"login":"alice"},"created_at":"2024-01-01T00:00:00Z"},{"id":2002,"body":`+mustJSON(t, approval)+`,"user":{"id":5,"login":"bob"},"created_at":"2024-01-02T00:00:00Z"}]`, ""), nil
		}
		if r.URL.Path == "/graphql" {
			b, _ := io.ReadAll(r.Body)
			var req struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.Unmarshal(b, &req)
			mu.Lock()
			defer mu.Unlock()
			h := make(http.Header)
			h.Set("X-OAuth-Scopes", "project, read:project")
			if strings.Contains(req.Query, "GardenProjects") {
				if !projectExists {
					return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{"data":{"repositoryOwner":{"login":"acme","projectsV2":{"nodes":[]}}}}`))}, nil
				}
				p := map[string]any{"id": "P_1", "title": "Garden", "number": 1, "shortDescription": projectDescription, "creator": map[string]string{"login": "alice"}, "fields": map[string]any{"nodes": []any{}}}
				if fieldExists {
					p["fields"] = map[string]any{"nodes": []any{map[string]string{"id": "F_1", "name": "Status"}}}
				}
				bb, _ := json.Marshal(map[string]any{"data": map[string]any{"repositoryOwner": map[string]any{"login": "acme", "projectsV2": map[string]any{"nodes": []any{p}}}}})
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(bb))}, nil
			}
			if strings.Contains(req.Query, "GardenCreateProject") {
				projectExists = true
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{"data":{"createProjectV2":{"projectV2":{"id":"P_1","title":"Garden","number":1,"creator":{"login":"alice"}}}}}`))}, nil
			}
			if strings.Contains(req.Query, "GardenMarkProject") {
				projectDescription, _ = req.Variables["description"].(string)
				bb, _ := json.Marshal(map[string]any{"data": map[string]any{"updateProjectV2": map[string]any{"projectV2": map[string]string{"id": "P_1", "shortDescription": projectDescription}}}})
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(bb))}, nil
			}
			if strings.Contains(req.Query, "GardenCreateField") {
				fieldExists = true
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{"data":{"createProjectV2Field":{"projectV2Field":{"id":"F_1","name":"Status"}}}}`))}, nil
			}
		}
		return response(404, `{}`, ""), nil
	}
	a, err := New(&http.Client{Transport: fake}, "https://api.github.test")
	if err != nil {
		t.Fatal(err)
	}
	remotes := staticRemotes{{Name: "origin", URL: "https://github.com/acme/repo"}}
	resolver := RepositoryResolver{Adapter: a}
	target, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if target.GardenID.Repo != 9 {
		t.Fatalf("resolved target: %+v", target)
	}
	plan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{Labels: []connect.Label{{Name: "bonsai:managed", Color: "00aa00", Description: "garden"}}, ProjectName: "Garden", Fields: []core.ProjectField{{Name: "Status"}}})
	if err != nil {
		t.Fatal(err)
	}
	local, _ := core.NewLocalRepoID("local-e2e")
	store := &connect.Store{Path: filepath.Join(t.TempDir(), "garden.json")}
	ports := connect.SetupPorts{Labels: a, LabelWrites: a, Projects: a, ProjectWrites: a, OperationProjects: a, ViewerLogin: "alice"}
	_, err = connect.Apply(context.Background(), store, local, 9, connect.AccessWrite, "operation-e2e", plan, ports)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err = connect.Resume(context.Background(), store, "operation-e2e", 9, ports, local, connect.AccessWrite); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	id, _ := core.NewGardenID(9)
	snapshot := gardensync.Load(context.Background(), gardensync.Request{GardenID: id, LocalRepoID: local, Issues: a.Reader(), ApprovalPolicy: core.ApprovalPolicy{AllowedActorIDs: []string{"5"}}, Limits: gardensync.Limits{PageCap: 2, ItemCap: 10, CallTimeout: time.Second}})
	if !snapshot.Complete || len(snapshot.Items) != 1 || snapshot.Items[0].Lane.Lane != core.LaneWork {
		t.Fatalf("sync snapshot=%+v", snapshot)
	}
}

type staticRemotes []connect.Remote

func (s staticRemotes) ListRemotes(context.Context) ([]connect.Remote, error) {
	return []connect.Remote(s), nil
}
func mustJSON(t *testing.T, v string) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestRepositoryPermissionsNeverFabricateWrite(t *testing.T) {
	for name, perms := range map[string]string{"all false": `{"admin":false,"maintain":false,"push":false,"triage":false,"pull":false}`, "absent": ``} {
		t.Run(name, func(t *testing.T) {
			body := `{"id":9,"full_name":"o/r","owner":{"login":"o","node_id":"O"}`
			if perms != "" {
				body += `,"permissions":` + perms
			}
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, body+`}`, ""), nil })
			got, err := (RepositoryResolver{Adapter: a}).ResolveRepositoryID(context.Background(), 9, "o/r")
			if err != nil || got.Access != connect.AccessRead {
				t.Fatalf("access=%v err=%v", got.Access, err)
			}
		})
	}
}

func TestInsufficientScopesGraphQLErrorIsScopeMissingWithHeader(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		r := response(200, `{"errors":[{"type":"INSUFFICIENT_SCOPES","message":"x"}]}`, "")
		r.Header.Set("X-OAuth-Scopes", "repo")
		return r, nil
	})
	_, err := a.ListProjects(context.Background(), 9)
	var miss connect.ErrScopeMissing
	if !errors.As(err, &miss) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnsureFieldMatchesGitHubCreateFieldSchema(t *testing.T) {
	for _, field := range []core.ProjectField{
		{Name: "Stage", Values: []string{"draft", "ready", "in-progress", "review", "blocked", "done"}},
		{Name: core.ProjectTypeField, Values: []string{"feature", "bug", "chore", "research"}},
		{Name: "Type", Values: []string{"feature", "bug", "chore", "research"}},
		{Name: "Notes"},
	} {
		t.Run(field.Name, func(t *testing.T) {
			wantField := core.CanonicalSetupField(field)
			a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
				var req struct {
					Query     string `json:"query"`
					Variables struct {
						Project string                                      `json:"project"`
						Name    string                                      `json:"name"`
						Type    string                                      `json:"type"`
						Options []struct{ Name, Color, Description string } `json:"options"`
					} `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if req.Variables.Name == "Type" {
					return response(200, `{"data":{"createProjectV2Field":null},"errors":[{"type":"UNPROCESSABLE","message":"Name cannot have a reserved value, Name has already been taken"}]}`, ""), nil
				}
				// CreateProjectV2FieldInput.dataType uses the custom-field enum,
				// not the output-field enum. GitHub validates this before execution.
				if !strings.Contains(req.Query, "$type:ProjectV2CustomFieldType!") {
					t.Error("field mutation does not declare GitHub's custom-field enum")
					return response(200, `{"errors":[{"message":"unknown type"}]}`, ""), nil
				}
				wantType := "TEXT"
				if len(field.Values) > 0 {
					wantType = "SINGLE_SELECT"
				}
				if req.Variables.Project != "P_1" || req.Variables.Name != wantField.Name || req.Variables.Type != wantType || len(req.Variables.Options) != len(field.Values) {
					t.Fatalf("unexpected field input: %+v", req.Variables)
				}
				options := []map[string]string{}
				for i, opt := range req.Variables.Options {
					if opt.Name != field.Values[i] || opt.Color != "GRAY" || opt.Description != "" {
						t.Fatalf("option input = %+v", opt)
					}
					options = append(options, map[string]string{"name": opt.Name})
				}
				body, err := json.Marshal(map[string]any{"data": map[string]any{"createProjectV2Field": map[string]any{"projectV2Field": map[string]any{"id": "F_1", "name": wantField.Name, "options": options}}}})
				if err != nil {
					t.Fatal(err)
				}
				return response(200, string(body), ""), nil
			})
			got, err := a.EnsureField(context.Background(), "P_1", field)
			if err != nil || got.ID != "F_1" || got.Name != wantField.Name || strings.Join(got.Values, ",") != strings.Join(field.Values, ",") {
				t.Fatalf("EnsureField = %+v, %v", got, err)
			}
		})
	}
}

func TestViewerReadsNumericIdentity(t *testing.T) {
	a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/user" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		return response(200, `{"id":42,"login":"octo-dev","email":"private@example.com"}`, ""), nil
	})
	got, err := a.Viewer(context.Background())
	if err != nil || got != (connect.GitHubIdentity{ID: "42", Login: "octo-dev"}) {
		t.Fatalf("Viewer = %+v, %v", got, err)
	}
}

func TestViewerFailsClosedOnUnusableIdentity(t *testing.T) {
	for name, body := range map[string]string{"no id": `{"login":"x"}`, "no login": `{"id":1}`, "garbage": `nope`} {
		a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, body, ""), nil })
		if _, err := a.Viewer(context.Background()); !errors.Is(err, connect.ErrOffline) {
			t.Fatalf("%s: err = %v, want ErrOffline", name, err)
		}
	}
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(401, `{}`, ""), nil })
	if _, err := a.Viewer(context.Background()); !errors.Is(err, connect.ErrAuthRequired) {
		t.Fatalf("401 err = %v, want ErrAuthRequired", err)
	}
}
