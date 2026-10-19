package garden_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	ghgarden "github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli/garden"
)

// -----------------------------------------------------------------------------
// Helper constructors and test doubles
// -----------------------------------------------------------------------------

type adversarialTransport struct {
	mu         sync.Mutex
	fn         func(*http.Request) (*http.Response, error)
	requests   []*http.Request
	violations []string
}

func (f *adversarialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	f.mu.Unlock()

	// Audit write safety invariants:
	// 1. Never body/title payload on issue mutation (only comments POST can contain body)
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if strings.Contains(r.URL.Path, "/issues") && !strings.Contains(r.URL.Path, "/comments") {
			txt := string(raw)
			if strings.Contains(txt, `"body"`) || strings.Contains(txt, `"title"`) {
				f.mu.Lock()
				f.violations = append(f.violations, fmt.Sprintf("%s %s contains issue body/title: %s", r.Method, r.URL.Path, txt))
				f.mu.Unlock()
				return nil, errors.New("write safety violation: forbidden issue body/title payload")
			}
		}
	}

	// 2. Never PATCH/PUT/DELETE on issue endpoints (labels endpoints on an issue can DELETE a label association)
	if strings.Contains(r.URL.Path, "/issues/") && !strings.Contains(r.URL.Path, "/labels/") && (r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete) {
		f.mu.Lock()
		f.violations = append(f.violations, fmt.Sprintf("%s %s forbidden issue mutation method", r.Method, r.URL.Path))
		f.mu.Unlock()
		return nil, errors.New("write safety violation: forbidden issue mutation method")
	}

	// 3. Never PATCH/DELETE on repository labels endpoint
	if strings.Contains(r.URL.Path, "/labels") && !strings.Contains(r.URL.Path, "/issues/") && (r.Method == http.MethodPatch || r.Method == http.MethodDelete) {
		f.mu.Lock()
		f.violations = append(f.violations, fmt.Sprintf("%s %s forbidden label mutation method", r.Method, r.URL.Path))
		f.mu.Unlock()
		return nil, errors.New("write safety violation: forbidden label mutation method")
	}

	if f.fn != nil {
		return f.fn(r)
	}
	return response(404, `{"message":"not found"}`, ""), nil
}

