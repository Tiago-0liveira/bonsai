package gardenbackend

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
)

func TestPrivateDraftsOptional(t *testing.T) {
	b := &Backend{local: "clone-a"}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"list", func() error { _, err := b.ListPrivateDrafts(ctx, 12, false); return err }},
		{"read", func() error { _, err := b.ReadPrivateDraft(ctx, 12, "local"); return err }},
		{"save", func() error { _, err := b.SavePrivateDraft(ctx, 12, drafts.Draft{}, 0); return err }},
		{"archive", func() error { _, err := b.ArchivePrivateDraft(ctx, 12, "local", 1); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, drafts.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivateDraftBackendBindsCloneWithoutRemoteCalls(t *testing.T) {
	ctx := context.Background()
	store, err := drafts.New(filepath.Join(t.TempDir(), "private", "drafts.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var opts options
	WithPrivateDrafts(store)(&opts)
	if opts.drafts != store {
		t.Fatal("option lost injected store")
	}
	// No remote, authentication or managed-draft dependencies are installed.
	a := &Backend{local: "clone-a", deps: Deps{PrivateDrafts: store}}
	b := &Backend{local: "clone-b", deps: Deps{PrivateDrafts: store}}
	d, err := a.SavePrivateDraft(ctx, 12, drafts.Draft{Title: "Private"}, 0)
	if err != nil || d.Scope.Local != a.local {
		t.Fatalf("save: %+v, %v", d, err)
	}
	for _, tc := range []struct {
		name    string
		backend *Backend
		repo    garden.RepoID
	}{
		{"other clone", b, 12}, {"other remote", a, 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list, err := tc.backend.ListPrivateDrafts(ctx, tc.repo, true)
			if err != nil || len(list) != 0 {
				t.Fatalf("list: %+v, %v", list, err)
			}
			if _, err := tc.backend.ReadPrivateDraft(ctx, tc.repo, d.ID); !errors.Is(err, drafts.ErrNotFound) {
				t.Fatalf("read: %v", err)
			}
			if _, err := tc.backend.SavePrivateDraft(ctx, tc.repo, d, d.Revision); !errors.Is(err, drafts.ErrInvalid) {
				t.Fatalf("save: %v", err)
			}
			if _, err := tc.backend.ArchivePrivateDraft(ctx, tc.repo, d.ID, d.Revision); !errors.Is(err, drafts.ErrNotFound) {
				t.Fatalf("archive: %v", err)
			}
		})
	}
	d, err = a.ArchivePrivateDraft(ctx, 12, d.ID, d.Revision)
	if err != nil || d.ArchivedAt.IsZero() {
		t.Fatalf("archive: %+v, %v", d, err)
	}
	got, err := a.ReadPrivateDraft(ctx, 12, d.ID)
	if err != nil || got.Title != "Private" {
		t.Fatalf("read archived: %+v, %v", got, err)
	}
}
