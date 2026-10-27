// Package usagestore is the one place the TUI fetches and holds account
// usage. Every view (the pane, the Agents tab strip, the profiles modal, the
// quick overlay and the launch dialog) fetches through Fetch and changes its
// entries only through Store.Apply, so one account never shows two truths.
package usagestore

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// callTimeout bounds every provider call so a hung provider cannot leave a
// view waiting forever.
const callTimeout = 15 * time.Second

// Entry is the last usage known for an account. A failed refresh keeps the
// last good data and marks it stale instead of clearing it.
type Entry struct {
	Usage agentsapi.Usage // Usage.Stale and Usage.Err carry a failed refresh
	Have  bool
	// Err is why there is no data; it is only set while Have is false.
	Err        string
	Refreshing bool
	// Seq is the latest fetch issued for the account; older answers are dropped.
	Seq uint64
}

// Stale reports that the shown numbers are the last good ones after a failure.
func (e Entry) Stale() bool { return e.Have && e.Usage.Stale }

// Message is the failure text to show: the stale note while data is kept, the
// error alone otherwise.
func (e Entry) Message() string {
	if e.Have {
		return e.Usage.Err
	}
	return e.Err
}

// Store holds one Entry per account. It is a value: Apply and the other
// writers return a changed copy, so a Store shared by value never changes
// under a reader.
type Store map[agents.AccountID]Entry

// seqs numbers fetches across every store, so an answer for a closed view
// can never match a newer view's request.
var seqs atomic.Uint64

// Begin marks the account as refreshing under a new sequence number. Only the
// answer carrying that number is applied.
func (s Store) Begin(id agents.AccountID) (Store, uint64) {
	out := s.clone()
	e := out[id]
	e.Seq, e.Refreshing = seqs.Add(1), true
	out[id] = e
	return out, e.Seq
}

// Seeded copies the data of s without its in-flight state, for a view that
// starts from what is already known.
func (s Store) Seeded() Store {
	out := make(Store, len(s))
	for id, e := range s {
		if e.Have {
			out[id] = Entry{Usage: e.Usage, Have: true}
		}
	}
	return out
}

// Retain drops the entries of accounts not in ids.
func (s Store) Retain(ids []agents.AccountID) Store {
	out := make(Store, len(ids))
	for _, id := range ids {
		if e, ok := s[id]; ok {
			out[id] = e
		}
	}
	return out
}

func (s Store) clone() Store {
	out := make(Store, len(s)+1)
	for id, e := range s {
		out[id] = e
	}
	return out
}

// Result is the answer to one fetch or stored read. Observed re-sends an
// applied Result to the root model (see Observe).
type Result struct {
	Account agents.AccountID
	// Seq is the number Begin returned for a fetch; 0 means the result is not
	// tied to a request (a stored read, or an observation).
	Seq   uint64
	Usage agentsapi.Usage
	Err   error
	// Stored marks a disk read; Found is false when nothing was stored.
	Stored bool
	Found  bool
}

// Observation tells the root model that a view applied a Result, so the shared
// store, the pane and the strip follow without fetching again.
type Observation Result

// Observe is the command a view returns after Apply accepted r.
func Observe(r Result) tea.Cmd {
	r.Seq = 0
	return func() tea.Msg { return Observation(r) }
}

// Apply is the one reducer. It reports whether the result changed the store;
// a dropped result must not be observed or redrawn.
//
//   - A fetch result whose Seq is not the account's latest is late: dropped.
//   - A stored read never overwrites an entry that already has data.
//   - Older data (earlier FetchedAt) never overwrites newer data; a result
//     without a FetchedAt carries no age and is taken as is.
//   - A failure keeps the last good data, marked stale.
func (s Store) Apply(r Result) (Store, bool) {
	e, known := s[r.Account]
	if r.Stored {
		if (known && e.Have) || !r.Found || r.Err != nil {
			return s, false
		}
		out := s.clone()
		e.Usage, e.Have, e.Err = r.Usage, true, ""
		out[r.Account] = e
		return out, true
	}
	if r.Seq != 0 && r.Seq != e.Seq {
		return s, false
	}
	out := s.clone()
	if r.Seq != 0 {
		e.Refreshing = false
	}
	switch {
	case r.Err != nil:
		if e.Have {
			e.Usage.Stale, e.Usage.Err = true, Sanitize(r.Err)
		} else {
			e.Err = Sanitize(r.Err)
		}
	case e.Have && !r.Usage.FetchedAt.IsZero() && r.Usage.FetchedAt.Before(e.Usage.FetchedAt):
		// An older snapshot is not news; only the in-flight state changes.
	default:
		e.Usage, e.Have, e.Err = r.Usage, true, ""
	}
	out[r.Account] = e
	return out, true
}

// Limiter bounds the provider calls a group of fetches may have in flight.
type Limiter chan struct{}

// NewLimiter allows n concurrent calls.
func NewLimiter(n int) Limiter { return make(Limiter, n) }

// Fetch asks the backend for an account's usage. It is the only way the TUI
// reads usage from a provider: refresh=false takes the stored snapshot while
// it is inside agents.UsageMaxAge, refresh=true bypasses it. A nil limiter
// does not bound the call.
func Fetch(ctx context.Context, api agentsapi.Accounts, lim Limiter, id agents.AccountID, seq uint64, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if lim != nil {
			select {
			case lim <- struct{}{}:
				defer func() { <-lim }()
			case <-ctx.Done():
				return Result{Account: id, Seq: seq, Err: ctx.Err()}
			}
		}
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		u, err := api.Usage(ctx, id, refresh)
		return Result{Account: id, Seq: seq, Usage: u, Err: err}
	}
}

// Stored reads the persisted snapshot without calling the provider, to paint
// numbers before the first fetch answers.
func Stored(ctx context.Context, api agentsapi.Accounts, id agents.AccountID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		u, ok, err := api.UsageStored(ctx, id)
		return Result{Account: id, Usage: u, Err: err, Stored: true, Found: ok}
	}
}

// maxErrLen keeps one error from filling the column.
const maxErrLen = 120

// pathLike matches absolute paths and URL tails: provider errors often quote
// the config dir, which the UI must never render.
var pathLike = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\\/][^\s\\/:"']+){2,}`)

// Sanitize turns a backend error into one short display-safe line. Providers
// already keep secrets out of their errors; this only strips control bytes,
// paths and length.
func Sanitize(err error) string {
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