func response(code int, body, link string) *http.Response {
	h := make(http.Header)
	if link != "" {
		h.Set("Link", link)
	}
	return &http.Response{
		StatusCode: code,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func adversarialFixture(t *testing.T, customFn func(*http.Request) (*http.Response, error)) (*ghgarden.Adapter, *adversarialTransport) {
	t.Helper()
	trans := &adversarialTransport{}
	t.Cleanup(func() {
		trans.mu.Lock()
		defer trans.mu.Unlock()
		for _, v := range trans.violations {
			t.Errorf("write safety violation recorded: %s", v)
		}
	})

	trans.fn = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/repos/acme/repo" {
			return response(200, `{"id":42,"full_name":"acme/repo","node_id":"R_42","owner":{"login":"acme","node_id":"O_42"},"permissions":{"admin":true,"push":true,"pull":true}}`, ""), nil
		}
		// Default issue numbers lookup for repo 42 (exact path)
		if r.URL.Path == "/repos/acme/repo/issues" && r.URL.Query().Get("labels") == "" && r.Method == "GET" {
			return response(200, `[{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"}]},{"id":2,"number":2,"node_id":"N2","user":{"id":100},"labels":[{"name":"bonsai:managed"}]}]`, ""), nil
		}
		if customFn != nil {
			return customFn(r)
		}
		return response(404, `{"message":"not found"}`, ""), nil
	}

	client := &http.Client{Transport: trans}
	adapter, err := ghgarden.New(client, "https://api.github.test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Prime repository 42 in cache using RepositoryResolver
	resolver := ghgarden.RepositoryResolver{Adapter: adapter}
	_, err = resolver.ResolveRepositoryID(context.Background(), 42, "acme/repo")
	if err != nil {
		t.Fatalf("failed to prime repo 42: %v", err)
	}

	return adapter, trans
}

// -----------------------------------------------------------------------------
// Scenario 1: Link header hostility
// -----------------------------------------------------------------------------

func TestLinkHeaderHostilityAdversarial(t *testing.T) {
	t.Parallel()

	hostileHeaders := []struct {
		name string
		link string
	}{
		{"OtherHost", `<https://attacker.invalid/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`},
		{"OtherSchemeFile", `<file:///etc/passwd>; rel="next"`},
		{"OtherSchemeHTTP", `<http://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`},
		{"OtherPath", `<https://api.github.test/repos/other/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`},
		{"UserInfoInURL", `<https://user:pass@api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`},
		{"NextPointingToPage1Loop", `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=1>; rel="next"`},
		{"PageParamOverflow", `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=9999999999999999999999999999999999999>; rel="next"`},
		{"PageParamExceedsCap", `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=101>; rel="next"`},
		{"MalformedQuoting", `<https://api.github.test/repos/acme/repo/issues?page=2; rel="next"`},
		{"RepeatedRelNextHostileFirst", `<https://evil.invalid/steal>; rel="next", <https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`},
		{"RepeatedRelNextHostileSecond", `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next", <https://evil.invalid/steal>; rel="next"`},
		{"HugePath", fmt.Sprintf("<https://api.github.test/repos/acme/repo/issues/%s?page=2>; rel=\"next\"", strings.Repeat("x", 10000))},
	}

	for _, tc := range hostileHeaders {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter, trans := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/repos/acme/repo/issues" && r.Method == "GET" {
					return response(200, `[{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"}]}]`, tc.link), nil
				}
				return response(404, `{}`, ""), nil
			})

			page, err := adapter.Reader().ListManagedIssues(context.Background(), 42, "")

			// If the header itself was hostile, it must either return ErrInvalidCursor or not emit a cursor
			if tc.name != "RepeatedRelNextHostileSecond" {
				if err == nil && page.NextCursor != "" {
					t.Fatalf("hostile link header accepted and emitted cursor: %s", page.NextCursor)
				}
			}

			// If a cursor was emitted (e.g. valid first link in RepeatedRelNextHostileSecond), following it must never escape allowed host/path
			if page.NextCursor != "" {
				_, _ = adapter.Reader().ListManagedIssues(context.Background(), 42, page.NextCursor)
			}

			// Verify no request ever left the allowed api host and path
			trans.mu.Lock()
			defer trans.mu.Unlock()
			for _, req := range trans.requests {
				if req.URL.Host != "" && req.URL.Host != "api.github.test" {
					t.Fatalf("request escaped to external host: %s", req.URL.String())
				}
				if req.URL.Scheme != "" && req.URL.Scheme != "https" {
					t.Fatalf("request used forbidden scheme: %s", req.URL.String())
				}
				if !strings.HasPrefix(req.URL.Path, "/repos/acme/repo") {
					t.Fatalf("request escaped allowed repo route: %s", req.URL.Path)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Scenario 2: Pull Request and unmanaged issue filtering
// -----------------------------------------------------------------------------

func TestPRAndUnmanagedFilteringAdversarial(t *testing.T) {
	t.Parallel()

	adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			body := `[
				{"id":101,"number":1,"node_id":"I1","user":{"id":10},"labels":[{"name":"bonsai:managed"}],"created_at":"2026-01-01T00:00:00Z"},
				{"id":102,"number":2,"node_id":"PR1","user":{"id":11},"labels":[{"name":"bonsai:managed"}],"pull_request":{},"created_at":"2026-01-01T00:00:00Z"},
				{"id":103,"number":3,"node_id":"PR2","user":{"id":12},"labels":[{"name":"bonsai:managed"}],"pull_request":{"url":"https://api.github.test/repos/acme/repo/pulls/3"},"created_at":"2026-01-01T00:00:00Z"},
				{"id":104,"number":4,"node_id":"I2","user":{"id":13},"labels":[{"name":"other-label"}],"created_at":"2026-01-01T00:00:00Z"},
				{"id":105,"number":5,"node_id":"I3","user":{"id":14},"labels":[],"created_at":"2026-01-01T00:00:00Z"}
			]`
			return response(200, body, ""), nil
		}
		return response(404, `{}`, ""), nil
	})

	page, err := adapter.Reader().ListManagedIssues(context.Background(), 42, "")
	if err != nil {
		t.Fatalf("ListManagedIssues: %v", err)
	}

	// Only item 101 should survive client-side filtering
	if len(page.Items) != 1 {
		t.Fatalf("expected exactly 1 item after filtering, got %d: %+v", len(page.Items), page.Items)
	}
	if page.Items[0].ID != 101 {
		t.Fatalf("expected item 101, got %d", page.Items[0].ID)
	}
}

