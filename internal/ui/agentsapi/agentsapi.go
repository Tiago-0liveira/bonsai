// Package agentsapi is the contract between the TUI and the agent backend.
// The TUI codes against these interfaces only, so it never imports the agent
// runtime or providers and every UI test can run on the fake package.
package agentsapi

import (
	"context"
	"os/exec"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Session is one hosted agent as the TUI sees it: the holder's summary plus
// the directory it runs in, which is how the TUI matches it to a worktree.
type Session struct {
	agentterminal.Summary
	WorkDir string // cleaned absolute path the holder runs in
}

// StartRequest is what the TUI's launch dialog asks the backend to start.
type StartRequest struct {
	ProjectID, WorktreeID string
	WorkDir               string
	AccountID             agents.AccountID
	Name                  string
	Cols, Rows            int
	Launch                agents.LaunchOptions
}

// AttachOptions picks how an attachment takes part in writing, since viewing
// a session must not steal control from another client.
type AttachOptions struct {
	ReadOnly   bool   // observe only
	Take       bool   // take control immediately
	Offset     uint64 // resume after this stream offset (0 = full replay)
	Generation string // holder generation the offset belongs to
}

// Stream is one terminal attachment. Frames closes when the attachment ends.
type Stream interface {
	Frames() <-chan agentterminal.Frame
	Writer() bool
	Size() (cols, rows int) // holder PTY size at attach and after resize frames
	Input(p []byte) error
	Resize(cols, rows int) error
	Take() error
	Close() error
}

// Sessions lists, starts, stops and attaches to agent holders, shared with
// the web UI and the bonsai agents CLI.
type Sessions interface {
	Supported() bool                             // false on Windows / no holder paths
	List(ctx context.Context) ([]Session, error) // live + recently ended records
	// Changes yields after any record create/update/remove, coalesced; closes with ctx.
	Changes(ctx context.Context) (<-chan struct{}, error)
	Start(ctx context.Context, req StartRequest) (Session, error)
	Stop(ctx context.Context, id string) error
	Attach(ctx context.Context, id string, opts AttachOptions) (Stream, error)
}

// Account is an agent profile with the details the accounts modal shows.
type Account struct {
	agents.Account
	Info    agents.AccountInfo // identity, plan, auth; zero until described
	Running int                // active sessions on this account
}

// Usage is an account's usage with its freshness, so a failed refresh can
// keep showing the last good data instead of clearing it.
type Usage struct {
	Snapshot  agents.UsageSnapshot
	Headline  agents.UsageHeadline
	FetchedAt time.Time
	Stale     bool   // last refresh failed; data is the last good snapshot
	Err       string // last refresh error, sanitized
}

// Defaults is what the defaults form needs to edit an account's launch
// defaults without knowing the provider.
type Defaults struct {
	Fields []agents.LaunchField
	// LaunchFields are the provider's LaunchFields; the new agent dialog edits
	// these, not Fields (the profile defaults).
	LaunchFields []agents.LaunchField
	Models       []agents.ModelOption // from ModelLister.Models
	Values       map[string]string    // current per-account defaults
}

// Accounts reads and edits agent profiles. Adding an account and signing in
// again stay in the CLI flows, run in a real terminal via tea.ExecProcess.
type Accounts interface {
	List(ctx context.Context) ([]Account, error) // fast: no DescribeAccount
	Describe(ctx context.Context, id agents.AccountID) (agents.AccountInfo, error)
	Usage(ctx context.Context, id agents.AccountID, refresh bool) (Usage, error)
	Defaults(ctx context.Context, id agents.AccountID) (Defaults, error)
	UpdateDefaults(ctx context.Context, id agents.AccountID, values map[string]*string) error
	Remove(ctx context.Context, id agents.AccountID) error
	// Commands for tea.ExecProcess: the existing CLI flows in a real terminal.
	AddCommand(provider agents.ProviderID, name string) *exec.Cmd // bonsai agent account add
	LoginCommand(id agents.AccountID) *exec.Cmd                   // bonsai agent account login
}

// Backend is everything the TUI needs from agents, injected by main.
type Backend struct {
	Sessions Sessions
	Accounts Accounts
}
