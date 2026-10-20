package work

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

func draftItem(id garden.IssueID, number int64, title string, typ garden.ItemType, extra ...string) gardensync.Item {
	labels := append([]string{garden.ManagedLabel, garden.FormatTypeLabel(typ), garden.FormatStageLabel(garden.StageDraft)}, extra...)
	meta, _ := garden.EncodeMetadata(garden.Metadata{Provenance: garden.ProvenanceCapture, Owner: "1"})
	issue := garden.Issue{Repo: 9, ID: id, Number: number, Title: title, Labels: labels, Metadata: meta}
	return gardensync.Item{Issue: issue, Classification: garden.Classify(labels), Verification: garden.Verify(garden.IssueIdentity{Repo: 9, Issue: id}, nil, garden.ApprovalPolicy{})}
}

func TestDraftsIncludeEveryTypeAndExcludeOtherStagesAndUnusableItems(t *testing.T) {
	ready := draftItem(5, 5, "Ready one", garden.TypeBug)
	ready.Issue.Labels = []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageReady)}
	noID := draftItem(0, 6, "No id", garden.TypeBug)
	deleted := draftItem(7, 7, "Gone", garden.TypeChore)
	deleted.Deleted = true
	items := []gardensync.Item{
		draftItem(3, 3, "Research the thing", garden.TypeResearch),
		draftItem(1, 1, "Add dark mode", garden.TypeFeature),
		draftItem(2, 2, "Crash on start", garden.TypeBug),
		draftItem(4, 4, "Tidy up", garden.TypeChore),
		ready, noID, deleted,
	}
	c := Drafts(items, Query{})
	if c.Total != 4 || len(c.Records) != 4 || c.Excluded != 3 {
		t.Fatalf("total %d, records %d, excluded %d", c.Total, len(c.Records), c.Excluded)
	}
	for i, want := range []garden.IssueID{1, 2, 3, 4} {
		if c.Records[i].Item.Issue.ID != want || c.Records[i].Attention != "" {
			t.Errorf("row %d = %d (%q)", i, c.Records[i].Item.Issue.ID, c.Records[i].Attention)
		}
	}
	if sel := Select(items, Query{}); len(sel.Records) != 0 {
		t.Fatalf("drafts leaked into Board/Table: %d", len(sel.Records))
	}
}

func TestDraftsQueryMatchesNumberTitleAndType(t *testing.T) {
	items := []gardensync.Item{draftItem(1, 11, "Add dark mode", garden.TypeFeature), draftItem(2, 12, "Crash", garden.TypeBug)}
	for name, tc := range map[string]struct {
		q    Query
		want int
	}{
		"text":        {Query{Text: "DARK"}, 1},
		"number":      {Query{Text: "#12"}, 1},
		"type":        {Query{Type: garden.TypeBug}, 1},
		"no match":    {Query{Text: "zzz"}, 0},
		"both":        {Query{Text: "dark", Type: garden.TypeBug}, 0},
		"priority is": {Query{Priority: "P0"}, 2},
	} {
		c := Drafts(items, tc.q)
		if len(c.Records) != tc.want || c.Total != 2 {
			t.Errorf("%s: %d rows, total %d", name, len(c.Records), c.Total)
		}
	}
}

func TestDraftsKeepConflictedItemsVisibleAsAttention(t *testing.T) {
	conflicting := draftItem(1, 1, "Two stages", garden.TypeBug, garden.FormatStageLabel(garden.StageReady))
	badMeta := draftItem(2, 2, "Bad metadata", garden.TypeBug)
	badMeta.Issue.Metadata = []byte(`{"schema_version":9}`)
	heads := draftItem(3, 3, "Competing heads", garden.TypeBug)
	heads.Verification.ConflictingHeads = []garden.Revision{{Hash: "a"}, {Hash: "b"}}
	heads.Problems = []gardensync.Problem{{Code: "x", Message: "Issue revision or approval needs attention"}}
	accepted := draftItem(4, 4, "Accepted but labelled draft", garden.TypeBug)
	hash := garden.ContentHash("spec")
	accepted.Issue.Metadata, _ = garden.EncodeMetadata(garden.Metadata{Provenance: "x", Owner: "1", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash})
	c := Drafts([]gardensync.Item{conflicting, badMeta, heads, accepted}, Query{})
	if len(c.Records) != 4 {
		t.Fatalf("rows = %d", len(c.Records))
	}
	for _, d := range c.Records {
		if d.Attention == "" {
			t.Errorf("#%d has no attention reason", d.Item.Issue.Number)
		}
	}
	if _, i, ok := c.Find(3); !ok || i != 2 {
		t.Fatalf("Find = %d, %v", i, ok)
	}
	if _, _, ok := c.Find(99); ok {
		t.Fatal("found a missing draft")
	}
}
