// Package work selects accepted Garden work independently of its presentation.
package work

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

type Column int

const (
	Approved Column = iota
	Planning
	PlanReview
	Ready
	Running
	Reviewing
	HumanReview
	Done
	Blocked
	ColumnCount
)

var names = [ColumnCount]string{"Approved", "Planning", "Plan Review", "Ready", "Running", "Reviewing", "Human Review", "Done", "Blocked"}

func (c Column) String() string {
	if c < 0 || c >= ColumnCount {
		return "Approved"
	}
	return names[c]
}

type Query struct {
	Text     string
	Type     garden.ItemType
	Priority string
}

func (q Query) Active() bool {
	return strings.TrimSpace(q.Text) != "" || q.Type != "" || q.Priority != ""
}

type Record struct {
	Findings []gardensync.Finding
	Item     gardensync.Item
	Column   Column
	Priority string
}
type Collection struct {
	Records         []Record
	Columns         [ColumnCount][]Record
	Total, Excluded int
}

// Select uses the canonical verifier again rather than trusting a cached lane or
// a stage label. Attention findings cannot manufacture specification acceptance.
func Select(items []gardensync.Item, q Query, findings ...gardensync.Finding) Collection {
	var out Collection
	for _, it := range items {
		if it.Deleted {
			continue
		}
		meta, err := garden.DecodeMetadata(it.Issue.Metadata)
		c := garden.Classify(it.Issue.Labels)
		// Recomputed canonical acceptance is authoritative; a cached Attention lane
		// may only describe display diagnostics, which remain visible as conflicts.
		// Adapter identity defects still exclude an unusable canonical Issue.
		lane := garden.Lane(c, meta, it.Verification, err)
		if it.Issue.ID == 0 || c.Stage == garden.StageDraft || (lane.Lane != garden.LaneWork && !retained(it)) || len(it.Issue.Problems) > 0 {
			out.Excluded++
			continue
		}
		out.Total++
		r := Record{Item: it, Column: placement(c.Stage, it.Issue, it.Verification), Priority: priority(it.Issue.Labels)}
		r.Item.Classification = c
		for _, f := range findings {
			if f.IssueID == it.Issue.ID {
				r.Findings = append(r.Findings, f)
			}
		}
		hay := strings.ToLower(fmt.Sprintf("#%d %s", it.Issue.Number, it.Issue.Title))
		if !strings.Contains(hay, strings.ToLower(strings.TrimSpace(q.Text))) || q.Type != "" && q.Type != c.Type || q.Priority != "" && q.Priority != r.Priority {
			continue
		}
		out.Records = append(out.Records, r)
	}
	sort.Slice(out.Records, func(i, j int) bool {
		a, b := out.Records[i].Item.Issue, out.Records[j].Item.Issue
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		return a.ID < b.ID
	})
	for _, r := range out.Records {
		out.Columns[r.Column] = append(out.Columns[r.Column], r)
	}
	return out
}

// Labels do not prove plan approval. Ready is derived from the verified pair;
// active planner/reviewer columns still require separate local-job evidence.
func placement(stage garden.Stage, issue garden.Issue, v garden.Verification) Column {
	switch stage {
	case garden.StageInProgress:
		return Running
	case garden.StageReview:
		return HumanReview
	case garden.StageDone:
		return Done
	case garden.StageBlocked:
		return Blocked
	default:
		rv := planrev.FromVerification(issue, v)
		if rv.Ready {
			return Ready
		}
		if rv.Latest != nil && rv.Latest.State == planrev.StateRejected {
			return Approved
		}
		for _, h := range v.Heads {
			if h.Kind == garden.RevisionPlan {
				return PlanReview
			}
		}
		return Approved
	}
}

// NextGate names the next human decision for a state. It is fixed text per
// column: the read projection proves no more than the column itself.
func NextGate(c Column) string {
	switch c {
	case Approved:
		return "Request an AI plan"
	case Planning:
		return "Approve the plan once drafted"
	case PlanReview:
		return "Review and approve the plan"
	case Ready:
		return "Launch unavailable (plan 09)"
	case Running:
		return "Review the change when the run ends"
	case Reviewing:
		return "Review the change after agent review"
	case HumanReview:
		return "Review the change"
	case Blocked:
		return "Resolve the blocker"
	}
	return "None"
}

// Priority labels are optional display data outside the managed namespace.
// Conflicting values are unknown rather than silently choosing one.
func priority(labels []string) string {
	found := ""
	for _, l := range labels {
		l = strings.ToLower(l)
		for _, p := range []string{"p0", "p1", "p2", "p3"} {
			if l == "priority:"+p {
				if found != "" && found != strings.ToUpper(p) {
					return "?"
				}
				found = strings.ToUpper(p)
			}
		}
	}
	if found == "" {
		return "?"
	}
	return found
}

func (c Collection) Find(id garden.IssueID) (Record, int, bool) {
	for col, rows := range c.Columns {
		for i, r := range rows {
			if r.Item.Issue.ID == id {
				r.Column = Column(col)
				return r, i, true
			}
		}
	}
	return Record{}, 0, false
}

