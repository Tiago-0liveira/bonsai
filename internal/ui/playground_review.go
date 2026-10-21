package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// Review reads have independent freshness. In particular, a failed report-store
// read cannot erase a known HEAD or turn unavailable CI into a pass.
type pgReview struct {
	open                      bool
	session                   string
	reports                   pgCall[playgroundapi.ReviewsView]
	head                      pgCall[playgroundapi.ReviewsView]
	policy                    pgCall[playgroundapi.ReviewPolicyView]
	checks                    pgCall[playgroundapi.ChecksView]
	revision, finding, scroll int
	busy                      bool
	seq                       uint64
	action                    pgReviewAction
	reconcileBusy             bool
	reconcileErr              *playgroundapi.Error
	note                      string
}

type pgReviewAction struct {
	session      string
	head         string
	revision     int
	finding      string
	reason       string
	preview      *playgroundapi.AcceptPreviewView
	acknowledged []string
}

type pgReviewDoneMsg struct {
	session string
	seq     uint64
	err     error
	note    string
}
type pgReviewPreviewMsg struct {
	session string
	seq     uint64
	view    playgroundapi.AcceptPreviewView
	err     error
}
type pgReviewTickMsg struct{}
type pgReviewReconciledMsg struct {
	view playgroundapi.ReconcileView
	err  error
}

func (m Model) reviewBackend() (playgroundapi.ReviewBackend, bool) {
	b, ok := m.pg.api.(playgroundapi.ReviewBackend)
	return b, ok
}

func (m Model) pgReviewOpen() (tea.Model, tea.Cmd) {
	if _, ok := m.reviewBackend(); !ok {
		m.status = "Read-only review is unavailable on this host"
		return m, nil
	}
	r, ok := m.playgroundSelected()
	if !ok || m.pg.sub == pgSubUsage {
		return m, nil
	}
	v := &m.pg.rev
	if v.busy {
		return m, nil
	}
	if v.session != r.ID {
		v.revision, v.finding, v.scroll = 0, 0, 0
	}
	v.open, v.session, v.note = true, r.ID, ""
	m.pg.open = false
	// Review never captures terminal input, even if this session was controlled.
	if m.pg.own && m.agents.view != nil {
		m.agents.view.control = false
	}
	return m, m.pgReviewRead()
}

func (m *Model) pgReviewClose() {
	v := &m.pg.rev
	v.open, v.action = false, pgReviewAction{}
	// A preview received after leaving must never open a dialog on another screen.
	if !v.busy {
		v.seq++
	}
}

func (m *Model) pgReviewRead() tea.Cmd {
	b, ok := m.reviewBackend()
	if !ok {
		return nil
	}
	v := &m.pg.rev
	id, ctx, api := v.session, m.pgCtx(), m.pg.api
	rg, hg, pg, kg := v.reports.begin(id), v.head.begin(id), v.policy.begin("policy"), v.checks.begin(id)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			r, err := b.Reviews(ctx, playgroundapi.ReviewsRequest{SessionID: id})
			return pgReviewReportsMsg{pgReadMsg[playgroundapi.ReviewsView]{key: id, gen: rg, v: r, err: err}, hg}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			p, err := b.ReviewPolicy(ctx)
			return pgReadMsg[playgroundapi.ReviewPolicyView]{key: "policy", gen: pg, v: p, err: err}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			k, err := api.Checks(ctx, playgroundapi.ChecksRequest{SessionID: id})
			return pgReviewChecksMsg{pgReadMsg[playgroundapi.ChecksView]{key: id, gen: kg, v: k, err: err}}
		},
	)
}

// Checks have their own message so a Changes read can never land in Review.
type pgReviewChecksMsg struct {
	pgReadMsg[playgroundapi.ChecksView]
}

type pgReviewReportsMsg struct {
	pgReadMsg[playgroundapi.ReviewsView]
	headGen uint64
}

func reviewTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return pgReviewTickMsg{} })
}

