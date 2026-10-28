// Package work selects accepted Garden work independently of its presentation.
package work

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

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

// Phase groups verified stages without granting workflow approval.
type Phase int

const (
	Backlog Phase = iota
	PlanningPhase
	ReadyPhase
	Building
	Review
	DonePhase
	PhaseCount
)

func (p Phase) String() string {
	if p < 0 || p >= PhaseCount {
		return "Unplaced"
	}
	return [...]string{"Backlog", "Planning", "Ready", "Building", "Review", "Done"}[p]
}
func (c Column) Phase() Phase {
	switch c {
	case Planning, PlanReview:
		return PlanningPhase
	case Ready:
		return ReadyPhase
	case Running:
		return Building
	case Reviewing, HumanReview:
		return Review
	case Done:
		return DonePhase
	case Blocked:
		return PhaseCount
	default:
		return Backlog
	}
}

type Activity int

const (
	ActivityUnknown Activity = iota
	ActivityWorking
	ActivityNeedsInput
	ActivityIdle
	ActivityExited
	ActivityFailed
)

type RunEvidence struct {
	Kind, State               string
	Live                      bool
	Activity                  Activity
	AwaitingReview, PlanReady bool
	Updated                   time.Time
}
type Evidence struct {
	Planner, Implementation *RunEvidence
	Stale                   bool
}
type Mode int

const (
	ModePhases Mode = iota
	ModeDetailed
)

func (c Collection) Lanes(m Mode) [][]Record {
	if m == ModeDetailed {
		return c.Columns[:]
	}
	lanes := append([][]Record(nil), c.Phases[:]...)
	if len(c.Unplaced) > 0 {
		lanes = append(lanes, c.Unplaced)
	}
	return lanes
}
func (c Collection) LaneTitles(m Mode) []string {
	if m == ModeDetailed {
		return append([]string(nil), names[:]...)
	}
	titles := make([]string, PhaseCount)
	for p := Phase(0); p < PhaseCount; p++ {
		titles[p] = p.String()
	}
	if len(c.Unplaced) > 0 {
		titles = append(titles, "Unplaced")
	}
	return titles
}

// LaneTitle is "" when the lane is gone, e.g. Unplaced vanished under a search.
func (c Collection) LaneTitle(m Mode, lane Column) string {
	titles := c.LaneTitles(m)
	if lane < 0 || int(lane) >= len(titles) {
		return ""
	}
	return titles[lane]
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
	Phase    Phase
	Placed   bool
	Reason   string
	Evidence Evidence
	Findings []gardensync.Finding
	Item     gardensync.Item
	Column   Column
	Priority string
}
type Collection struct {
	Records         []Record
	Columns         [ColumnCount][]Record
	Phases          [PhaseCount][]Record
	Unplaced        []Record
	Total, Excluded int
}

// Select uses the canonical verifier again rather than trusting a cached lane or
// a stage label. Attention findings cannot manufacture specification acceptance.
func Select(items []gardensync.Item, q Query, evidence map[garden.IssueID]Evidence, findings ...gardensync.Finding) Collection {
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
		r.Evidence = evidence[it.Issue.ID]
		if r.Column == Approved && lane.Lane == garden.LaneWork && r.Evidence.Planner != nil && (r.Evidence.Planner.Live || r.Evidence.Planner.PlanReady) {
			r.Column = Planning
		}
		r.Phase, r.Placed = r.Column.Phase(), true
		if r.Column == Blocked {
			r.Phase, r.Reason, r.Placed = blockerPhase(it, r.Evidence, lane.Lane == garden.LaneWork)
			if r.Placed && lane.Lane != garden.LaneWork {
				r.Reason += "; approval not current"
			}
		}
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
		if r.Placed {
			out.Phases[r.Phase] = append(out.Phases[r.Phase], r)
		} else {
			out.Unplaced = append(out.Unplaced, r)
		}
	}
	return out
}