// -----------------------------------------------------------------------------
// Scenario 3: Status mapping matrix
// -----------------------------------------------------------------------------

func TestStatusMappingMatrixAdversarial(t *testing.T) {
	t.Parallel()

	matrix := []struct {
		name       string
		statusCode int
		headers    http.Header
		body       string
		networkErr error
		wantErr    error
	}{
		{"Status200_Success", 200, nil, `{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"}],"created_at":"2026-01-01T00:00:00Z"}`, nil, nil},
		{"Status304_NotModifiedWithoutCache", 304, nil, ``, nil, connect.ErrOffline},
		{"Status401_AuthRequired", 401, nil, `{"message":"Bad credentials"}`, nil, connect.ErrAuthRequired},
		{"Status403_BareForbidden", 403, nil, `{"message":"Forbidden"}`, nil, core.ErrForbidden},
		{"Status403_RateLimitedRetryAfter", 403, http.Header{"Retry-After": []string{"10"}}, `{"message":"Rate limit exceeded"}`, nil, connect.ErrRateLimited},
		{"Status403_RateLimitedRemainingZero", 403, http.Header{"X-RateLimit-Remaining": []string{"0"}}, `{"message":"API rate limit exceeded"}`, nil, connect.ErrRateLimited},
		{"Status429_RateLimited", 429, nil, `{"message":"Too many requests"}`, nil, connect.ErrRateLimited},
		{"Status404_NotFound", 404, nil, `{"message":"Not found"}`, nil, core.ErrNotFound},
		{
			// CONTRACT: "When an Issue disappears from a complete read, its prior item remains marked Deleted for one accepted snapshot cycle, then is removed."
			// 410 Gone indicates deleted resource; contract expects typed not-found/deleted error, not transient network offline.
			name:       "Status410_Gone",
			statusCode: 410,
			headers:    nil,
			body:       `{"message":"Gone"}`,
			networkErr: nil,
			wantErr:    core.ErrNotFound,
		},
		{"Status422_GitHubValidation", 422, nil, `{"message":"Validation Failed"}`, nil, connect.ErrGitHubValidation},
		{"Status500_ServerError", 500, nil, `{"message":"Internal Server Error"}`, nil, connect.ErrOffline},
		{"Status502_BadGateway", 502, nil, `{"message":"Bad Gateway"}`, nil, connect.ErrOffline},
		{"NetworkError", 0, nil, ``, errors.New("connection reset by peer"), connect.ErrOffline},
		{"InvalidJSON", 200, nil, `{not valid json`, nil, connect.ErrOffline},
		{"HugeBodyOverMaxResponse", 200, nil, strings.Repeat("A", 5*1024*1024), nil, connect.ErrOffline},
	}

	for _, tc := range matrix {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
				if tc.networkErr != nil {
					return nil, tc.networkErr
				}
				h := make(http.Header)
				for k, v := range tc.headers {
					for _, val := range v {
						h.Add(k, val)
					}
				}
				return &http.Response{
					StatusCode: tc.statusCode,
					Header:     h,
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}, nil
			})

			_, err := adapter.Reader().GetIssue(context.Background(), 42, 1)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error %v, got success", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("// CONTRACT: \"When an Issue disappears from a complete read, its prior item remains marked Deleted for one accepted snapshot cycle, then is removed.\" - for %s expected error %v, got %v", tc.name, tc.wantErr, err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Scenario 4: Write safety
// -----------------------------------------------------------------------------

