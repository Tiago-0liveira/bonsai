package garden

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
)

// createServer answers the repository lookup and records every issue-create request. It is separate
// from fixture because that transport forbids title and body payloads on every other Issue mutation.
type createServer struct {
	mu      sync.Mutex
	creates []map[string]any
	reply   func(n int) (*http.Response, error)
}

func (s *createServer) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/repos/acme/repo" {
		return response(200, `{"id":9,"full_name":"acme/repo","node_id":"R_9","owner":{"login":"acme","node_id":"O_9"},"permissions":{"pull":true,"push":true}}`, ""), nil
	}
	if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/repo/issues" {
		return response(404, `{}`, ""), nil
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.creates = append(s.creates, body)
	n := len(s.creates)
	s.mu.Unlock()
	return s.reply(n)
}

func creator(t *testing.T, reply func(n int) (*http.Response, error)) (*IssueCreator, *createServer) {
	t.Helper()
	s := &createServer{reply: reply}
	a, err := New(&http.Client{Transport: s}, "https://api.github.test")
	if err != nil {
		t.Fatal(err)
	}
	a.rememberRepository(repoJSON{ID: 9, FullName: "acme/repo", NodeID: "R_9"})
	return a.Creator(), s
}

const createdIssue = `{"id":7001,"number":42,"title":"Idea","node_id":"I_1","body":"b","user":{"id":1001},"labels":[{"name":"bonsai:managed"},{"name":"bonsai:type/bug"},{"name":"bonsai:stage/draft"}],"repository_url":"https://api.github.test/repos/acme/repo"}`

func TestCreateIssueSendsTitleBodyAndOnlyBonsaiLabels(t *testing.T) {
	c, s := creator(t, func(int) (*http.Response, error) { return response(201, createdIssue, ""), nil })
	labels := []string{core.ManagedLabel, core.FormatTypeLabel(core.TypeBug), core.FormatStageLabel(core.StageDraft)}
	got, err := c.CreateIssue(context.Background(), 9, core.NewIssue{Title: "Idea", Body: "b", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 7001 || got.Number != 42 || got.Repo != 9 || got.AuthorID != "1001" || len(got.Labels) != 3 {
		t.Fatalf("issue = %+v", got)
	}
	if len(s.creates) != 1 || s.creates[0]["title"] != "Idea" || s.creates[0]["body"] != "b" {
		t.Fatalf("request = %+v", s.creates)
	}
	sent, _ := s.creates[0]["labels"].([]any)
	if len(sent) != 3 {
		t.Fatalf("labels = %v", sent)
	}
	if n := c.a.issueNumbers[9][7001]; n != 42 {
		t.Fatalf("number not cached: %d", n)
	}
}

func TestCreateIssueRefusesBeforeAnyRequest(t *testing.T) {
	for name, n := range map[string]core.NewIssue{
		"foreign label": {Title: "x", Labels: []string{"bug"}},
		"blank title":   {Title: "  ", Labels: []string{core.ManagedLabel}},
		"huge body":     {Title: "x", Body: strings.Repeat("a", maxComment+1)},
	} {
		c, s := creator(t, func(int) (*http.Response, error) { t.Fatal("request sent"); return nil, nil })
		if _, err := c.CreateIssue(context.Background(), 9, n); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if len(s.creates) != 0 {
			t.Errorf("%s: sent %d requests", name, len(s.creates))
		}
	}
}

func TestCreateIssueErrorClasses(t *testing.T) {
	cases := []struct {
		name     string
		reply    func(int) (*http.Response, error)
		rejected bool
		is       error
	}{
		{"validation is definitive", func(int) (*http.Response, error) { return response(422, `{}`, ""), nil }, true, connect.ErrGitHubValidation},
		{"forbidden is definitive", func(int) (*http.Response, error) { return response(403, `{}`, ""), nil }, true, core.ErrForbidden},
		{"missing repo is definitive", func(int) (*http.Response, error) { return response(404, `{}`, ""), nil }, true, core.ErrNotFound},
		{"5xx is ambiguous", func(int) (*http.Response, error) { return response(502, `{}`, ""), nil }, false, core.ErrOffline},
		{"408 is ambiguous", func(int) (*http.Response, error) { return response(408, `{}`, ""), nil }, false, core.ErrOffline},
		{"transport error is ambiguous", func(int) (*http.Response, error) { return nil, errors.New("i/o timeout") }, false, core.ErrOffline},
		{"unreadable success is ambiguous", func(int) (*http.Response, error) { return response(201, `not json`, ""), nil }, false, core.ErrOffline},
		{"foreign repository is ambiguous", func(int) (*http.Response, error) {
			return response(201, strings.Replace(createdIssue, "/repos/acme/repo", "/repos/evil/other", 1), ""), nil
		}, false, core.ErrOffline},
		{"pull request is ambiguous", func(int) (*http.Response, error) {
			return response(201, strings.Replace(createdIssue, `"user"`, `"pull_request":{},"user"`, 1), ""), nil
		}, false, core.ErrOffline},
	}
	for _, tc := range cases {
		c, _ := creator(t, tc.reply)
		_, err := c.CreateIssue(context.Background(), 9, core.NewIssue{Title: "x", Labels: []string{core.ManagedLabel}})
		if err == nil || errors.Is(err, core.ErrRejected) != tc.rejected || !errors.Is(err, tc.is) {
			t.Errorf("%s: err = %v (rejected=%v)", tc.name, err, errors.Is(err, core.ErrRejected))
		}
	}
}

func TestCreateIssueRateLimitAndAuth(t *testing.T) {
	c, _ := creator(t, func(int) (*http.Response, error) {
		r := response(429, `{}`, "")
		r.Header.Set("Retry-After", "30")
		return r, nil
	})
	_, err := c.CreateIssue(context.Background(), 9, core.NewIssue{Title: "x"})
	if !errors.Is(err, core.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	c, _ = creator(t, func(int) (*http.Response, error) { return response(401, `{}`, ""), nil })
	if _, err = c.CreateIssue(context.Background(), 9, core.NewIssue{Title: "x"}); !errors.Is(err, core.ErrAuthRequired) {
		t.Fatalf("err = %v", err)
	}
}
