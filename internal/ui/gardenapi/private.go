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

// PrivatePublisher is optional on top of PrivateDrafts: a Backend without it keeps the review screen
// read-only. Publishing creates one Issue and records the draft's specification approval; see
// core/garden/publish.
type PrivatePublisher interface {
	// PreviewPublish rechecks the connection and Write access and reports exactly what would be published.
	PreviewPublish(context.Context, garden.RepoID, drafts.ID) (PublishPreviewView, error)
	// Publish confirms a preview, which must be handed back unchanged.
	Publish(context.Context, garden.RepoID, PublishPreviewView) (PublishView, error)
	// ResumePublish continues a publication the draft is stuck in. It never starts a new one.
	ResumePublish(context.Context, garden.RepoID, drafts.ID) (PublishView, error)
}

// PublishPreviewView is what the confirmation shows. The identity fields only travel back to Publish.
type PublishPreviewView struct {
	RepoID        uint64
	RepoFullName  string
	ActorID       string
	ActorLogin    string
	DraftID       drafts.ID
	DraftLabel    string
	DraftRevision uint64
	Title         string
	Type          garden.ItemType
	Description   string
	SpecText      string
	Labels        []string
	ContentHash   string
	OperationID   string
}

// PublishState is how a started publication ended.
type PublishState string

const (
	PublishDone       PublishState = "published"
	PublishIncomplete PublishState = "incomplete"
	PublishUnknown    PublishState = "unknown"
	PublishFailed     PublishState = "failed"
)

// PublishView is the outcome of a started publication. A failed one created nothing.
type PublishView struct {
	State       PublishState
	Step        string
	IssueNumber int64
	IssueURL    string
	Err         *Error
}
