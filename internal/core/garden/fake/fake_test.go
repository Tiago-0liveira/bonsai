package fake_test

import (
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
)

func TestFakeContract(t *testing.T) {
	f := fake.New()
	f.SetPageSize(1)
	for i := garden.IssueID(1); i <= 2; i++ {
		f.SeedIssue(garden.Issue{Repo: 9, ID: i, Labels: []string{garden.ManagedLabel}})
	}
	p, err := f.ListManagedIssues(t.Context(), 9, "")
	if err != nil || p.Complete || p.NextCursor == "" {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = f.ListManagedIssues(t.Context(), 9, p.NextCursor)
	if err != nil || !p.Complete || len(p.Items) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	f.SetDenied(true)
	if _, err = f.GetIssue(t.Context(), 9, 1); !errors.Is(err, garden.ErrPermissionDenied) {
		t.Fatalf("%v", err)
	}
}
