// Package drafts stores intentional private authoring content, independently of
// managed GitHub drafts. It has no credentials, agent sessions or remote writer.
package drafts

import (
	"slices"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

type ID string

// Scope includes the remote target so reassociating a clone cannot mix inboxes.
type Scope struct {
	Local garden.LocalRepoID `json:"local_repo_id"`
	Repo  garden.RepoID      `json:"repo_id"`
}

type Answer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type PublicationState string

const (
	Unpublished PublicationState = "unpublished"
	Publishing  PublicationState = "publishing"
	Published   PublicationState = "published"
)

type IssueIdentity struct {
	ID     garden.IssueID `json:"id"`
	Number int64          `json:"number"`
	URL    string         `json:"url"`
}

// Publication is durable intent and identity, never live request progress.
// VerifiedAt is supplied only after the caller has verified remote success.
type Publication struct {
	State       PublicationState `json:"state"`
	OperationID string           `json:"operation_id,omitempty"`
	Issue       *IssueIdentity   `json:"issue,omitempty"`
	VerifiedAt  time.Time        `json:"verified_at,omitempty"`
}

type Draft struct {
	ID               ID              `json:"id"`
	Scope            Scope           `json:"scope"`
	Revision         uint64          `json:"revision"`
	Description      string          `json:"description"`
	Title            string          `json:"title"`
	Type             garden.ItemType `json:"type"`
	Spec             spec.Spec       `json:"spec"`
	ConfirmedAnswers []Answer        `json:"confirmed_answers,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	ArchivedAt       time.Time       `json:"archived_at,omitempty"`
	Publication      Publication     `json:"publication"`
	// ConflictOf makes a rejected edit discoverable after a restart.
	ConflictOf ID `json:"conflict_of,omitempty"`
}

func (s Scope) valid() bool { return s.Local != "" && s.Repo != 0 }

func (p Publication) valid() bool {
	if p.Issue != nil && (p.Issue.ID == 0 || p.Issue.Number <= 0 || p.Issue.URL == "") {
		return false
	}
	switch p.State {
	case Unpublished:
		return p.OperationID == "" && p.Issue == nil && p.VerifiedAt.IsZero()
	case Publishing:
		return p.OperationID != "" && p.VerifiedAt.IsZero()
	case Published:
		return p.OperationID != "" && p.Issue != nil && !p.VerifiedAt.IsZero()
	}
	return false
}

func (d Draft) valid() bool {
	texts := []string{string(d.ID), string(d.Scope.Local), d.Description, d.Title, string(d.Type), string(d.ConflictOf), d.Publication.OperationID}
	for i := 0; i < spec.FixedCount; i++ {
		texts = append(texts, d.Spec.Text(i))
	}
	for _, extra := range d.Spec.Extra {
		texts = append(texts, extra.Title, extra.Text)
	}
	for _, answer := range d.ConfirmedAnswers {
		texts = append(texts, answer.Question, answer.Answer)
	}
	if d.Publication.Issue != nil {
		texts = append(texts, d.Publication.Issue.URL)
	}
	for _, text := range texts {
		if !utf8.ValidString(text) {
			return false
		}
	}
	return d.ID != "" && d.Scope.valid() && d.Revision > 0 &&
		!d.CreatedAt.IsZero() && !d.UpdatedAt.Before(d.CreatedAt) &&
		d.Publication.valid() && (d.ArchivedAt.IsZero() || d.Publication.State != Publishing)
}

func transition(cur, next Publication) bool {
	if !next.valid() {
		return false
	}
	if cur.State == Unpublished {
		return next.State != Published
	}
	// Without a retained Issue identity, a publication verified as never created
	// may return to Unpublished so the draft can be retried or archived.
	if next.State == Unpublished {
		return cur.Issue == nil
	}
	if cur.OperationID != next.OperationID {
		return false
	}
	if cur.Issue != nil && (next.Issue == nil || *cur.Issue != *next.Issue) {
		return false
	}
	if cur.State == Published {
		return cur.State == next.State && cur.VerifiedAt.Equal(next.VerifiedAt)
	}
	return true
}

// sameContent reports whether two drafts hold the same authored text, so a
// verified publication cannot stay attached to edited content.
func sameContent(a, b Draft) bool {
	return a.Description == b.Description && a.Title == b.Title && a.Type == b.Type &&
		a.Spec.Summary == b.Spec.Summary && a.Spec.CurrentBehavior == b.Spec.CurrentBehavior &&
		a.Spec.DesiredBehavior == b.Spec.DesiredBehavior && a.Spec.Requirements == b.Spec.Requirements &&
		a.Spec.AcceptanceCriteria == b.Spec.AcceptanceCriteria && a.Spec.Constraints == b.Spec.Constraints &&
		a.Spec.OpenQuestions == b.Spec.OpenQuestions && slices.Equal(a.Spec.Extra, b.Spec.Extra) &&
		slices.Equal(a.ConfirmedAnswers, b.ConfirmedAnswers)
}
