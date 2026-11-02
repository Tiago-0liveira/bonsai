package work

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

func accepted(t *testing.T, id garden.IssueID, stage garden.Stage, plan bool) gardensync.Item {
	t.Helper()
	spec := "Verified specification"
	hash := garden.ContentHash(spec)
	meta, err := garden.EncodeMetadata(garden.Metadata{SchemaVersion: 1, Provenance: "test", Owner: "1001", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash})
	if err != nil {
		t.Fatal(err)
	}
	var comments []garden.Comment
	add := func(n int64, e garden.CommentEnvelope, body string) {
		t.Helper()
		e.SchemaVersion = 1
		e.RepoID = 7
		e.IssueID = id
		raw, err := garden.EncodeCommentEnvelope(e, body)
		if err != nil {
			t.Fatal(err)
		}
		comments = append(comments, garden.Comment{ID: n, AuthorID: "1001", Body: raw})
	}
	add(1, garden.CommentEnvelope{Kind: garden.CommentRevision, RevisionKind: garden.RevisionSpec, ContentHash: hash}, spec)
	add(2, garden.CommentEnvelope{Kind: garden.CommentApproval, RevisionHash: hash, RevisionCommentID: 1, SourceSpecFingerprint: hash}, "Approved")
	if plan {
		ph := garden.ContentHash("Plan")
		add(3, garden.CommentEnvelope{Kind: garden.CommentRevision, RevisionKind: garden.RevisionPlan, ContentHash: ph, SourceSpecFingerprint: hash}, "Plan")
	}
	issue := garden.Issue{Repo: 7, ID: id, Number: int64(id), Title: fmt.Sprintf("Task %d 界 é", id), Metadata: meta, Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(stage), "priority:p1"}}
	v := garden.Verify(garden.IssueIdentity{Repo: 7, Issue: id}, comments, garden.ApprovalPolicy{AllowedActorIDs: []string{"1001"}})
	c := garden.Classify(issue.Labels)
	lane := garden.Lane(c, mustMeta(t, meta), v, nil)
	if lane.Lane != garden.LaneWork {
		t.Fatalf("fixture lane: %+v / %+v", lane, v)
	}
	return gardensync.Item{Issue: issue, Classification: c, Verification: v, Lane: lane}
}
func mustMeta(t *testing.T, b []byte) garden.Metadata {
	t.Helper()
	m, err := garden.DecodeMetadata(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestCanonicalEligibilityAndIndependentType(t *testing.T) {
	base := accepted(t, 1, garden.StageReady, false)
	for _, tc := range []struct {
		name   string
		change func(*gardensync.Item)
		want   int
	}{
		{"accepted", func(*gardensync.Item) {}, 1},
		{"draft even with acceptance", func(i *gardensync.Item) { i.Issue.Labels[2] = garden.FormatStageLabel(garden.StageDraft) }, 0},
		{"missing metadata", func(i *gardensync.Item) { i.Issue.Metadata = nil }, 0},
		{"unverified ready", func(i *gardensync.Item) { i.Verification = garden.Verification{} }, 0},
		{"missing approval", func(i *gardensync.Item) { i.Verification.ValidApprovals = nil }, 0},
		{"revoked acceptance", func(i *gardensync.Item) { i.Verification.AcceptedSpecHash = "" }, 0},
		{"classification conflict", func(i *gardensync.Item) {
			i.Issue.Labels = append(i.Issue.Labels, garden.FormatStageLabel(garden.StageDone))
		}, 0},
		{"adapter data defect", func(i *gardensync.Item) { i.Issue.Problems = []string{garden.IssueProblemAuthor} }, 0},
		{"cached attention with valid acceptance", func(i *gardensync.Item) { i.Lane.Lane = garden.LaneAttention }, 1},
		{"deleted", func(i *gardensync.Item) { i.Deleted = true }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := base
			it.Issue.Labels = slices.Clone(base.Issue.Labels)
			tc.change(&it)
			got := Select([]gardensync.Item{it}, Query{}, nil)
			if len(got.Records) != tc.want {
				t.Fatalf("records=%d want %d", len(got.Records), tc.want)
			}
		})
	}
	for _, typ := range []garden.ItemType{garden.TypeFeature, garden.TypeBug, garden.TypeChore, garden.TypeResearch} {
		it := base
		it.Issue.Labels = slices.Clone(base.Issue.Labels)
		it.Issue.Labels[1] = garden.FormatTypeLabel(typ)
		c := Select([]gardensync.Item{it}, Query{Type: typ}, nil)
		if len(c.Columns[Approved]) != 1 || c.Records[0].Item.Classification.Type != typ {
			t.Fatalf("type %s: %+v", typ, c)
		}
	}
}
func TestGroupingAndFilters(t *testing.T) {
	for _, tc := range []struct {
		stage garden.Stage
		plan  bool
		want  Column
	}{
		{garden.StageReady, false, Approved}, {garden.StageReady, true, PlanReview},
		{garden.StageInProgress, false, Running}, {garden.StageReview, true, HumanReview}, {garden.StageDone, false, Done}, {garden.StageBlocked, false, Blocked},
	} {
		it := accepted(t, 1, tc.stage, tc.plan)
		got := Select([]gardensync.Item{it}, Query{}, nil)
		if len(got.Columns[tc.want]) != 1 {
			t.Fatalf("%+v placed in %+v", tc, got.Columns)
		}
	}
	items := []gardensync.Item{accepted(t, 3, garden.StageReady, false), accepted(t, 1, garden.StageReady, false), accepted(t, 2, garden.StageReady, false)}
	for _, tc := range []struct {
		q    Query
		want int
	}{{Query{}, 3}, {Query{Text: "TASK 1"}, 1}, {Query{Text: "#2"}, 1}, {Query{Text: "界"}, 3}, {Query{Type: garden.TypeBug}, 0}, {Query{Priority: "P1"}, 3}, {Query{Priority: "P0"}, 0}, {Query{Text: "missing"}, 0}, {Query{Text: "Task", Type: garden.TypeFeature, Priority: "P1"}, 3}} {
		got := Select(items, tc.q, nil)
		if len(got.Records) != tc.want || got.Total != 3 {
			t.Fatalf("query %+v: %d/%d", tc.q, len(got.Records), got.Total)
		}
	}
	got := Select(items, Query{}, nil)
	if got.Records[0].Item.Issue.ID != 1 || got.Records[2].Item.Issue.ID != 3 {
		t.Fatal("unstable order")
	}
	if len(got.Columns[Planning]) != 0 {
		t.Fatal("invented active planning")
	}
}
func TestSelectionEveryColumnScrollAndFallback(t *testing.T) {
	c := Collection{}
	for col := Column(0); col < ColumnCount; col++ {
		for row := 0; row < 5; row++ {
			c.Columns[col] = append(c.Columns[col], Record{Column: col, Item: gardensync.Item{Issue: garden.Issue{ID: garden.IssueID(int(col)*10 + row + 1)}}})
		}
	}
	s := Selection{Mode: ModeDetailed}
	s.Reconcile(c)
	for col := Column(0); col < ColumnCount; col++ {
		for row := 0; row < 5; row++ {
			if s.Column != col || s.ID != garden.IssueID(int(col)*10+row+1) {
				t.Fatalf("selection %+v", s)
			}
			s.EnsureVisible(c, 2, 4)
			if int(col) < s.Window || int(col) >= s.Window+4 || row < s.Scroll[col] || row >= s.Scroll[col]+2 {
				t.Fatalf("not visible %+v", s)
			}
			if row < 4 {
				s.Move(c, 0, 1)
			}
		}
		if col < ColumnCount-1 {
			s.Move(c, 1, 0)
		}
	}
	s.Move(c, -1, 0)
	if s.Rows[s.Column] != 4 || s.Scroll[s.Column] != 3 {
		t.Fatal("lost column cursor/scroll")
	}
	before := s
	c.Columns[s.Column] = slices.Clone(c.Columns[s.Column])
	slices.Reverse(c.Columns[s.Column])
	s.Reconcile(c)
	if s.ID != before.ID || s.Rows[s.Column] != 0 {
		t.Fatal("reorder lost identity")
	}
	c.Columns[s.Column] = c.Columns[s.Column][1:]
	if s.Reconcile(c) == "" || s.ID == before.ID {
		t.Fatal("deletion fallback missing")
	}
	col := s.Column
	c.Columns[col] = nil
	s.Reconcile(c)
	if s.ID == 0 || s.Column == col {
		t.Fatal("nearest column fallback missing")
	}
	s.Move(c, int(col)-int(s.Column), 0)
	if s.ID != 0 || s.Column != col {
		t.Fatal("empty column not navigable")
	}
	if s.Reconcile(c) != "" || s.Column != col {
		t.Fatal("refresh stole empty focus")
	}
	if s.Reconcile(Collection{}) != "" || s.ID != 0 {
		t.Fatal("empty refresh")
	}
}

func TestAcceptedDiagnosticsStayVisibleWithoutPromotingUnverifiedWork(t *testing.T) {
	item := accepted(t, 1, garden.StageReady, false)
	item.Lane.Lane = garden.LaneAttention
	item.Problems = []gardensync.Problem{{Code: "display-conflict", Message: "Display projection needs attention"}}
	got := Select([]gardensync.Item{item}, Query{}, nil)
	if len(got.Records) != 1 || got.Excluded != 0 || len(got.Records[0].Item.Problems) != 1 {
		t.Fatalf("accepted diagnostics were hidden: %+v", got)
	}
	item.Verification.ValidApprovals = nil
	if got := Select([]gardensync.Item{item}, Query{}, nil); len(got.Records) != 0 || got.Excluded != 1 {
		t.Fatal("diagnostics bypassed canonical acceptance")
	}
}

func TestSelectionWaitsForFirstNonEmptyLoad(t *testing.T) {
	s := Selection{Mode: ModeDetailed}
	for range 2 {
		s.Reconcile(Collection{})
	}
	s.Move(Collection{}, int(Blocked), 0)
	if s.Initialized {
		t.Fatal("empty navigation counted as selecting an item")
	}
	c := Collection{}
	c.Columns[Running] = []Record{{Column: Running, Item: gardensync.Item{Issue: garden.Issue{ID: 7}}}}
	s.Reconcile(c)
	if s.ID != 7 || s.Column != Running || !s.Initialized {
		t.Fatalf("first work not selected: %+v", s)
	}
	s.Move(c, 1, 0)
	s.Reconcile(c)
	if s.Column != Reviewing || s.ID != 0 {
		t.Fatal("refresh stole an intentionally empty column")
	}
}

func TestScrollClampsAfterCollectionShrinks(t *testing.T) {
	for _, count := range []int{0, 1, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c := Collection{}
			for i := 0; i < count; i++ {
				c.Columns[Approved] = append(c.Columns[Approved], Record{Item: gardensync.Item{Issue: garden.Issue{ID: garden.IssueID(i + 1)}}})
			}
			s := Selection{Mode: ModeDetailed, Column: Approved, Initialized: true}
			s.resize(c)
			s.Rows[Approved] = 9
			s.Scroll[Approved] = 9
			s.Scroll[Blocked] = 9
			s.EnsureVisible(c, 3, 4)
			if s.Scroll[Approved] != max(0, count-3) || s.Scroll[Blocked] != 0 {
				t.Fatalf("scroll not clamped: %+v", s.Scroll)
			}
		})
	}
}

