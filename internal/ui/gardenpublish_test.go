package ui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// pubBackend scripts the publisher on top of the real private store, so what the UI shows after a reply is
// what a later list read returns.
type pubBackend struct {
	*privBackend

	mu         sync.Mutex
	previews   int
	publishes  int
	resumes    int
	previewErr error
	outcome    gardenapi.PublishView // returned by Publish
	outErr     error
	resume     gardenapi.PublishView // returned by ResumePublish
	resumeErr  error
	gate       chan struct{} // Publish waits for it when set
}

var _ gardenapi.PrivatePublisher = (*pubBackend)(nil)

var testIssue = &drafts.IssueIdentity{ID: 5, Number: 12, URL: "https://github.com/acme/my-app/issues/12"}

func (b *pubBackend) PreviewPublish(ctx context.Context, repo garden.RepoID, id drafts.ID) (gardenapi.PublishPreviewView, error) {
	b.mu.Lock()
	b.previews++
	err := b.previewErr
	b.mu.Unlock()
	if err != nil {
		return gardenapi.PublishPreviewView{}, err
	}
	d, err := b.store.Read(ctx, b.scope, id)
	if err != nil {
		return gardenapi.PublishPreviewView{}, err
	}
	labels := []string{garden.ManagedLabel, garden.FormatTypeLabel(d.Type), garden.FormatStageLabel(garden.StageDraft)}
	return gardenapi.PublishPreviewView{RepoID: uint64(repo), RepoFullName: "acme/my-app", ActorID: "1001", ActorLogin: "octo-dev", DraftID: d.ID,
		DraftLabel: drafts.DraftLabel(d.ID), DraftRevision: d.Revision, Title: d.Title, Type: d.Type, Description: d.Description, SpecText: spec.Render(d.Spec),
		Labels: labels, ContentHash: "h", OperationID: "publish-test"}, nil
}

// settle moves the stored draft where a real publication would have left it.
func (b *pubBackend) settle(ctx context.Context, id drafts.ID, state drafts.PublicationState) {
	d, err := b.store.Read(ctx, b.scope, id)
	if err != nil {
		return
	}
	switch state {
	case drafts.Published:
		d.Publication = drafts.Publication{State: drafts.Publishing, OperationID: "publish-test", Issue: testIssue}
		d, _ = b.store.Save(ctx, b.scope, d, d.Revision)
		d.Publication = drafts.Publication{State: drafts.Published, OperationID: "publish-test", Issue: testIssue, VerifiedAt: time.Now()}
	case drafts.Publishing:
		d.Publication = drafts.Publication{State: drafts.Publishing, OperationID: "publish-test", Issue: testIssue}
	}
	_, _ = b.store.Save(ctx, b.scope, d, d.Revision)
}

func (b *pubBackend) stateFor(v gardenapi.PublishView, id drafts.ID) {
	switch v.State {
	case gardenapi.PublishDone:
		b.settle(context.Background(), id, drafts.Published)
	case gardenapi.PublishIncomplete, gardenapi.PublishUnknown:
		b.settle(context.Background(), id, drafts.Publishing)
	}
}

func (b *pubBackend) Publish(_ context.Context, _ garden.RepoID, v gardenapi.PublishPreviewView) (gardenapi.PublishView, error) {
	b.mu.Lock()
	b.publishes++
	gate, out, err := b.gate, b.outcome, b.outErr
	b.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if err == nil {
		b.stateFor(out, v.DraftID)
	}
	return out, err
}

func (b *pubBackend) ResumePublish(_ context.Context, _ garden.RepoID, id drafts.ID) (gardenapi.PublishView, error) {
	b.mu.Lock()
	b.resumes++
	out, err := b.resume, b.resumeErr
	b.mu.Unlock()
	if err == nil {
		b.stateFor(out, id)
	}
	return out, err
}

func (e privEnv) openAPI(t *testing.T, api gardenapi.Backend) Model {
	t.Helper()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	m := sized(t, New(e.repo, &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(api)), 120, 35)
	m, _ = drainAll(t, m, m.Init())
	m = pressAll(t, m, "enter")
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Drafts did not open:\n%s", view(m))
	}
	return m
}