// One reconciliation at a time, including while another workspace is open.
// The backend re-reads persisted local consent and fences run keys durably.
func (m Model) pgReviewMsg(msg tea.Msg) (bool, Model, tea.Cmd) {
	v := &m.pg.rev
	switch msg := msg.(type) {
	case pgReviewTickMsg:
		b, ok := m.reviewBackend()
		if !ok {
			return true, m, nil
		}
		if v.reconcileBusy {
			return true, m, nil
		}
		v.reconcileBusy = true
		ctx := m.pgCtx()
		return true, m, func() tea.Msg { r, err := b.ReconcileReviews(ctx); return pgReviewReconciledMsg{r, err} }
	case pgReviewReconciledMsg:
		v.reconcileBusy = false
		v.reconcileErr = msg.view.Err
		if msg.err != nil {
			v.reconcileErr = pgAPIError(msg.err)
		}
		var read tea.Cmd
		if v.open && m.workspace == wsPlayground && !v.busy && v.action.preview == nil {
			read = m.pgReviewRead()
		}
		return true, m, tea.Batch(reviewTick(), read)
	case pgReviewReportsMsg:
		if msg.err != nil {
			v.reports.fail(msg.key, msg.gen, msg.err)
			v.head.fail(msg.key, msg.headGen, msg.err)
			return true, m, nil
		}
		r := msg.v
		v.head.ok(msg.key, msg.headGen, r.Stamp, r, r.HeadErr)
		if v.reports.ok(msg.key, msg.gen, r.Stamp, r, r.ReportsErr) {
			// Follow the revision rather than its shifting index when a new report arrives.
			if v.reports.err == nil && m.pgReviewReport().Revision == 0 {
				v.revision = 0
				if len(r.Reports) > 0 {
					v.revision = r.Reports[0].Revision
				}
			}
			v.finding = max(0, min(v.finding, len(m.pgReviewReport().Findings)-1))
		}
		return true, m, nil
	case pgReadMsg[playgroundapi.ReviewPolicyView]:
		if msg.err != nil {
			v.policy.fail(msg.key, msg.gen, msg.err)
		} else {
			err := msg.v.PrefsErr
			if err == nil {
				err = msg.v.AccountErr
			}
			v.policy.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, err)
		}
		return true, m, nil
	case pgReviewChecksMsg:
		if msg.err != nil {
			v.checks.fail(msg.key, msg.gen, msg.err)
		} else {
			var err *playgroundapi.Error
			if msg.v.State == playgroundapi.ChecksUnavailable {
				err = &playgroundapi.Error{Code: msg.v.Code}
			}
			v.checks.ok(msg.key, msg.gen, msg.v.Stamp, msg.v, err)
		}
		return true, m, nil
	case pgReviewPreviewMsg:
		if msg.seq != v.seq || msg.session != v.session {
			return true, m, nil
		}
		v.busy = false
		if !v.open || m.workspace != wsPlayground {
			return true, m, nil
		}
		if msg.err != nil {
			v.note = "Accept preview: " + pgReviewError(msg.err)
			return true, m, nil
		}
		if !msg.view.AcceptAllowed || msg.view.SessionID != v.session {
			v.note = "Acceptance is unavailable for this attempt"
			return true, m, nil
		}
		v.action = pgReviewAction{session: v.session, preview: &msg.view}
		v.scroll = 0
		return true, m, nil
	case pgReviewDoneMsg:
		if msg.seq != v.seq || msg.session != v.session {
			return true, m, nil
		}
		v.busy, v.action = false, pgReviewAction{}
		if msg.err != nil {
			v.note = pgReviewError(msg.err)
		} else {
			v.note = msg.note
		}
		if v.open && m.workspace == wsPlayground {
			return true, m, m.pgReviewRead()
		}
		return true, m, nil
	}
	return false, m, nil
}

func pgReviewError(err error) string {
	switch c := pgAPIError(err).Code; c {
	case playgroundapi.CodeReviewRunning:
		return "A reviewer is already running"
	case playgroundapi.CodeReadOnlyUnsupported:
		return "The account cannot enforce read-only access and a single report file"
	case playgroundapi.CodePolicyIncomplete:
		return "Choose a review account in Settings > Agents"
	case playgroundapi.CodeNotConfirmed:
		return "Local confirmation is required"
	case playgroundapi.CodeNeedsAcknowledgment:
		return "Preview again and acknowledge the required conditions"
	case playgroundapi.CodeNotReviewable:
		return "This session has no verified implementation attempt"
	case playgroundapi.CodeStore:
		return "Review records are unavailable"
	default:
		return pgReasonText(c)
	}
}

func (m Model) pgReviewReport() playgroundapi.ReviewReport {
	for _, r := range m.pg.rev.reports.val.Reports {
		if r.Revision == m.pg.rev.revision {
			return r
		}
	}
	return playgroundapi.ReviewReport{}
}