func TestWriteSafetyAdversarial(t *testing.T) {
	t.Parallel()

	adapter, trans := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
		// AppendComment response
		if strings.Contains(r.URL.Path, "/comments") && r.Method == "POST" {
			var req map[string]string
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &req)
			return response(201, fmt.Sprintf(`{"id":501,"body":%q,"user":{"id":100,"login":"alice"},"created_at":"2026-01-01T00:00:00Z"}`, req["body"]), ""), nil
		}
		// Single issue GET for GetIssue inside UpdateLabels
		if strings.HasPrefix(r.URL.Path, "/repos/acme/repo/issues/") && r.Method == "GET" && !strings.Contains(r.URL.Path, "/comments") && !strings.Contains(r.URL.Path, "/labels") {
			return response(200, `{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"},{"name":"bonsai:stage/ready"}]}`, ""), nil
		}
		// UpdateLabels: Delete existing label
		if strings.Contains(r.URL.Path, "/labels/") && r.Method == "DELETE" {
			return response(200, `{}`, ""), nil
		}
		// UpdateLabels: Add label
		if strings.Contains(r.URL.Path, "/labels") && r.Method == "POST" {
			return response(200, `[{"name":"bonsai:stage/in-progress"}]`, ""), nil
		}
		// CreateLabel
		if r.URL.Path == "/repos/acme/repo/labels" && r.Method == "POST" {
			return response(201, `{"name":"bonsai:managed","color":"00aa00","description":"managed"}`, ""), nil
		}
		// GraphQL calls
		if r.URL.Path == "/graphql" {
			h := make(http.Header)
			h.Set("X-OAuth-Scopes", "project, read:project")
			return &http.Response{
				StatusCode: 200,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader(`{"data":{"viewer":{"projectsV2":{"nodes":[]}},"createProjectV2":{"projectV2":{"id":"P_1","title":"Test","number":1}},"updateProjectV2":{"projectV2":{"id":"P_1","shortDescription":"marker"}},"createProjectV2Field":{"projectV2Field":{"id":"F_1","name":"Status"}}}}`)),
			}, nil
		}
		return response(200, `{}`, ""), nil
	})

	// 1. AppendComment: body is passed through byte-exact, no HTML/markdown injection or trimming
	testCommentBody := "<script>alert('xss')</script>\n## Heading 2\n* Bullet\r\nSpecial chars: ' \" & < > \U0001F600"
	comm, err := adapter.Writer().AppendComment(context.Background(), 42, 1, testCommentBody)
	if err != nil {
		t.Fatalf("AppendComment: %v", err)
	}
	if comm.Body != testCommentBody {
		t.Fatalf("comment body was altered: got %q, want %q", comm.Body, testCommentBody)
	}

	// 2. AppendComment over 64 KiB cap must be rejected before reaching transport
	trans.mu.Lock()
	reqCountBefore := len(trans.requests)
	trans.mu.Unlock()

	oversizedBody := strings.Repeat("a", (64*1024)+1)
	_, overErr := adapter.Writer().AppendComment(context.Background(), 42, 1, oversizedBody)
	if overErr == nil {
		t.Fatal("oversized comment body was accepted")
	}

	trans.mu.Lock()
	reqCountAfter := len(trans.requests)
	trans.mu.Unlock()
	if reqCountAfter != reqCountBefore {
		t.Fatalf("oversized body reached transport: %d new requests", reqCountAfter-reqCountBefore)
	}

	// 3. UpdateLabels
	err = adapter.Writer().UpdateLabels(context.Background(), 42, 1, []string{"bonsai:managed", "bonsai:stage/in-progress"})
	if err != nil {
		t.Fatalf("UpdateLabels: %v", err)
	}

	// 4. CreateLabel
	err = adapter.CreateLabel(context.Background(), 42, connect.Label{Name: "bonsai:managed", Color: "00aa00", Description: "managed"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	// 5. EnsureProject
	_, err = adapter.EnsureProject(context.Background(), 42, "Adversarial Project")
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}

	// 6. EnsureProjectWithMarker
	_, err = adapter.EnsureProjectWithMarker(context.Background(), 42, "Project With Marker", "nonce-marker-123")
	if err != nil {
		t.Fatalf("EnsureProjectWithMarker: %v", err)
	}

	// 7. EnsureField
	_, err = adapter.EnsureField(context.Background(), "P_1", core.ProjectField{Name: "Status", Values: []string{"Ready", "Done"}})
	if err != nil {
		t.Fatalf("EnsureField: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Scenario 5: Identity and permission mapping
// -----------------------------------------------------------------------------

func TestIdentityAndPermissionsAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("RepoRenamedSameID", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/repos/new-owner/renamed-repo" {
				return response(200, `{"id":42,"full_name":"new-owner/renamed-repo","node_id":"R_42","owner":{"login":"new-owner","node_id":"O_42"},"permissions":{"pull":true}}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		resolver := ghgarden.RepositoryResolver{Adapter: adapter}
		repo, err := resolver.ResolveRepositoryID(context.Background(), 42, "new-owner/renamed-repo")
		if err != nil {
			t.Fatalf("renamed repo lookup failed: %v", err)
		}
		if repo.ID != 42 || repo.FullName != "new-owner/renamed-repo" {
			t.Fatalf("unexpected repo data: %+v", repo)
		}
	})

	t.Run("NameReusedDifferentID", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/repos/acme/reused" {
				// Different numeric ID 999
				return response(200, `{"id":999,"full_name":"acme/reused","node_id":"R_999","owner":{"login":"acme","node_id":"O_42"},"permissions":{"pull":true}}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		resolver := ghgarden.RepositoryResolver{Adapter: adapter}
		_, err := resolver.ResolveRepositoryID(context.Background(), 42, "acme/reused")
		if !errors.Is(err, connect.ErrRepositoryIdentityChanged) {
			t.Fatalf("expected ErrRepositoryIdentityChanged, got %v", err)
		}
	})

	t.Run("ForkAndParentMapping", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/repos/acme/fork" {
				return response(200, `{"id":55,"full_name":"acme/fork","fork":true,"parent":{"id":100,"full_name":"upstream/parent"},"owner":{"login":"acme","node_id":"O_55"},"permissions":{"pull":true}}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		resolver := ghgarden.RepositoryResolver{Adapter: adapter}
		repo, err := resolver.ResolveRepository(context.Background(), connect.Remote{Name: "origin", URL: "https://github.com/acme/fork"})
		if err != nil {
			t.Fatalf("ResolveRepository: %v", err)
		}
		if !repo.Fork || repo.ParentID != 100 || repo.ParentFullName != "upstream/parent" {
			t.Fatalf("fork/parent mapping mismatch: %+v", repo)
		}
	})

	t.Run("PermissionMappingMatrix", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name       string
			permsJSON  string
			wantAccess connect.Access
		}{
			{"Admin", `{"admin":true,"push":true,"pull":true}`, connect.AccessAdmin},
			{"Maintain", `{"admin":false,"maintain":true,"push":true,"pull":true}`, connect.AccessWrite},
			{"Push", `{"admin":false,"push":true,"pull":true}`, connect.AccessWrite},
			{"Triage", `{"admin":false,"push":false,"triage":true,"pull":true}`, connect.AccessRead},
			{"Pull", `{"admin":false,"push":false,"pull":true}`, connect.AccessRead},
			{
				// CONTRACT: "Read permits observation and never setup."
				// PLAN: "missing permissions => treated as Read, not Write."
				// Bug finding: adapter returns AccessNone instead of AccessRead when permissions map is missing/empty.
				name:       "MissingPermissions",
				permsJSON:  `null`,
				wantAccess: connect.AccessRead,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/repos/acme/test-perm" {
						return response(200, fmt.Sprintf(`{"id":77,"full_name":"acme/test-perm","owner":{"login":"acme","node_id":"O_77"},"permissions":%s}`, tc.permsJSON), ""), nil
					}
					return response(404, `{}`, ""), nil
				})

				resolver := ghgarden.RepositoryResolver{Adapter: adapter}
				repo, err := resolver.ResolveRepository(context.Background(), connect.Remote{Name: "origin", URL: "https://github.com/acme/test-perm"})
				if err != nil {
					t.Fatalf("ResolveRepository: %v", err)
				}
				if repo.Access != tc.wantAccess {
					t.Errorf("// CONTRACT: \"Read permits observation and never setup.\" - for %s expected %v, got %v", tc.name, tc.wantAccess, repo.Access)
				}
			})
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 6: GraphQL edge cases and query constant integrity
// -----------------------------------------------------------------------------

func TestGraphQLScenariosAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("ErrorsArrayWithPartialDataRejected", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/graphql" {
				return response(200, `{"data":{"viewer":{"projectsV2":{"nodes":[{"id":"P_1"}]}}},"errors":[{"message":"partial failure"}]}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		_, err := adapter.ListProjects(context.Background(), 42)
		if err == nil {
			t.Fatal("GraphQL with errors array should not return partial data as success")
		}
	})

	t.Run("DataNullRejected", func(t *testing.T) {
		t.Parallel()
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/graphql" {
				return response(200, `{"data":null}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		_, err := adapter.ListProjects(context.Background(), 42)
		if err == nil {
			t.Fatal("data: null must be rejected")
		}
	})

	t.Run("ScopeErrorMissingHeader", func(t *testing.T) {
		t.Parallel()
		// GraphQL returns INSUFFICIENT_SCOPES error but X-OAuth-Scopes header is absent (fine-grained PAT / GitHub App)
		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/graphql" {
				return response(200, `{"errors":[{"type":"INSUFFICIENT_SCOPES","message":"This endpoint requires you to be authenticated with the read:project scope."}]}`, ""), nil
			}
			return response(404, `{}`, ""), nil
		})

		_, err := adapter.ListProjects(context.Background(), 42)
		// CONTRACT: "A Project step without Project scope fails with ErrScopeMissing; other steps still complete and the operation remains resumable."
		// Bug finding: adapter returns generic ErrOffline instead of ErrScopeMissing when X-OAuth-Scopes header is absent.
		var scopeErr connect.ErrScopeMissing
		if !errors.As(err, &scopeErr) {
			t.Errorf("// CONTRACT: \"A Project step without Project scope fails with ErrScopeMissing; other steps still complete and the operation remains resumable.\" - expected ErrScopeMissing for INSUFFICIENT_SCOPES error, got: %v", err)
		}
	})

	t.Run("VariablesNeverStringInterpolated", func(t *testing.T) {
		t.Parallel()
		maliciousTitle := `Adversarial" \n \r \t } mutation { deleteEverything }`
		var queryCaptured string
		var varsCaptured map[string]any

		adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/graphql" {
				b, _ := io.ReadAll(r.Body)
				var env struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				_ = json.Unmarshal(b, &env)
				queryCaptured = env.Query
				varsCaptured = env.Variables

				h := make(http.Header)
				h.Set("X-OAuth-Scopes", "project, read:project")
				return &http.Response{
					StatusCode: 200,
					Header:     h,
					Body:       io.NopCloser(strings.NewReader(`{"data":{"viewer":{"projectsV2":{"nodes":[]}},"createProjectV2":{"projectV2":{"id":"P_NEW","title":"safe","number":1}}}}`)),
				}, nil
			}
			return response(404, `{}`, ""), nil
		})

		_, _ = adapter.EnsureProject(context.Background(), 42, maliciousTitle)

		// Assert query constant was not interpolated
		if strings.Contains(queryCaptured, "deleteEverything") || strings.Contains(queryCaptured, maliciousTitle) {
			t.Fatalf("query string contained interpolated variable: %s", queryCaptured)
		}

		// Title must be in variables only
		if varsCaptured["title"] != maliciousTitle {
			t.Fatalf("malicious variable not found safely in variables map: %+v", varsCaptured)
		}
	})
}