// Sort orders Table rows. Every order ends in Issue number then ID, so rows
// never swap between renders.
type Sort int

const (
	SortState Sort = iota
	SortNumber
	SortPriority
	sortCount
)

func (s Sort) String() string {
	switch s {
	case SortNumber:
		return "Number"
	case SortPriority:
		return "Priority"
	}
	return "State"
}

// Next cycles State, Number, Priority.
func (s Sort) Next() Sort { return (s + 1) % sortCount }

// Rows returns a sorted copy of the filtered records; Records stays untouched.
func (c Collection) Rows(s Sort) []Record {
	out := make([]Record, len(c.Records))
	copy(out, c.Records)
	rank := func(p string) int {
		if len(p) == 2 && p[0] == 'P' && p[1] >= '0' && p[1] <= '3' {
			return int(p[1] - '0')
		}
		return 4
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch s {
		case SortState:
			if a.Column != b.Column {
				return a.Column < b.Column
			}
		case SortPriority:
			if ra, rb := rank(a.Priority), rank(b.Priority); ra != rb {
				return ra < rb
			}
		}
		if a.Item.Issue.Number != b.Item.Issue.Number {
			return a.Item.Issue.Number < b.Item.Issue.Number
		}
		return a.Item.Issue.ID < b.Item.Issue.ID
	})
	return out
}

// Selection keeps identity separate from position and remembers each column's
// cursor and scroll. Empty columns retain focus without inventing an item.
type Selection struct {
	ID          garden.IssueID
	Column      Column
	Rows        [ColumnCount]int
	Scroll      [ColumnCount]int
	Window      int
	Initialized bool
}

func (s *Selection) choose(c Collection) {
	rows := c.Columns[s.Column]
	s.Rows[s.Column] = max(0, min(s.Rows[s.Column], len(rows)-1))
	s.ID = 0
	if len(rows) > 0 {
		s.ID = rows[s.Rows[s.Column]].Item.Issue.ID
		s.Initialized = true
	}
}

// Focus points the Board cursor at id so Table and Board share one identity.
func (s *Selection) Focus(c Collection, id garden.IssueID) bool {
	r, i, ok := c.Find(id)
	if !ok {
		return false
	}
	s.ID, s.Column, s.Initialized = id, r.Column, true
	s.Rows[r.Column] = i
	return true
}
func (s *Selection) Move(c Collection, horizontal, vertical int) {
	s.Column = Column(max(0, min(int(s.Column)+horizontal, int(ColumnCount)-1)))
	s.Rows[s.Column] += vertical
	s.choose(c)
}
func (s *Selection) Reconcile(c Collection) string {
	if !s.Initialized {
		for col, rows := range c.Columns {
			if len(rows) > 0 {
				s.Column = Column(col)
				break
			}
		}
		s.choose(c)
		return ""
	}
	if s.ID == 0 {
		s.choose(c)
		return ""
	}
	if r, i, ok := c.Find(s.ID); ok {
		s.Column = r.Column
		s.Rows[s.Column] = i
		return ""
	}
	s.choose(c)
	if s.ID == 0 {
		for d := 1; d < int(ColumnCount); d++ {
			for _, n := range []int{int(s.Column) + d, int(s.Column) - d} {
				if n >= 0 && n < int(ColumnCount) && len(c.Columns[n]) > 0 {
					s.Column = Column(n)
					s.choose(c)
					return "Selected issue left this view; selected nearest available item."
				}
			}
		}
	}
	if s.ID == 0 {
		return "Selected issue left this view; no matching work remains."
	}
	return "Selected issue left this view; selected nearest available item."
}
func (s *Selection) EnsureVisible(c Collection, capacity, columns int) {
	capacity = max(1, capacity)
	columns = max(1, min(columns, int(ColumnCount)))
	for col, rows := range c.Columns {
		s.Scroll[col] = max(0, min(s.Scroll[col], len(rows)-capacity))
	}
	row := max(0, min(s.Rows[s.Column], len(c.Columns[s.Column])-1))
	s.Scroll[s.Column] = max(0, min(s.Scroll[s.Column], row))
	if row >= s.Scroll[s.Column]+capacity {
		s.Scroll[s.Column] = row - capacity + 1
	}
	s.Window = max(0, min(s.Window, int(ColumnCount)-columns))
	if int(s.Column) < s.Window {
		s.Window = int(s.Column)
	}
	if int(s.Column) >= s.Window+columns {
		s.Window = int(s.Column) - columns + 1
	}
}

// retained keeps an accepted snapshot in Work when its specification head moves.
// Gate assessment still blocks every downstream action until it is revalidated.
func retained(it gardensync.Item) bool {
	m, err := garden.DecodeMetadata(it.Issue.Metadata)
	if err != nil || len(garden.Classify(it.Issue.Labels).Problems) > 0 {
		return false
	}
	for _, a := range it.Verification.SupersededApprovals {
		if !a.Revoked && (!m.Accepted || a.RevisionHash == m.AcceptedSpecHash && a.SourceSpecFingerprint == m.SpecFingerprint) {
			return true
		}
	}
	return false
}