func tableRecord(id garden.IssueID, number int64, col Column, prio string) Record {
	return Record{Column: col, Priority: prio, Item: gardensync.Item{Issue: garden.Issue{ID: id, Number: number}}}
}
func TestRowsOrderingIsDeterministicAndReadOnly(t *testing.T) {
	c := Collection{Records: []Record{
		tableRecord(1, 10, Done, "P1"), tableRecord(2, 20, Approved, "?"), tableRecord(3, 30, Approved, "P0"),
		tableRecord(4, 30, Running, "P1"), tableRecord(5, 40, Approved, "P3"), tableRecord(6, 30, Approved, "P0"),
	}}
	for _, tc := range []struct {
		sort Sort
		want []garden.IssueID
	}{
		{SortState, []garden.IssueID{2, 3, 6, 5, 4, 1}},
		{SortNumber, []garden.IssueID{1, 2, 3, 4, 6, 5}},
		{SortPriority, []garden.IssueID{3, 6, 1, 4, 5, 2}},
	} {
		var got []garden.IssueID
		for _, r := range c.Rows(tc.sort) {
			got = append(got, r.Item.Issue.ID)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%v: %v want %v", tc.sort, got, tc.want)
		}
	}
	if c.Records[0].Item.Issue.ID != 1 || c.Records[5].Item.Issue.ID != 6 {
		t.Fatal("Rows reordered Records")
	}
	if SortState.Next() != SortNumber || SortNumber.Next() != SortPriority || SortPriority.Next() != SortState {
		t.Fatal("sort cycle")
	}
}
func TestRowsCarryColumnFromSelect(t *testing.T) {
	c := Select([]gardensync.Item{accepted(t, 1, garden.StageDone, false), accepted(t, 2, garden.StageReady, false)}, Query{}, nil)
	rows := c.Rows(SortState)
	if rows[0].Column != Approved || rows[1].Column != Done {
		t.Fatalf("%+v", rows)
	}
}
func TestSelectionFocusSharesIdentity(t *testing.T) {
	c := Select([]gardensync.Item{accepted(t, 1, garden.StageDone, false), accepted(t, 2, garden.StageDone, false), accepted(t, 3, garden.StageReady, false)}, Query{}, nil)
	s := Selection{Mode: ModeDetailed}
	if !s.Focus(c, 2) || s.ID != 2 || s.Column != Done || s.Rows[Done] != 1 || !s.Initialized {
		t.Fatalf("%+v", s)
	}
	before := s
	if s.Focus(c, 99) || !reflect.DeepEqual(s, before) {
		t.Fatal("missing id changed selection")
	}
}
func TestNextGateIsAHumanGateForEveryColumn(t *testing.T) {
	want := map[Column]string{
		Approved: "Request an AI plan", Planning: "Approve the plan once drafted", PlanReview: "Review and approve the plan",
		Ready: "Start implementation locally (s)", Running: "Review the change when the run ends", Reviewing: "Review the change after agent review",
		HumanReview: "Review the change", Done: "None", Blocked: "Resolve the blocker",
	}
	if len(want) != int(ColumnCount) {
		t.Fatal("table misses a column")
	}
	for c, g := range want {
		if NextGate(c) != g {
			t.Errorf("%v: %q want %q", c, NextGate(c), g)
		}
	}
}