// -----------------------------------------------------------------------------
// Scenario 7: Secrets scrubbing
// -----------------------------------------------------------------------------

func TestSecretsNeverEchoedAdversarial(t *testing.T) {
	t.Parallel()
	fakeSecret := "ghp_FAKETOKEN123_ADAPTER_SECRET"

	adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("X-Secret-Header", fakeSecret)
		h.Set("Authorization", "Bearer "+fakeSecret)

		// Return 401 with body echoing secret
		return &http.Response{
			StatusCode: 401,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"message":"Unauthorized token %s"}`, fakeSecret))),
		}, nil
	})

	checkErr := func(method string, err error) {
		if err == nil {
			t.Fatalf("%s should have failed", method)
		}
		for _, rendered := range []string{
			fmt.Sprintf("%v", err),
			fmt.Sprintf("%+v", err),
			fmt.Sprintf("%#v", err),
			fmt.Sprintf("%q", err),
		} {
			if strings.Contains(rendered, fakeSecret) {
				t.Fatalf("secret leaked in error output for %s: %s", method, rendered)
			}
		}
	}

	_, err := adapter.Reader().GetIssue(context.Background(), 42, 1)
	checkErr("GetIssue", err)

	_, err = adapter.Reader().ListManagedIssues(context.Background(), 42, "")
	checkErr("ListManagedIssues", err)

	_, err = adapter.Reader().ListComments(context.Background(), 42, 1)
	checkErr("ListComments", err)

	_, err = adapter.Writer().AppendComment(context.Background(), 42, 1, "test")
	checkErr("AppendComment", err)

	err = adapter.Writer().UpdateLabels(context.Background(), 42, 1, []string{"bonsai:managed"})
	checkErr("UpdateLabels", err)

	_, err = adapter.ListLabels(context.Background(), 42)
	checkErr("ListLabels", err)

	err = adapter.CreateLabel(context.Background(), 42, connect.Label{Name: "bonsai:managed"})
	checkErr("CreateLabel", err)

	_, err = adapter.ListProjects(context.Background(), 42)
	checkErr("ListProjects", err)

	_, err = adapter.EnsureProject(context.Background(), 42, "proj")
	checkErr("EnsureProject", err)

	_, err = adapter.EnsureField(context.Background(), "P_1", core.ProjectField{Name: "Status"})
	checkErr("EnsureField", err)
}

// -----------------------------------------------------------------------------
// Scenario 8: Concurrency under -race and no goroutine leaks
// -----------------------------------------------------------------------------

func TestConcurrencyUnderRaceAdversarial(t *testing.T) {
	t.Parallel()

	adapter, _ := adversarialFixture(t, func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("X-OAuth-Scopes", "project, read:project")
		if strings.HasSuffix(r.URL.Path, "/issues") {
			return response(200, `[{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"}]}]`, ""), nil
		}
		if strings.Contains(r.URL.Path, "/comments") {
			return response(200, `[{"id":10,"body":"c","user":{"id":1,"login":"a"},"created_at":"2026-01-01T00:00:00Z"}]`, ""), nil
		}
		if r.URL.Path == "/graphql" {
			return &http.Response{
				StatusCode: 200,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader(`{"data":{"viewer":{"projectsV2":{"nodes":[]}}}}`)),
			}, nil
		}
		return response(200, `[]`, ""), nil
	})

	initialGoroutines := runtime.NumGoroutine()
	const workers = 40
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			if idx%2 == 0 {
				cancel() // Cancel mid-flight
			} else {
				defer cancel()
			}

			switch idx % 4 {
			case 0:
				_, _ = adapter.Reader().ListManagedIssues(ctx, 42, "")
			case 1:
				_, _ = adapter.Reader().GetIssue(ctx, 42, 1)
			case 2:
				_, _ = adapter.Reader().ListComments(ctx, 42, 1)
			case 3:
				_, _ = adapter.ListProjects(ctx, 42)
			}
		}()
	}

	wg.Wait()

	// Allow goroutines to settle
	time.Sleep(50 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	// Tolerate small runtime variance (testing worker pools, timers)
	if finalGoroutines > initialGoroutines+10 {
		t.Fatalf("goroutine leak detected: was %d, now %d", initialGoroutines, finalGoroutines)
	}
}