func blockerPhase(it gardensync.Item, e Evidence, accepted bool) (Phase, string, bool) {
	if a := e.Implementation; a != nil {
		p := Building
		if a.AwaitingReview {
			p = Review
		}
		return p, "implementation attempt " + a.State, true
	}
	if planrev.FromVerification(it.Issue, it.Verification).Ready {
		return ReadyPhase, "plan approved", true
	}
	for _, h := range it.Verification.Heads {
		if h.Kind == garden.RevisionPlan {
			return PlanningPhase, "plan revision published", true
		}
	}
	if e.Planner != nil {
		return PlanningPhase, "local planner attempt", true
	}
	if accepted {
		return Backlog, "specification accepted", true
	}
	return PhaseCount, "no proven phase", false
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
		return "Start implementation locally (s)"
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

// Selection keeps identity separate from position and remembers each lane's
// cursor and scroll. Column indexes a phase lane or a detailed stage according
// to Mode. Empty lanes retain focus without inventing an item.
type Selection struct {
	ID          garden.IssueID
	Column      Column
	Mode        Mode
	Rows        []int
	Scroll      []int
	Window      int
	Initialized bool
}

// resize copies cursor memory so Bubble Tea model values never alias on update.
func (s *Selection) resize(c Collection) {
	n := len(c.Lanes(s.Mode))
	rows, scroll := make([]int, n), make([]int, n)
	copy(rows, s.Rows)
	copy(scroll, s.Scroll)
	s.Rows, s.Scroll = rows, scroll
	s.Column = Column(max(0, min(int(s.Column), n-1)))
}

// Clamp keeps the lane index valid when lanes disappear and nothing else reconciles.
func (s *Selection) Clamp(c Collection) { s.resize(c) }
func (s *Selection) Clone() Selection {
	out := *s
	out.Rows, out.Scroll = slices.Clone(s.Rows), slices.Clone(s.Scroll)
	return out
}
func (s *Selection) find(c Collection, id garden.IssueID) (Column, int, bool) {
	for lane, rows := range c.Lanes(s.Mode) {
		for i, r := range rows {
			if r.Item.Issue.ID == id {
				return Column(lane), i, true
			}
		}
	}
	return 0, 0, false
}
func (s *Selection) choose(c Collection) {
	s.resize(c)
	rows := c.Lanes(s.Mode)[s.Column]
	s.Rows[s.Column] = max(0, min(s.Rows[s.Column], len(rows)-1))
	s.ID = 0
	if len(rows) > 0 {
		s.ID = rows[s.Rows[s.Column]].Item.Issue.ID
		s.Initialized = true
	}
}

// Focus points the Board cursor at id so Table and Board share one identity.
func (s *Selection) Focus(c Collection, id garden.IssueID) bool {
	col, i, ok := s.find(c, id)
	if !ok {
		return false
	}
	s.ID, s.Column, s.Initialized = id, col, true
	s.resize(c)
	s.Rows[col] = i
	return true
}
func (s *Selection) Move(c Collection, horizontal, vertical int) {
	s.resize(c)
	s.Column = Column(max(0, min(int(s.Column)+horizontal, len(c.Lanes(s.Mode))-1)))
	s.Rows[s.Column] += vertical
	s.choose(c)
}
func (s *Selection) Reconcile(c Collection) string {
	s.resize(c)
	if !s.Initialized {
		for col, rows := range c.Lanes(s.Mode) {
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
	if col, i, ok := s.find(c, s.ID); ok {
		s.Column = col
		s.Rows[s.Column] = i
		return ""
	}
	s.choose(c)
	if s.ID == 0 {
		for d := 1; d < len(c.Lanes(s.Mode)); d++ {
			for _, n := range []int{int(s.Column) + d, int(s.Column) - d} {
				if n >= 0 && n < len(c.Lanes(s.Mode)) && len(c.Lanes(s.Mode)[n]) > 0 {
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
	s.resize(c)
	capacity = max(1, capacity)
	columns = max(1, min(columns, len(c.Lanes(s.Mode))))
	for col, rows := range c.Lanes(s.Mode) {
		s.Scroll[col] = max(0, min(s.Scroll[col], len(rows)-capacity))
	}
	row := max(0, min(s.Rows[s.Column], len(c.Lanes(s.Mode)[s.Column])-1))
	s.Scroll[s.Column] = max(0, min(s.Scroll[s.Column], row))
	if row >= s.Scroll[s.Column]+capacity {
		s.Scroll[s.Column] = row - capacity + 1
	}
	s.Window = max(0, min(s.Window, len(c.Lanes(s.Mode))-columns))
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
