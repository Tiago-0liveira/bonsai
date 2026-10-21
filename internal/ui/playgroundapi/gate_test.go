package playgroundapi_test

import (
	"context"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi/fake"
)

func TestGateDropsLateAndForeignReplies(t *testing.T) {
	var g playgroundapi.Gate
	if g.Accept(playgroundapi.Stamp{Generation: 1}) {
		t.Fatal("a reply without a scope must be dropped")
	}
	if !g.Accept(playgroundapi.Stamp{Scope: "a", Generation: 2}) {
		t.Fatal("first reply is accepted")
	}
	for name, s := range map[string]playgroundapi.Stamp{
		"late":  {Scope: "a", Generation: 1},
		"same":  {Scope: "a", Generation: 2},
		"other": {Scope: "b", Generation: 9},
	} {
		if g.Accept(s) {
			t.Errorf("%s reply accepted", name)
		}
	}
	if !g.Accept(playgroundapi.Stamp{Scope: "a", Generation: 3}) || g.Last().Generation != 3 {
		t.Fatal("newer reply must be accepted")
	}
	g.Reset()
	if !g.Accept(playgroundapi.Stamp{Scope: "b", Generation: 1}) {
		t.Fatal("reset gate accepts the first reply again")
	}
}

func TestFakeStampsPerKindAndIndependentFailure(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	a, _ := f.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	b, _ := f.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	p, _ := f.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if b.Stamp.Generation <= a.Stamp.Generation || p.Stamp.Generation != 1 || a.Stamp.Scope != fake.Scope {
		t.Fatalf("stamps: %+v %+v %+v", a.Stamp, b.Stamp, p.Stamp)
	}
	f.PRErr = &playgroundapi.Error{Code: playgroundapi.CodeUnavailable}
	if _, err := f.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"}); err == nil {
		t.Fatal("scripted PR failure expected")
	}
	if _, err := f.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"}); err != nil {
		t.Fatalf("a PR failure must not fail Checks: %v", err)
	}
}

func TestFakeLinkRules(t *testing.T) {
	f := fake.New()
	ctx := context.Background()
	if v, err := f.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 5}); err != nil || !v.Created {
		t.Fatalf("link: %+v %v", v, err)
	}
	if v, err := f.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 5}); err != nil || v.Created {
		t.Fatalf("same link is idempotent: %+v %v", v, err)
	}
	if _, err := f.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 6}); err == nil {
		t.Fatal("another issue needs Replace")
	}
	if v, err := f.LinkSession(ctx, playgroundapi.LinkRequest{SessionID: "s", IssueNumber: 6, Replace: true}); err != nil || v.Link.IssueNumber != 6 {
		t.Fatalf("replace: %+v %v", v, err)
	}
}