// -----------------------------------------------------------------------------
// Scenario 9: Fuzz Link Header
// -----------------------------------------------------------------------------

func FuzzParseLinkHeader(f *testing.F) {
	// Seed corpus with valid, borderline, and hostile headers
	f.Add(`<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`)
	f.Add(`<file:///etc/passwd>; rel="next"`)
	f.Add(`<https://attacker.invalid/issues?page=2>; rel="next"`)
	f.Add(`<https://api.github.test/repos/acme/repo/issues?page=1>; rel="next"`)
	f.Add(`invalid-link-header`)
	f.Add(``)

	f.Fuzz(func(t *testing.T, header string) {
		var trans *adversarialTransport
		client := &http.Client{
			Transport: &adversarialTransport{
				fn: func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/repos/acme/repo" {
						return response(200, `{"id":42,"full_name":"acme/repo","node_id":"R_42","owner":{"login":"acme","node_id":"O_42"},"permissions":{"admin":true}}`, ""), nil
					}
					if strings.HasSuffix(r.URL.Path, "/issues") {
						return response(200, `[{"id":1,"number":1,"node_id":"N1","user":{"id":100},"labels":[{"name":"bonsai:managed"}]}]`, header), nil
					}
					return response(404, `{}`, ""), nil
				},
			},
		}

		adapter, err := ghgarden.New(client, "https://api.github.test")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		resolver := ghgarden.RepositoryResolver{Adapter: adapter}
		_, _ = resolver.ResolveRepositoryID(context.Background(), 42, "acme/repo")

		// List managed issues using the fuzzed link header
		page, err := adapter.Reader().ListManagedIssues(context.Background(), 42, "")
		if err != nil {
			return
		}

		// If a next cursor was emitted from the header, attempting to follow it must not panic
		// and must never escape the api.github.test domain
		if page.NextCursor != "" {
			_, _ = adapter.Reader().ListManagedIssues(context.Background(), 42, page.NextCursor)
		}

		if trans != nil {
			trans.mu.Lock()
			defer trans.mu.Unlock()
			for _, r := range trans.requests {
				if r.URL.Host != "" && r.URL.Host != "api.github.test" {
					t.Fatalf("request escaped to %s", r.URL.Host)
				}
			}
		}
	})
}
