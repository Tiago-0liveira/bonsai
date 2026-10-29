package gitbridge

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type fakeLaunchIsolationService struct {
	domain.LocalGitService
	mu       sync.Mutex
	calls    []string
	proposal domain.IsolationProposal
	check    domain.IsolationCheck
	head     domain.PublishedHead
	err      error
}

func (f *fakeLaunchIsolationService) ListWorktrees(context.Context, string) ([]domain.Worktree, error) {
	return []domain.Worktree{{ID: "wt-impl", RepositoryID: "local"}}, nil
}
func (f *fakeLaunchIsolationService) PreviewIsolation(_ context.Context, r domain.IsolationRequest) (domain.IsolationProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "preview")
	p := f.proposal
	p.IsolationRequest = r
	return p, f.err
}
func (f *fakeLaunchIsolationService) CreateIsolation(_ context.Context, r domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create")
	c := f.check
	c.Proposal = r.Proposal
	return c, f.err
}
func (f *fakeLaunchIsolationService) InspectIsolation(_ context.Context, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "inspect")
	c := f.check
	c.Proposal = p
	return c, f.err
}
func (f *fakeLaunchIsolationService) RemoveIsolation(context.Context, domain.IsolationRemoveRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove")
	return f.err
}
func (f *fakeLaunchIsolationService) InspectPublishedHead(context.Context, domain.IsolationProposal) (domain.PublishedHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "head")
	return f.head, f.err
}
func (f *fakeLaunchIsolationService) PushIsolation(context.Context, domain.IsolationPushRequest) (domain.PublishedHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "push")
	return f.head, f.err
}
func fakeLaunchBridgeFixture(t *testing.T) (*fakeLaunchIsolationService, *Executor) {
	t.Helper()
	p := domain.IsolationProposal{IsolationRequest: domain.IsolationRequest{OperationID: "impl-bridge", RepositoryID: "local", Branch: "bonsai/9-test", BaseRef: "main", BaseSHA: strings.Repeat("a", 40), RemoteName: "origin", Repository: "owner/repo"}, Path: filepath.Join(t.TempDir(), "worktree"), RootRevision: 1, ConfigHash: strings.Repeat("b", 64)}
	f := &fakeLaunchIsolationService{proposal: p, check: domain.IsolationCheck{Proposal: p, Worktree: domain.Worktree{ID: "wt-impl", RepositoryID: "local"}, Exists: true, Owned: true, Clean: true, HeadSHA: p.BaseSHA, Hooks: "done", Fingerprint: strings.Repeat("c", 64)}, head: domain.PublishedHead{HeadSHA: p.BaseSHA}}
	db, err := gitstore.Open(filepath.Join(t.TempDir(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	return f, &Executor{Local: f, Journal: db}
}
func TestIsolationBridgeSixTypedCommandsAndReadClassification(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	for _, tc := range []struct {
		name, key string
		value     any
		read      bool
	}{{"preview", "isolation_request", f.proposal.IsolationRequest, true}, {"create", "isolation_create", domain.IsolationCreateRequest{Proposal: f.proposal}, false}, {"inspect", "isolation_proposal", f.proposal, true}, {"remove", "isolation_remove", domain.IsolationRemoveRequest{Check: f.check}, false}, {"head", "isolation_proposal", f.proposal, true}, {"push", "isolation_push", domain.IsolationPushRequest{Proposal: f.proposal, HeadSHA: f.proposal.BaseSHA}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			name := "git.isolation." + tc.name
			if !Allowed(name) || IsRead(name) != tc.read {
				t.Fatalf("allowlist %s read=%v", name, IsRead(name))
			}
			c := Command{ID: "impl-bridge/" + tc.name, UserID: "1001", RepositoryID: "local", Type: name, Arguments: MarshalArguments(map[string]any{tc.key: tc.value}), CreatedAt: time.Now().UTC()}
			r := x.Execute(context.Background(), c)
			if r.Error != nil || r.ID != c.ID {
				t.Fatalf("result=%+v", r)
			}
			if !tc.read {
				again := x.Execute(context.Background(), c)
				if !reflect.DeepEqual(r, again) {
					t.Fatalf("mutation replay differs: %+v / %+v", r, again)
				}
			}
		})
	}
	for _, name := range []string{"git.isolation.shell", "git.isolation.exec", "shell", "exec", "git.isolation.reset"} {
		if Allowed(name) {
			t.Fatalf("generic/destructive endpoint allowed: %s", name)
		}
	}
}
func TestIsolationBridgeStrictNestedArgumentsAndIdentityBinding(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	valid := string(MarshalArguments(map[string]any{"isolation_create": domain.IsolationCreateRequest{Proposal: f.proposal}}))
	cases := []string{`{}`, `{"isolation_create":null}`, `{"isolation_create":{},"branch":"unrelated"}`, strings.Replace(valid, `"proposal":{`, `"proposal":{"unknown":1,`, 1), strings.Replace(valid, `"branch":"bonsai/9-test"`, `"branch":"bonsai/9-test","branch":"other"`, 1), strings.Replace(valid, `"repository_id":"local"`, `"repository_id":"foreign"`, 1), valid + ` {}`, strings.Replace(valid, `"isolation_create":`, `"isolation_push":`, 1)}
	for i, args := range cases {
		r := x.Execute(context.Background(), Command{ID: time.Now().Format("150405.000000000") + string(rune('a'+i)), UserID: "1001", RepositoryID: "local", Type: "git.isolation.create", Arguments: json.RawMessage(args), CreatedAt: time.Now().UTC()})
		if r.Error == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid args reached optional authority: %v", f.calls)
	}
}

type fakeLaunchOldGit struct{ domain.LocalGitService }

func (fakeLaunchOldGit) ListWorktrees(context.Context, string) ([]domain.Worktree, error) {
	return nil, nil
}
func TestIsolationBridgeLegacyServiceRefusesOptionalCommands(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	x.Local = fakeLaunchOldGit{}
	r := x.Execute(context.Background(), Command{ID: "preview-old", UserID: "1001", RepositoryID: "local", Type: "git.isolation.preview", Arguments: MarshalArguments(map[string]any{"isolation_request": f.proposal.IsolationRequest}), CreatedAt: time.Now().UTC()})
	if r.Error == nil {
		t.Fatal("legacy service silently accepted isolation")
	}
}

type fakeLaunchGitCaller struct {
	mu            sync.Mutex
	executor      *Executor
	commands      []Command
	wrongID       bool
	badPayload    bool
	uncertainOnce bool
}

func (f *fakeLaunchGitCaller) GitContext(ctx context.Context, c Command) (*Result, error) {
	f.mu.Lock()
	f.commands = append(f.commands, c)
	wrong, bad, uncertain := f.wrongID, f.badPayload, f.uncertainOnce
	if uncertain && !IsRead(c.Type) {
		f.uncertainOnce = false
	} else {
		uncertain = false
	}
	f.mu.Unlock()
	r := f.executor.Execute(ctx, c)
	if uncertain {
		return &Result{ID: c.ID, Error: &domain.Error{Code: domain.Code(domain.ErrUncertain), Message: "safe"}}, nil
	}
	if wrong {
		r.ID = "wrong"
	}
	if bad {
		r.Payload = json.RawMessage(`{"unexpected":true}`)
	}
	return &r, nil
}
func TestIsolationClientPersistsOriginalMutationEnvelopeAcrossRestart(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	caller := &fakeLaunchGitCaller{executor: x}
	path := filepath.Join(t.TempDir(), "envelopes.json")
	journal, err := gitstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	client := &IsolationClient{Caller: caller, RepositoryID: "local", UserID: "1001", Now: func() time.Time { return now }, NewID: func() string { return "first" }, Journal: journal}
	req := domain.IsolationCreateRequest{Proposal: f.proposal}
	a, err := client.CreateIsolation(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := gitstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	client = &IsolationClient{Caller: caller, RepositoryID: "local", UserID: "1001", Now: func() time.Time { return now.Add(time.Hour) }, NewID: func() string { return "second" }, Journal: reopened}
	b, err := client.CreateIsolation(context.Background(), req)
	if err != nil || a.Worktree.ID != b.Worktree.ID {
		t.Fatalf("restart replay=%+v %v", b, err)
	}
	var original *Command
	for i := range caller.commands {
		c := caller.commands[i]
		if c.Type == "git.isolation.create" {
			if c.ID != f.proposal.OperationID+"/worktree" {
				t.Fatalf("unstable mutation ID: %s", c.ID)
			}
			if original == nil {
				copy := c
				original = &copy
			} else if c.CreatedAt != original.CreatedAt || c.UserID != original.UserID {
				t.Fatalf("replay envelope changed: %+v / %+v", c, *original)
			}
		}
	}
	count := 0
	for _, call := range f.calls {
		if call == "create" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("underlying effects=%d", count)
	}
}
func TestIsolationClientRejectsWrongResultIdentityAndType(t *testing.T) {
	for _, mode := range []string{"identity", "type"} {
		t.Run(mode, func(t *testing.T) {
			f, x := fakeLaunchBridgeFixture(t)
			caller := &fakeLaunchGitCaller{executor: x, wrongID: mode == "identity", badPayload: mode == "type"}
			client := &IsolationClient{Caller: caller, RepositoryID: "local", UserID: "1001", Now: time.Now, NewID: func() string { return "read" }}
			if _, err := client.PreviewIsolation(context.Background(), f.proposal.IsolationRequest); err == nil {
				t.Fatal("invalid typed result accepted")
			}
		})
	}
}
func TestIsolationClientUnknownCreateInspectsBeforeReissue(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	caller := &fakeLaunchGitCaller{executor: x, uncertainOnce: true}
	journal, err := gitstore.Open(filepath.Join(t.TempDir(), "envelopes.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &IsolationClient{Caller: caller, RepositoryID: "local", UserID: "1001", Now: time.Now, NewID: func() string { return "recover" }, Journal: journal}
	req := domain.IsolationCreateRequest{Proposal: f.proposal}
	if _, err = client.CreateIsolation(context.Background(), req); err != nil && !errors.Is(err, domain.ErrUncertain) {
		t.Fatal(err)
	}
	_, _ = client.CreateIsolation(context.Background(), req)
	creates := 0
	inspected := false
	for _, c := range caller.commands {
		if c.Type == "git.isolation.inspect" {
			inspected = true
		}
		if c.Type == "git.isolation.create" {
			creates++
			if creates > 1 && !inspected {
				t.Fatal("blind reissue after uncertain add")
			}
		}
	}
	effects := 0
	for _, call := range f.calls {
		if call == "create" {
			effects++
		}
	}
	if effects != 1 {
		t.Fatalf("uncertain create duplicated resource: %v", f.calls)
	}
}

func TestIsolationClientStablePushAndCleanupIDs(t *testing.T) {
	f, x := fakeLaunchBridgeFixture(t)
	caller := &fakeLaunchGitCaller{executor: x}
	journal, err := gitstore.Open(filepath.Join(t.TempDir(), "envelopes.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &IsolationClient{Caller: caller, RepositoryID: "local", UserID: "1001", Now: time.Now, NewID: func() string { return "typed" }, Journal: journal}
	if _, err := client.PushIsolation(context.Background(), domain.IsolationPushRequest{Proposal: f.proposal, HeadSHA: f.proposal.BaseSHA}); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: f.check}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"git.isolation.push": f.proposal.OperationID + "/push/" + f.proposal.BaseSHA, "git.isolation.remove": f.proposal.OperationID + "/cleanup/" + f.check.Fingerprint}
	for _, c := range caller.commands {
		if expected, ok := want[c.Type]; ok {
			if c.ID != expected || c.UserID != "1001" || c.RepositoryID != "local" {
				t.Fatalf("unstable/badly bound envelope: %+v", c)
			}
			delete(want, c.Type)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing typed envelopes=%v", want)
	}
}
