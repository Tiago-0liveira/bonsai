package drafts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPublicationIdentitiesAndDiscovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "drafts.json")
	s := testStore(t, path)
	d := seed(t, s)
	issue := IssueIdentity{ID: 909, Number: 42, URL: "https://github.com/example/repo/issues/42"}
	invalid := clone(d)
	invalid.Publication = Publication{State: Published, OperationID: "capture-1", Issue: &issue, VerifiedAt: testTime}
	if _, err := s.Save(ctx, testScope, invalid, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("skipped intent: %v", err)
	}
	d.Publication = Publication{State: Publishing, OperationID: "capture-1"}
	d, err := s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s, err = New(path, func() time.Time { return testTime }, func() ID { return "recovery-1" })
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, testScope, false)
	if err != nil || len(list) != 1 || list[0].Publication.State != Publishing || list[0].Publication.OperationID != "capture-1" {
		t.Fatalf("unknown publication lost: %+v, %v", list, err)
	}
	if _, err := s.Archive(ctx, testScope, d.ID, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("archive publishing: %v", err)
	}
	d.Publication.Issue = &issue
	d, err = s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	list, err = testStore(t, path).List(ctx, testScope, false)
	if err != nil || len(list) != 1 || list[0].Publication.Issue == nil || *list[0].Publication.Issue != issue {
		t.Fatalf("partial publication identity: %+v, %v", list, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Publication)
	}{
		{"missing verification", func(p *Publication) { p.State = Published }},
		{"missing issue", func(p *Publication) { p.State, p.Issue, p.VerifiedAt = Published, nil, testTime }},
		{"changed operation", func(p *Publication) { p.OperationID = "another-attempt" }},
		{"changed issue", func(p *Publication) { p.Issue.ID++ }},
		{"reverted to unpublished", func(p *Publication) { *p = Publication{State: Unpublished} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := clone(d)
			tc.mutate(&bad.Publication)
			if _, err := s.Save(ctx, testScope, bad, d.Revision); !errors.Is(err, ErrInvalid) {
				t.Fatalf("save: %v", err)
			}
		})
	}
	d.Publication.State = Published
	d.Publication.VerifiedAt = testTime.In(time.FixedZone("caller zone", 3600))
	d, err = s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	list, err = s.List(ctx, testScope, false)
	if err != nil || len(list) != 0 {
		t.Fatalf("published still active: %+v, %v", list, err)
	}
	list, err = testStore(t, path).List(ctx, testScope, true)
	if err != nil || len(list) != 1 || list[0].ID != d.ID || list[0].Publication.Issue.ID != issue.ID {
		t.Fatalf("published identity lost: %+v, %v", list, err)
	}
	bad := clone(d)
	bad.Publication.State = Publishing
	bad.Publication.VerifiedAt = time.Time{}
	if _, err := s.Save(ctx, testScope, bad, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("published regression: %v", err)
	}
	// Stale authoring content survives without claiming the original Issue.
	d.Title = "Late local edit"
	fork, err := s.Save(ctx, testScope, d, 1)
	if !errors.Is(err, ErrConflict) || fork.ID == d.ID || fork.Publication.State != Unpublished || fork.Publication.Issue != nil {
		t.Fatalf("published conflict: %+v, %v", fork, err)
	}
	list, err = testStore(t, path).List(ctx, testScope, false)
	if err != nil || len(list) != 1 || list[0].ID != fork.ID {
		t.Fatalf("late edit invisible: %+v, %v", list, err)
	}
}

func TestPublicationValidation(t *testing.T) {
	issue := &IssueIdentity{ID: 1, Number: 2, URL: "https://github.com/o/r/issues/2"}
	for _, tc := range []struct {
		name        string
		publication Publication
		valid       bool
	}{
		{"unpublished", Publication{State: Unpublished}, true},
		{"publishing unknown outcome", Publication{State: Publishing, OperationID: "op"}, true},
		{"publishing known issue", Publication{State: Publishing, OperationID: "op", Issue: issue}, true},
		{"published verified", Publication{State: Published, OperationID: "op", Issue: issue, VerifiedAt: testTime}, true},
		{"empty state", Publication{}, false},
		{"unpublished with issue", Publication{State: Unpublished, Issue: issue}, false},
		{"publishing without identity", Publication{State: Publishing}, false},
		{"publishing marked verified", Publication{State: Publishing, OperationID: "op", VerifiedAt: testTime}, false},
		{"published without verification", Publication{State: Published, OperationID: "op", Issue: issue}, false},
		{"published without number", Publication{State: Published, OperationID: "op", Issue: &IssueIdentity{ID: 1, URL: issue.URL}, VerifiedAt: testTime}, false},
		{"published without link", Publication{State: Published, OperationID: "op", Issue: &IssueIdentity{ID: 1, Number: 2}, VerifiedAt: testTime}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.publication.valid(); got != tc.valid {
				t.Fatalf("valid=%v, want %v", got, tc.valid)
			}
		})
	}
}

func TestPublishingAbandonAndFrozenContent(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, filepath.Join(t.TempDir(), "drafts.json"))
	d := seed(t, s)
	d.Publication = Publication{State: Publishing, OperationID: "capture-1"}
	d, err := s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	edited := clone(d)
	edited.Title = "Edited while the remote write is unknown"
	if _, err := s.Save(ctx, testScope, edited, d.Revision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("edit while publishing: %v", err)
	}
	d.Publication = Publication{State: Unpublished}
	d, err = s.Save(ctx, testScope, d, d.Revision)
	if err != nil {
		t.Fatalf("abandon publishing without Issue: %v", err)
	}
	if _, err := s.Archive(ctx, testScope, d.ID, d.Revision); err != nil {
		t.Fatalf("archive after abandon: %v", err)
	}
}
