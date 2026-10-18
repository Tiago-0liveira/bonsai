package accounts

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// callTimeout bounds every backend call so a hung provider cannot leave the
// modal waiting forever.
const callTimeout = 15 * time.Second

// savedFor is how long "Saved" replaces the unsaved marker.
const savedFor = 3 * time.Second

// Each result carries the sequence number of the request that produced it.
// Update drops a result whose sequence is not the latest of its kind, and one
// for an account that is no longer selected, so a slow response can never
// overwrite newer state.
type (
	listMsg struct {
		seq      int
		accounts []agentsapi.Account
		err      error
		want     agents.AccountID
	}
	sidebarInfoMsg struct {
		seq   int
		infos map[agents.AccountID]agents.AccountInfo
	}
	describeMsg struct {
		id   agents.AccountID
		seq  int
		info agents.AccountInfo
		err  error
	}
	usageMsg struct {
		id    agents.AccountID
		seq   int
		usage agentsapi.Usage
		err   error
	}
	defaultsMsg struct {
		id       agents.AccountID
		seq      int
		defaults agentsapi.Defaults
		err      error
	}
	savedMsg struct {
		id  agents.AccountID
		seq int
		err error
	}
	removedMsg struct {
		id       agents.AccountID
		seq      int
		neighbor agents.AccountID
		err      error
	}
	savedClearMsg struct {
		id  agents.AccountID
		seq int
	}
)

func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

func listCmd(api agentsapi.Accounts, seq int, want agents.AccountID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		accounts, err := api.List(ctx)
		return listMsg{seq: seq, accounts: accounts, err: err, want: want}
	}
}

func describeCmd(api agentsapi.Accounts, id agents.AccountID, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		info, err := api.Describe(ctx, id)
		return describeMsg{id: id, seq: seq, info: info, err: err}
	}
}

// Load plans for the unselected profiles without delaying the selected details
// or starting an unbounded number of provider status commands.
func sidebarInfoCmd(api agentsapi.Accounts, accounts []agentsapi.Account, selected agents.AccountID, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		infos := make(map[agents.AccountID]agents.AccountInfo)
		var mu sync.Mutex
		var wg sync.WaitGroup
		slots := make(chan struct{}, 3)
		for _, a := range accounts {
			if a.ID == selected {
				continue
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return sidebarInfoMsg{seq: seq, infos: infos}
			}
			wg.Go(func() {
				defer func() { <-slots }()
				if info, err := api.Describe(ctx, a.ID); err == nil {
					mu.Lock()
					infos[a.ID] = info
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		return sidebarInfoMsg{seq: seq, infos: infos}
	}
}

func usageCmd(api agentsapi.Accounts, id agents.AccountID, seq int, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		u, err := api.Usage(ctx, id, refresh)
		return usageMsg{id: id, seq: seq, usage: u, err: err}
	}
}

func defaultsCmd(api agentsapi.Accounts, id agents.AccountID, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		d, err := api.Defaults(ctx, id)
		return defaultsMsg{id: id, seq: seq, defaults: d, err: err}
	}
}

func saveCmd(api agentsapi.Accounts, id agents.AccountID, seq int, changes map[string]*string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		return savedMsg{id: id, seq: seq, err: api.UpdateDefaults(ctx, id, changes)}
	}
}

func removeCmd(api agentsapi.Accounts, id agents.AccountID, seq int, neighbor agents.AccountID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := callCtx()
		defer cancel()
		return removedMsg{id: id, seq: seq, neighbor: neighbor, err: api.Remove(ctx, id)}
	}
}

func savedClearCmd(id agents.AccountID, seq int) tea.Cmd {
	return tea.Tick(savedFor, func(time.Time) tea.Msg { return savedClearMsg{id: id, seq: seq} })
}

// emit hands a message to the parent through the normal command path.
func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// maxErrLen keeps one error from filling the column.
const maxErrLen = 120

// pathLike matches absolute paths and URL tails: provider errors often quote
// the config dir, which the modal must never render.
var pathLike = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\\/][^\s\\/:"']+){2,}`)

// sanitize turns a backend error into one short display-safe line. Providers
// already keep secrets out of their errors; this only strips control bytes,
// paths and length.
func sanitize(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	s := strings.Join(strings.Fields(ansi.Strip(err.Error())), " ")
	s = pathLike.ReplaceAllString(s, "<path>")
	if r := []rune(s); len(r) > maxErrLen {
		s = string(r[:maxErrLen-1]) + "…"
	}
	return s
}
