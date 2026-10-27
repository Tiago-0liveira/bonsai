package gardenbackend

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// PrivateDraftStore is optional and independent of managed GitHub drafts.
type PrivateDraftStore interface {
	List(context.Context, drafts.Scope, bool) ([]drafts.Draft, error)
	Read(context.Context, drafts.Scope, drafts.ID) (drafts.Draft, error)
	Save(context.Context, drafts.Scope, drafts.Draft, uint64) (drafts.Draft, error)
	Archive(context.Context, drafts.Scope, drafts.ID, uint64) (drafts.Draft, error)
	Unarchive(context.Context, drafts.Scope, drafts.ID, uint64) (drafts.Draft, error)
}

// PrivateDraftBackend can be adopted without expanding the existing UI contract.
// Repository IDs come from the connection; the backend always binds the clone.
type PrivateDraftBackend interface {
	ListPrivateDrafts(context.Context, garden.RepoID, bool) ([]drafts.Draft, error)
	ReadPrivateDraft(context.Context, garden.RepoID, drafts.ID) (drafts.Draft, error)
	SavePrivateDraft(context.Context, garden.RepoID, drafts.Draft, uint64) (drafts.Draft, error)
	ArchivePrivateDraft(context.Context, garden.RepoID, drafts.ID, uint64) (drafts.Draft, error)
	UnarchivePrivateDraft(context.Context, garden.RepoID, drafts.ID, uint64) (drafts.Draft, error)
}

var _ PrivateDraftStore = (*drafts.Store)(nil)
var _ PrivateDraftBackend = (*Backend)(nil)
var _ gardenapi.PrivateDrafts = (*Backend)(nil)

func WithPrivateDrafts(s PrivateDraftStore) Option {
	return func(o *options) { o.drafts = s }
}

func (b *Backend) ListPrivateDrafts(ctx context.Context, repo garden.RepoID, includeInactive bool) ([]drafts.Draft, error) {
	if b.deps.PrivateDrafts == nil {
		return nil, drafts.ErrUnavailable
	}
	return b.deps.PrivateDrafts.List(ctx, drafts.Scope{Local: b.local, Repo: repo}, includeInactive)
}

func (b *Backend) ReadPrivateDraft(ctx context.Context, repo garden.RepoID, id drafts.ID) (drafts.Draft, error) {
	if b.deps.PrivateDrafts == nil {
		return drafts.Draft{}, drafts.ErrUnavailable
	}
	return b.deps.PrivateDrafts.Read(ctx, drafts.Scope{Local: b.local, Repo: repo}, id)
}

func (b *Backend) SavePrivateDraft(ctx context.Context, repo garden.RepoID, d drafts.Draft, expected uint64) (drafts.Draft, error) {
	if b.deps.PrivateDrafts == nil {
		return drafts.Draft{}, drafts.ErrUnavailable
	}
	return b.deps.PrivateDrafts.Save(ctx, drafts.Scope{Local: b.local, Repo: repo}, d, expected)
}

func (b *Backend) ArchivePrivateDraft(ctx context.Context, repo garden.RepoID, id drafts.ID, expected uint64) (drafts.Draft, error) {
	if b.deps.PrivateDrafts == nil {
		return drafts.Draft{}, drafts.ErrUnavailable
	}
	return b.deps.PrivateDrafts.Archive(ctx, drafts.Scope{Local: b.local, Repo: repo}, id, expected)
}

func (b *Backend) UnarchivePrivateDraft(ctx context.Context, repo garden.RepoID, id drafts.ID, expected uint64) (drafts.Draft, error) {
	if b.deps.PrivateDrafts == nil {
		return drafts.Draft{}, drafts.ErrUnavailable
	}
	return b.deps.PrivateDrafts.Unarchive(ctx, drafts.Scope{Local: b.local, Repo: repo}, id, expected)
}
