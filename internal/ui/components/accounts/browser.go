package accounts

import (
	"context"
	"maps"
	"os/exec"
	"slices"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// usageSlots bounds provider usage calls one view has in flight.
const usageSlots = 3

// seqBase gives every Browser its own range of request numbers, so an answer
// for a closed view never matches a request of a newer one.
var seqBase atomic.Int64

// Change says what a Browser.Update call changed, so the view that embeds the
// Browser can react to the parts it owns.
type Change int

const (
	ChangedList Change = 1 << iota
	ChangedDefaults
)

// Browser is the account list with the selected account's details: the
// profiles modal and the new-agent dialog both embed it, so they load, order
// and describe accounts, defaults and usage the same way. Backend calls run as
// commands; their results are ordered by sequence number before they apply.
// It is a value type.
type Browser struct {
	api agentsapi.Accounts

	accounts []agentsapi.Account
	listed   bool
	listErr  string
	selected agents.AccountID

	n           int // request counter
	latest      seqs
	describeSeq map[agents.AccountID]int

	// usage is this view's copy of the shared usage store: every result goes
	// through usagestore.Apply and is reported to the root model.
	usage    usagestore.Store
	usageLim usagestore.Limiter
	// allUsage: the sidebar shows every account's usage, so a list also
	// fetches the accounts nothing has loaded yet.
	allUsage bool

	describing  bool
	describeErr string

	defaults    agentsapi.Defaults
	defaultsErr string

	// pendingAdd remembers the account the terminal flow is creating so the
	// next list selects it.
	pendingAdd *newAccount
}

// NewBrowser builds the browser. initial is selected when it is listed
// (the first account otherwise); seed is the usage already known. allUsage
// loads every listed account's usage, not just the selected one's.
func NewBrowser(api agentsapi.Accounts, initial agents.AccountID, seed usagestore.Store, allUsage bool) Browser {
	b := Browser{
		api:         api,
		selected:    initial,
		describeSeq: map[agents.AccountID]int{},
		usage:       seed.Seeded(),
		usageLim:    usagestore.NewLimiter(usageSlots),
		allUsage:    allUsage,
	}
	b.n = int(seqBase.Add(1 << 20))
	b.latest.list = b.n
	return b
}

// Init lists accounts; the selected account's details load once the list is in.
func (b Browser) Init() tea.Cmd { return listCmd(b.api, b.latest.list, b.selected) }

// Accounts is the last good list.
func (b Browser) Accounts() []agentsapi.Account { return b.accounts }

// Listed reports that a list answered, with ListErr set when it failed.
func (b Browser) Listed() bool { return b.listed }

// ListErr is the sanitized list failure, or "".
func (b Browser) ListErr() string { return b.listErr }

// Selected is the selected account, or "".
func (b Browser) Selected() agents.AccountID { return b.selected }

// Usage is the usage this view knows, for rendering.
func (b Browser) Usage() usagestore.Store { return b.usage }

// Defaults is the selected account's last loaded defaults, and DefaultsErr why
// loading them failed.
func (b Browser) Defaults() agentsapi.Defaults { return b.defaults }
func (b Browser) DefaultsErr() string          { return b.defaultsErr }

// LoginCommand is the terminal sign-in flow for an account.
func (b Browser) LoginCommand(id agents.AccountID) *exec.Cmd { return b.api.LoginCommand(id) }

// Current is the selected account, or false when none is.
func (b Browser) Current() (agentsapi.Account, bool) {
	for _, a := range b.accounts {
		if a.ID == b.selected {
			return a, true
		}
	}
	return agentsapi.Account{}, false
}

// Ordered is the display and j/k order.
func (b Browser) Ordered() []agentsapi.Account { return Sidebar{Accounts: b.accounts}.Ordered() }

func (b Browser) orderedIDs() []agents.AccountID {
	ordered := b.Ordered()
	ids := make([]agents.AccountID, len(ordered))
	for i, a := range ordered {
		ids[i] = a.ID
	}
	return ids
}

// next advances the request counter and records it as the latest of a kind.
func (b *Browser) next(kind *int) int {
	b.n++
	*kind = b.n
	return b.n
}

// Relist re-reads the accounts and selects want when it is still listed.
func (b *Browser) Relist(want agents.AccountID) tea.Cmd {
	return listCmd(b.api, b.next(&b.latest.list), want)
}

// Expect remembers an account a terminal flow is creating, so the next list
// selects it.
func (b *Browser) Expect(provider agents.ProviderID, name string) {
	b.pendingAdd = &newAccount{provider: provider, name: name}
}

// Select moves the selection and loads everything shown for the account.
func (b Browser) Select(id agents.AccountID) (Browser, tea.Cmd) {
	if id != b.selected {
		b.resetSelection()
	}
	b.selected = id
	return b, b.load()
}

// resetSelection drops the per-account state when the selection moves, so
// nothing of the previous account shows under the new one.
func (b *Browser) resetSelection() {
	b.describing, b.describeErr = false, ""
	b.defaultsErr = ""
}

// load requests what the right column shows for the selected account. Usage
// goes through the shared fetch, which the backend answers from its snapshot
// while that is inside agents.UsageMaxAge.
func (b *Browser) load() tea.Cmd {
	id := b.selected
	if id == "" {
		return nil
	}
	b.describing, b.describeErr = true, ""
	seq := b.next(&b.latest.describe)
	b.describeSeq = maps.Clone(b.describeSeq)
	b.describeSeq[id] = seq
	return tea.Batch(
		describeCmd(b.api, id, seq),
		b.fetchUsage(id, false),
		defaultsCmd(b.api, id, b.next(&b.latest.defaults)),
	)
}

// fetchUsage starts a usage fetch for id and marks it refreshing.
func (b *Browser) fetchUsage(id agents.AccountID, refresh bool) tea.Cmd {
	var seq uint64
	b.usage, seq = b.usage.Begin(id)
	return usagestore.Fetch(context.Background(), b.api, b.usageLim, id, seq, refresh)
}

// RefreshUsage re-reads the selected account's usage from its provider.
func (b Browser) RefreshUsage() (Browser, tea.Cmd) {
	if b.selected == "" || b.usage[b.selected].Refreshing {
		return b, nil
	}
	return b, b.fetchUsage(b.selected, true)
}

// ReloadDefaults re-reads the selected account's defaults.
func (b Browser) ReloadDefaults() (Browser, tea.Cmd) {
	return b, defaultsCmd(b.api, b.selected, b.next(&b.latest.defaults))
}

// Update applies a backend result and reports what it changed. Messages that
// are not the browser's are ignored.
func (b Browser) Update(msg tea.Msg) (Browser, tea.Cmd, Change) {
	switch msg := msg.(type) {
	case ReloadMsg:
		return b, b.Relist(b.selected), 0
	case listMsg:
		return b.onList(msg)
	case describeMsg:
		return b.onDescribe(msg), nil, 0
	case sidebarInfoMsg:
		if msg.seq == b.latest.list {
			b.accounts = slices.Clone(b.accounts)
			for i := range b.accounts {
				id := b.accounts[i].ID
				if info, ok := msg.infos[id]; ok && id != b.selected && b.describeSeq[id] < msg.seq {
					b.accounts[i].Info = info
				}
			}
		}
		return b, nil, 0
	case usagestore.Result:
		var applied bool
		b.usage, applied = b.usage.Apply(msg)
		if !applied {
			return b, nil, 0
		}
		return b, usagestore.Observe(msg), 0
	case defaultsMsg:
		if msg.id != b.selected || msg.seq != b.latest.defaults {
			return b, nil, 0
		}
		b.defaults, b.defaultsErr = msg.defaults, ""
		if msg.err != nil {
			b.defaultsErr = usagestore.Sanitize(msg.err)
		}
		return b, nil, ChangedDefaults
	}
	return b, nil, 0
}

func (b Browser) onList(msg listMsg) (Browser, tea.Cmd, Change) {
	if msg.seq != b.latest.list {
		return b, nil, 0
	}
	b.listed = true
	if msg.err != nil {
		b.listErr = usagestore.Sanitize(msg.err)
		return b, nil, ChangedList
	}
	b.listErr = ""
	b.accounts = msg.accounts

	ids := b.orderedIDs()
	has := func(id agents.AccountID) bool { return id != "" && slices.Contains(ids, id) }

	pick := agents.AccountID("")
	switch {
	case b.pendingAdd != nil && b.findNew() != "":
		pick = b.findNew()
	case has(msg.want):
		pick = msg.want
	case has(b.selected):
		pick = b.selected
	case len(ids) > 0:
		pick = ids[0]
	}
	b.pendingAdd = nil

	if pick != b.selected {
		b.resetSelection()
	}
	b.selected = pick
	cmds := []tea.Cmd{b.load(), sidebarInfoCmd(b.api, b.accounts, b.selected, b.latest.list)}
	for _, id := range ids {
		if b.allUsage && id != b.selected && !b.usage[id].Have && !b.usage[id].Refreshing {
			cmds = append(cmds, b.fetchUsage(id, false))
		}
	}
	return b, tea.Batch(cmds...), ChangedList
}

// findNew is the account the last terminal add flow created, or "" when it
// did not get created (the user backed out).
func (b Browser) findNew() agents.AccountID {
	for _, a := range b.accounts {
		if b.pendingAdd != nil && a.Provider == b.pendingAdd.provider && a.Name == b.pendingAdd.name {
			return a.ID
		}
	}
	return ""
}

func (b Browser) onDescribe(msg describeMsg) Browser {
	if msg.id != b.selected || msg.seq != b.latest.describe {
		return b
	}
	b.describing = false
	if msg.err != nil {
		b.describeErr = usagestore.Sanitize(msg.err)
		return b
	}
	b.describeErr = ""
	b.accounts = slices.Clone(b.accounts)
	for i := range b.accounts {
		if b.accounts[i].ID == msg.id {
			b.accounts[i].Info = msg.info
		}
	}
	return b
}
