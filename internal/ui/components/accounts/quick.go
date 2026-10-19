package accounts

import (
	"context"
	"maps"
	"slices"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

const (
	// quickInflight bounds concurrent Usage calls so opening the overlay does
	// not hit every provider at once.
	quickInflight = 3
)

// OpenAccountsMsg asks the parent to replace the overlay with the accounts
// modal selected on Account.
type OpenAccountsMsg struct{ Account agents.AccountID }

type (
	quickListMsg struct {
		seq      int
		accounts []agentsapi.Account
		err      error
	}
)

type usageJob struct {
	id      agents.AccountID
	seq     uint64
	refresh bool
}

// Quick shows a stable list of account usage strips. It is a value type like
// Model; results are ordered by
// per-account sequence numbers so a late answer never overwrites newer data.
type Quick struct {
	api   agentsapi.Accounts
	ascii bool
	now   func() time.Time
	w, h  int // screen size; the frame is derived by quickSize

	accounts []agentsapi.Account
	listed   bool
	listErr  string
	focus    agents.AccountID

	n       int // request counter
	listSeq int
	queue   []usageJob // waiting for a free slot
	// pending holds the seqs of dispatched calls whose result has not arrived;
	// each one counts against quickInflight.
	pending map[uint64]struct{}

	usage usagestore.Store
}

// NewQuick builds the overlay focused on an account (the first one when
// focus is empty or unknown). Init starts loading.
func NewQuick(api agentsapi.Accounts, focus agents.AccountID, ascii bool) Quick {
	return Quick{
		api:     api,
		ascii:   ascii,
		now:     time.Now,
		focus:   focus,
		n:       1,
		listSeq: 1,
		pending: map[uint64]struct{}{},
		usage:   usagestore.Store{},
	}
}

// WithUsage seeds the overlay with the usage the root model already knows.
func (q Quick) WithUsage(seed usagestore.Store) Quick {
	q.usage = seed.Seeded()
	return q
}

// WithClock sets the clock used for "Updated .. ago" and reset countdowns.
func (q Quick) WithClock(now func() time.Time) Quick {
	if now != nil {
		q.now = now
	}
	return q
}

// Usage is the usage this overlay knows.
func (q Quick) Usage() usagestore.Store { return q.usage }

// Init lists accounts; usage loads once the list is in.
func (q Quick) Init() tea.Cmd { return quickListCmd(q.api, q.listSeq) }

// SetSize gives the view the available pane size.
func (q *Quick) SetSize(w, h int) { q.w, q.h = w, h }

func quickListCmd(api agentsapi.Accounts, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		accounts, err := api.List(ctx)
		return quickListMsg{seq: seq, accounts: accounts, err: err}
	}
}

// ordered is the display and j/k order: provider group, then name.
func (q Quick) ordered() []agentsapi.Account {
	out := Sidebar{Accounts: q.accounts}.Ordered()
	rank := func(p agents.ProviderID) int {
		if i := slices.Index(groupOrder, p); i >= 0 {
			return i
		}
		return len(groupOrder)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := rank(a.Provider), rank(b.Provider); ra != rb {
			return ra < rb
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Name < b.Name
	})
	return out
}

// Update handles keys and backend results.
func (q Quick) Update(msg tea.Msg) (Quick, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return q.onKey(msg)
	case quickListMsg:
		return q.onList(msg)
	case usagestore.Result:
		return q.onUsage(msg)
	}
	return q, nil
}

func (q Quick) onKey(k tea.KeyMsg) (Quick, tea.Cmd) {
	switch k.String() {
	case "esc", "u":
		return q, emit(CloseMsg{})
	case "m":
		return q, emit(OpenAccountsMsg{Account: q.focus})
	case "j", "down":
		return q.move(1), nil
	case "k", "up":
		return q.move(-1), nil
	case "r":
		return q.refresh()
	}
	return q, nil
}

func (q Quick) move(delta int) Quick {
	ids := q.ordered()
	i := slices.IndexFunc(ids, func(a agentsapi.Account) bool { return a.ID == q.focus })
	if len(ids) == 0 || i < 0 {
		return q
	}
	q.focus = ids[min(max(i+delta, 0), len(ids)-1)].ID
	return q
}

// refresh re-reads every account from its provider. Without a list yet it
// retries the list instead.
func (q Quick) refresh() (Quick, tea.Cmd) {
	if !q.listed || q.listErr != "" {
		q.n++
		q.listSeq = q.n
		return q, quickListCmd(q.api, q.listSeq)
	}
	return q.enqueue(true)
}

// enqueue replaces the waiting jobs with one per account and starts as many
// as the cap allows. Calls already in flight keep counting against the cap;
// their results are dropped once a newer request for the account is begun.
func (q Quick) enqueue(refresh bool) (Quick, tea.Cmd) {
	q.queue = q.queue[:0:0]
	accts := q.ordered()
	// The focused account goes first so the part that matters fills in first.
	sort.SliceStable(accts, func(i, j int) bool { return accts[i].ID == q.focus && accts[j].ID != q.focus })
	for _, a := range accts {
		var seq uint64
		q.usage, seq = q.usage.Begin(a.ID)
		q.queue = append(q.queue, usageJob{a.ID, seq, refresh})
	}
	return q.pump()
}

// pump dispatches queued jobs up to the in-flight cap.
func (q Quick) pump() (Quick, tea.Cmd) {
	var cmds []tea.Cmd
	for len(q.pending) < quickInflight && len(q.queue) > 0 {
		job := q.queue[0]
		q.queue = q.queue[1:]
		q.pending = maps.Clone(q.pending)
		q.pending[job.seq] = struct{}{}
		cmds = append(cmds, usagestore.Fetch(context.Background(), q.api, nil, job.id, job.seq, job.refresh))
	}
	return q, tea.Batch(cmds...)
}

func (q Quick) onList(msg quickListMsg) (Quick, tea.Cmd) {
	if msg.seq != q.listSeq {
		return q, nil
	}
	q.listed = true
	if msg.err != nil {
		q.listErr = usagestore.Sanitize(msg.err)
		return q, nil
	}
	q.listErr = ""
	q.accounts = msg.accounts
	ids := q.ordered()
	if !slices.ContainsFunc(ids, func(a agentsapi.Account) bool { return a.ID == q.focus }) {
		q.focus = ""
		if len(ids) > 0 {
			q.focus = ids[0].ID
		}
	}
	return q.enqueue(false)
}

func (q Quick) onUsage(r usagestore.Result) (Quick, tea.Cmd) {
	// Every dispatched call frees its slot, even when its answer is dropped.
	if _, ours := q.pending[r.Seq]; ours {
		q.pending = maps.Clone(q.pending)
		delete(q.pending, r.Seq)
	}
	var applied bool
	q.usage, applied = q.usage.Apply(r)
	q, cmd := q.pump()
	if applied {
		cmd = tea.Batch(cmd, usagestore.Observe(r))
	}
	return q, cmd
}

func (q Quick) busy() bool { return len(q.pending) > 0 || len(q.queue) > 0 }