func (m Model) pgReviewConfirm(kind modals.Kind, title, body string) Model {
	d := modals.NewConfirm(kind, title)
	d.SetBody(wrapParagraphs(body, max(min(m.width-14, 92), 24)))
	d.SetSize(m.width, m.height)
	m.modal = &d
	return m
}

func (m Model) pgReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.pg.rev
	if v.busy {
		return m, nil
	}
	if v.action.preview != nil {
		switch msg.String() {
		case "esc":
			v.action = pgReviewAction{}
			v.scroll = 0
		case "enter":
			return m.pgReviewAcceptDialog(), nil
		case "down", "j":
			m.pgReviewScroll(1)
		case "up", "k":
			m.pgReviewScroll(-1)
		case "pgdown":
			m.pgReviewScroll(max(m.height-12, 1))
		case "pgup":
			m.pgReviewScroll(-max(m.height-12, 1))
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.pgReviewClose()
	case "r":
		return m, m.pgReviewRead()
	case "down", "j":
		m.pgReviewScroll(1)
	case "up", "k":
		m.pgReviewScroll(-1)
	case "pgdown":
		m.pgReviewScroll(max(m.height-12, 1))
	case "pgup":
		m.pgReviewScroll(-max(m.height-12, 1))
	case "n", "p":
		d := 1
		if msg.String() == "p" {
			d = -1
		}
		v.finding = max(0, min(v.finding+d, len(m.pgReviewReport().Findings)-1))
		v.scroll = 0
	case "[", "]":
		rs := v.reports.val.Reports
		for i, r := range rs {
			if r.Revision == v.revision {
				d := 1
				if msg.String() == "[" {
					d = -1
				}
				i = max(0, min(i+d, len(rs)-1))
				v.revision, v.finding, v.scroll = rs[i].Revision, 0, 0
				break
			}
		}
	case "t":
		if !m.pgReviewWritable() {
			return m, nil
		}
		if !v.policy.have || v.policy.err != nil || !v.policy.val.Capability.Supported {
			v.note = "Choose an account with enforced read-only support in Settings > Agents"
			return m, nil
		}
		if v.reports.val.RunInFlight {
			v.note = "A reviewer is already running"
			return m, nil
		}
		v.action = pgReviewAction{session: v.session, head: v.head.val.Head}
		return m.pgReviewConfirm(modals.KindPlaygroundReviewRetry, "Run read-only review?", fmt.Sprintf("Account: %s (%s)\nRole: %s\nScope: attempt %s at %s\nThis starts one local reviewer run. It may write only its own report file.", pgClean(v.policy.val.AccountName, 80), pgClean(v.policy.val.Provider, 30), playgroundapi.ReviewRole, pgClean(v.reports.val.AttemptID, 100), pgShortSHA(v.head.val.Head))), nil
	case "d":
		if !m.pgReviewWritable() {
			return m, nil
		}
		r := m.pgReviewReport()
		if v.finding >= len(r.Findings) || r.State != review.StateReady {
			return m, nil
		}
		f := r.Findings[v.finding]
		if f.Dismissed != nil {
			return m, nil
		}
		v.action = pgReviewAction{session: v.session, revision: r.Revision, finding: f.ID}
		d := modals.NewInput(modals.KindPlaygroundReviewDismiss, "Reason for dismissing finding", "1-500 characters")
		d.SetBody(fmt.Sprintf("r%d at %s: %s", r.Revision, pgShortSHA(r.Commit), pgClean(f.Title, 120)))
		d.SetSize(m.width, m.height)
		m.modal = &d
	case "a":
		if !m.pgReviewWritable() {
			return m, nil
		}
		b, _ := m.reviewBackend()
		id, ctx := v.session, m.pgCtx()
		v.busy = true
		v.seq++
		seq := v.seq
		return m, func() tea.Msg {
			p, err := b.PreviewAccept(ctx, playgroundapi.AcceptPreviewRequest{SessionID: id})
			return pgReviewPreviewMsg{id, seq, p, err}
		}
	case "f":
		return m.pgReviewRequestFixes()
	}
	return m, nil
}

func (m Model) pgReviewWritable() bool {
	v := m.pg.rev
	return v.open && v.reports.have && v.reports.err == nil && v.reports.val.Reviewable && v.head.have && v.head.err == nil && v.head.val.HeadRead && !v.busy
}