func publishDraftEnv(t *testing.T) (privEnv, *pubBackend, drafts.Draft) {
	t.Helper()
	e := newPrivEnv(t)
	pb := &pubBackend{privBackend: e.backend(t)}
	d, err := pb.store.Save(context.Background(), pb.scope, drafts.Draft{Title: "Keep sessions", Type: garden.TypeFeature, Description: "Sessions drop.",
		Spec: spec.Spec{Summary: "Keep sessions.", CurrentBehavior: "They drop.", DesiredBehavior: "They stay.", Requirements: "- keep", AcceptanceCriteria: "- survive"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return e, pb, d
}

// toReview opens the only local draft and goes to its review.
func toReview(t *testing.T, m Model) Model {
	t.Helper()
	m = pk(t, m, "enter")
	if p := m.garden.drafts.private; p == nil || p.stage != privEditor {
		t.Fatalf("draft did not open:\n%s", view(m))
	}
	return pk(t, m, "ctrl+r")
}

func TestPublishConfirmationThenPublished(t *testing.T) {
	e, pb, d := publishDraftEnv(t)
	pb.outcome = gardenapi.PublishView{State: gardenapi.PublishDone, IssueNumber: 12, IssueURL: testIssue.URL}
	m := toReview(t, e.openAPI(t, pb))
	m = pk(t, m, "enter")
	out := view(m)
	mustContain(t, "confirmation", out, "APPROVE SPECIFICATION & CREATE ISSUE", "acme/my-app", "@octo-dev", drafts.DraftLabel(d.ID), "revision 1", "Keep sessions",
		"bonsai:stage/ready", "bonsai:type/feature", "Approve & create issue (Enter)", "Back to draft (Esc)", "Keep sessions.")
	if pb.previews != 1 || pb.publishes != 0 {
		t.Fatalf("previews %d publishes %d", pb.previews, pb.publishes)
	}
	m = pk(t, m, "enter")
	if pb.publishes != 1 {
		t.Fatalf("publishes = %d", pb.publishes)
	}
	if m.garden.drafts.private != nil || m.garden.drafts.stage != draftsList {
		t.Fatalf("must return to the inbox:\n%s", view(m))
	}
	mustContain(t, "inbox", view(m), "Published as #12")
	if len(m.garden.drafts.locals()) != 0 {
		t.Fatalf("published draft is still listed: %+v", m.garden.drafts.locals())
	}
}

func TestPublishBackReturnsToEditorWithoutPublishing(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	m := toReview(t, e.openAPI(t, pb))
	m = pk(t, pk(t, m, "enter"), "esc")
	if p := m.garden.drafts.private; p.stage != privEditor || p.pub != nil {
		t.Fatalf("stage %d", p.stage)
	}
	if pb.publishes != 0 {
		t.Fatal("published")
	}
}

func TestPublishPreviewIsDroppedByEditsAndNewerRevisions(t *testing.T) {
	t.Run("local edit", func(t *testing.T) {
		e, pb, _ := publishDraftEnv(t)
		m := pk(t, toReview(t, e.openAPI(t, pb)), "enter")
		p := m.garden.drafts.private
		m, _ = m.privEdited(p)
		if p.stage != privReview || p.pub != nil || !strings.Contains(p.notice, "review it again") {
			t.Fatalf("stage %d pub %v notice %q", p.stage, p.pub, p.notice)
		}
		if _, cmd := m.privPublishKey(p, "enter"); cmd != nil || pb.publishes != 0 {
			t.Fatal("confirmed a dropped preview")
		}
	})
	t.Run("newer revision from the list", func(t *testing.T) {
		e, pb, d := publishDraftEnv(t)
		m := pk(t, toReview(t, e.openAPI(t, pb)), "enter")
		edited := d
		edited.Title = "Changed elsewhere"
		if _, err := pb.store.Save(context.Background(), pb.scope, edited, d.Revision); err != nil {
			t.Fatal(err)
		}
		list, err := pb.store.List(context.Background(), pb.scope, false)
		if err != nil {
			t.Fatal(err)
		}
		m, _ = send(t, m, privListMsg{repo: m.privRepo(), seq: m.garden.drafts.privSeq, drafts: list})
		p := m.garden.drafts.private
		if p.pub != nil || p.stage != privReview || p.notice == "" {
			t.Fatalf("stage %d pub %v", p.stage, p.pub)
		}
		m = pk(t, m, "enter")
		if m.garden.drafts.private.stage != privPublish || m.garden.drafts.private.pub.view.Title != "Changed elsewhere" {
			t.Fatalf("re-preview did not use the new revision:\n%s", view(m))
		}
	})
}

func TestPublishDuplicateEnterIsIgnoredWhileRunning(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	pb.gate = make(chan struct{})
	pb.outcome = gardenapi.PublishView{State: gardenapi.PublishDone, IssueNumber: 12, IssueURL: testIssue.URL}
	m := pk(t, toReview(t, e.openAPI(t, pb)), "enter")
	m, cmd := send(t, m, pkey("enter"))
	if cmd == nil || !m.garden.drafts.private.pub.running {
		t.Fatal("first enter must start the publication")
	}
	mustContain(t, "busy", view(m), "Publishing… keys are ignored")
	for _, k := range []string{"enter", "esc", "r"} {
		var again tea.Cmd
		m, again = send(t, m, pkey(k))
		if again != nil {
			t.Fatalf("%s started something while running", k)
		}
	}
	if m.garden.drafts.private == nil || m.garden.drafts.private.stage != privPublish {
		t.Fatal("esc left the screen while publishing")
	}
	close(pb.gate)
	m, _ = drainAll(t, m, cmd)
	if pb.publishes != 1 {
		t.Fatalf("publishes = %d", pb.publishes)
	}
	mustContain(t, "done", view(m), "Published as #12")
}

func TestPublishIncompleteShowsLinkAndResumes(t *testing.T) {
	e, pb, d := publishDraftEnv(t)
	pb.outcome = gardenapi.PublishView{State: gardenapi.PublishIncomplete, Step: "approval", IssueNumber: 12, IssueURL: testIssue.URL,
		Err: &gardenapi.Error{Code: gardenapi.CodePublishIncomplete, Message: "Publishing is incomplete: the issue exists but its approval is not verified."}}
	pb.resume = gardenapi.PublishView{State: gardenapi.PublishDone, IssueNumber: 12, IssueURL: testIssue.URL}
	m := pk(t, toReview(t, e.openAPI(t, pb)), "enter", "enter")
	mustContain(t, "incomplete", view(m), "PUBLISHING INCOMPLETE — #12", testIssue.URL, "its approval is not verified", "Resume publishing (Enter)")
	if strings.Contains(view(m), "Private") || strings.Contains(view(m), "Published as") {
		t.Fatalf("must not read as private or published:\n%s", view(m))
	}
	// The inbox says so too, and opening the draft offers resume instead of editing.
	m = pk(t, m, "esc")
	mustContain(t, "inbox", view(m), "Publishing incomplete")
	m = pk(t, m, "enter")
	if p := m.garden.drafts.private; p == nil || p.stage != privPublish {
		t.Fatalf("opened the editor of a publishing draft:\n%s", view(m))
	}
	m = pk(t, m, "enter")
	if pb.resumes != 1 || pb.publishes != 1 {
		t.Fatalf("resumes %d publishes %d", pb.resumes, pb.publishes)
	}
	mustContain(t, "done", view(m), "Published as #12")
	if got, _ := pb.store.Read(context.Background(), pb.scope, d.ID); got.Publication.State != drafts.Published {
		t.Fatalf("publication = %+v", got.Publication)
	}
}

func TestPublishUnknownChecksGitHubOnResume(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	pb.outcome = gardenapi.PublishView{State: gardenapi.PublishUnknown, Step: "issue",
		Err: &gardenapi.Error{Code: gardenapi.CodePublishUnknown, Message: "The issue may already exist on GitHub."}}
	pb.resume = gardenapi.PublishView{State: gardenapi.PublishDone, IssueNumber: 12, IssueURL: testIssue.URL}
	m := pk(t, toReview(t, e.openAPI(t, pb)), "enter", "enter")
	mustContain(t, "unknown", view(m), "Checking GitHub for the issue…", "Resume publishing (Enter)")
	m = pk(t, m, "r")
	if pb.resumes != 1 {
		t.Fatalf("resumes = %d", pb.resumes)
	}
	mustContain(t, "done", view(m), "Published as #12")
}

func TestPublishFailedAndStaleReturnToReview(t *testing.T) {
	for name, tc := range map[string]struct {
		view gardenapi.PublishView
		err  error
		want string
	}{
		"failed": {view: gardenapi.PublishView{State: gardenapi.PublishFailed, Err: &gardenapi.Error{Code: gardenapi.CodeValidation, Message: "GitHub rejected the request."}}, want: "Nothing was created on GitHub"},
		"stale":  {err: &gardenapi.Error{Code: gardenapi.CodePublishStale, Message: "changed"}, want: "Review it again"},
	} {
		t.Run(name, func(t *testing.T) {
			e, pb, _ := publishDraftEnv(t)
			pb.outcome, pb.outErr = tc.view, tc.err
			m := pk(t, toReview(t, e.openAPI(t, pb)), "enter", "enter")
			p := m.garden.drafts.private
			if p == nil || p.stage != privReview || p.pub != nil || !strings.Contains(p.notice, tc.want) {
				t.Fatalf("stage %v notice %q", p, p.notice)
			}
			mustContain(t, "review", view(m), "REVIEW & CREATE ISSUE", tc.want)
		})
	}
}

func TestPublishPreviewErrorsStayOnTheReview(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	pb.previewErr = &gardenapi.Error{Code: gardenapi.CodePublishNotReady, Message: "This draft is not ready to publish.", Next: "Fill the missing parts."}
	m := pk(t, toReview(t, e.openAPI(t, pb)), "enter")
	if p := m.garden.drafts.private; p.stage != privReview || !strings.Contains(p.notice, "not ready to publish") {
		t.Fatalf("stage %d notice %q", p.stage, p.notice)
	}
	if pb.publishes != 0 {
		t.Fatal("published")
	}
}

func TestPublishUnsavedTextIsSavedBeforeThePreview(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	m := e.openAPI(t, pb)
	m = pk(t, m, "enter", "ctrl+t")
	m = ptype(t, m, " now\n")
	m = pk(t, m, "ctrl+r", "enter")
	p := m.garden.drafts.private
	if p.stage != privPublish || p.pub.view.Title != "Keep sessions now" || p.pub.view.DraftRevision != p.draft.Revision {
		t.Fatalf("preview did not use the saved text: %+v", p.pub)
	}
}

func TestPublishLockedDraftCannotBeEdited(t *testing.T) {
	e, pb, d := publishDraftEnv(t)
	pb.settle(context.Background(), d.ID, drafts.Publishing)
	pb.resume = gardenapi.PublishView{State: gardenapi.PublishFailed, Err: &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "no"}}
	m := e.openAPI(t, pb)
	mustContain(t, "inbox", view(m), "Publishing incomplete")
	m = pk(t, m, "enter")
	p := m.garden.drafts.private
	if p.stage != privPublish {
		t.Fatal("editor opened")
	}
	before := p.draft.Title
	m = ptype(t, m, "xyz")
	if m.garden.drafts.private.draft.Title != before || m.garden.drafts.private.gen != 0 {
		t.Fatal("typed into a publishing draft")
	}
}

func TestPublishPreviewUnavailableWithoutPublisherKeepsOldNotice(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	if _, err := pb.store.Save(context.Background(), pb.scope, drafts.Draft{Title: "x", Type: garden.TypeBug, Spec: spec.Spec{CurrentBehavior: "a"}}, 0); err != nil {
		t.Fatal(err)
	}
	m := toReview(t, e.open(t, pb, 120, 35))
	m = pk(t, m, "enter")
	if p := m.garden.drafts.private; p.stage != privReview || p.notice != privPublishUnavailable {
		t.Fatalf("stage %d notice %q", p.stage, p.notice)
	}
}

func TestPublishUnclassifiedErrorOffersResumeInsteadOfNothingStarted(t *testing.T) {
	e, pb, _ := publishDraftEnv(t)
	pb.outErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub could not be reached."}
	pb.resume = gardenapi.PublishView{State: gardenapi.PublishDone, IssueNumber: 12, IssueURL: testIssue.URL}
	m := pk(t, toReview(t, e.openAPI(t, pb)), "enter", "enter")
	p := m.garden.drafts.private
	if p.stage != privPublish || p.pub == nil || p.pub.result == nil {
		t.Fatalf("stage %d:\n%s", p.stage, view(m))
	}
	mustContain(t, "resume", view(m), "Resume publishing (Enter)", "GitHub could not be reached.")
	if strings.Contains(view(m), "Nothing was created") {
		t.Fatal("claimed nothing started")
	}
	m = pk(t, m, "enter")
	if pb.resumes != 1 {
		t.Fatalf("resumes = %d", pb.resumes)
	}
	mustContain(t, "done", view(m), "Published as #12")
}

func TestPublicationStatusNeverShowsIssueZero(t *testing.T) {
	p := &privateDraft{pub: &privPub{result: &gardenapi.PublishView{State: gardenapi.PublishIncomplete}}}
	title, lines, resume := p.publicationStatus()
	if strings.Contains(title+strings.Join(lines, " "), "#0") || !resume {
		t.Fatalf("%q %v", title, lines)
	}
}
