package gardenapi

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
)

// PrivateDrafts is optional: a Backend that does not implement it keeps the
// legacy GitHub capture form. Every call returns drafts.ErrUnavailable when
// the store cannot be used. Saves are revision-checked: see drafts.Store.
type PrivateDrafts interface {
	ListPrivateDrafts(context.Context, garden.RepoID, bool) ([]drafts.Draft, error)
	ReadPrivateDraft(context.Context, garden.RepoID, drafts.ID) (drafts.Draft, error)
	SavePrivateDraft(context.Context, garden.RepoID, drafts.Draft, uint64) (drafts.Draft, error)
	ArchivePrivateDraft(context.Context, garden.RepoID, drafts.ID, uint64) (drafts.Draft, error)
	UnarchivePrivateDraft(context.Context, garden.RepoID, drafts.ID, uint64) (drafts.Draft, error)
}