// Request fixes enters the same account/options, isolation and local confirmation
// flow as a new implementation. This key never starts a session itself.
func (m Model) pgReviewRequestFixes() (tea.Model, tea.Cmd) {
	r, ok := m.playgroundSelected()
	if !m.pgReviewWritable() || !ok || !r.Task || r.Ref.AttemptID != m.pg.rev.reports.val.AttemptID {
		return m, nil
	}
	rec, ok := pgTask(m.garden.board.collection, r.Ref)
	if !ok || !m.launchSupported() {
		m.pg.rev.note = "Open the canonical Issue in Garden to request a locally confirmed implementation"
		return m, nil
	}
	m, nav := m.playgroundOpenIssue()
	if m.workspace != wsGarden {
		return m, nav
	}
	m.garden.board.opened = rec.Item.Issue.ID
	m, form := m.gardenLaunchOpen()
	return m, tea.Batch(nav, form)
}

func (m Model) pgReviewSubmit(kind modals.Kind, value string) (tea.Model, tea.Cmd) {
	v := &m.pg.rev
	a := v.action
	if !v.open || m.workspace != wsPlayground || a.session != v.session || v.busy {
		return m, nil
	}
	b, ok := m.reviewBackend()
	if !ok {
		return m, nil
	}
	if kind == modals.KindPlaygroundReviewDismiss {
		reason, err := review.CleanReason(value)
		if err != nil {
			v.note = "Dismissal needs a reason of 1-500 characters, without control characters"
			v.action = pgReviewAction{}
			return m, nil
		}
		v.action.reason = reason
		return m.pgReviewConfirm(modals.KindPlaygroundReviewDismissAsk, "Dismiss this finding?", fmt.Sprintf("Report r%d\nReason: %s\nThe reason applies only to this finding and report revision.", a.revision, reason)), nil
	}
	if kind == modals.KindPlaygroundReviewAcceptAsk {
		if a.preview == nil {
			return m, nil
		}
		if len(a.acknowledged) < len(a.preview.Required) {
			v.action.acknowledged = append(append([]string(nil), a.acknowledged...), string(a.preview.Required[len(a.acknowledged)]))
			return m.pgReviewAcceptDialog(), nil
		}
	}
	ctx, id := m.pgCtx(), a.session
	v.busy = true
	v.seq++
	seq := v.seq
	switch kind {
	case modals.KindPlaygroundReviewRetry:
		// A fresh request identity for each confirmed local retry; captured in the
		// command so a double delivery uses exactly the same durable run key.
		req := playgroundapi.StartReviewRequest{SessionID: id, ExpectHead: a.head, Confirmed: true, RequestID: fmt.Sprintf("tui-%d-%d", time.Now().UnixNano(), seq)}
		return m, func() tea.Msg {
			_, err := b.StartReview(ctx, req)
			return pgReviewDoneMsg{id, seq, err, "Read-only reviewer requested"}
		}
	case modals.KindPlaygroundReviewDismissAsk:
		return m, func() tea.Msg {
			_, err := b.DismissFinding(ctx, playgroundapi.DismissRequest{SessionID: id, ReportRevision: a.revision, FindingID: a.finding, Reason: a.reason})
			return pgReviewDoneMsg{id, seq, err, "Dismissal reason recorded"}
		}
	case modals.KindPlaygroundReviewAcceptAsk:
		p := *a.preview
		return m, func() tea.Msg {
			_, err := b.Accept(ctx, playgroundapi.AcceptRequest{Preview: p, Acknowledged: a.acknowledged, Confirmed: true})
			return pgReviewDoneMsg{id, seq, err, "Human acceptance recorded; merge and cleanup remain separate"}
		}
	}
	v.busy = false
	return m, nil
}

func (m Model) pgReviewInit() tea.Cmd {
	if _, ok := m.reviewBackend(); !ok {
		return nil
	}
	return func() tea.Msg { return pgReviewTickMsg{} }
}

func (m *Model) pgReviewScroll(delta int) {
	rows := max(m.height-shellHeaderRows-shellFooterRows-settingsSubmenuRows, 1)
	total := len(m.pgReviewLines(m.width, 1<<20))
	m.pg.rev.scroll = max(0, min(m.pg.rev.scroll+delta, max(total-rows, 0)))
}
