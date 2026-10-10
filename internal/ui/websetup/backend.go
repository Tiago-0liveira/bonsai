// Package websetup is the `bonsai web setup` terminal UI: the first-run
// wizard and the edit-mode dashboard. It only edits a draft; everything that
// touches disk, processes or the network goes through Backend, which
// internal/cli implements, and nothing is written before the user confirms
// on the Review screen.
package websetup

import (
	"context"
	"os/exec"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// Info is the context the screens describe.
type Info struct {
	Home             string // for ~ in paths
	ConfigPath       string
	StandardInterval string // "2 min"
	HostedURL        string // https://app.bonsai.dev/app
	DiscoveryDepth   int
	// Running is the current stack, or nil when bonsai web is not running.
	Running *setup.Running
	// InstallID marks this computer's webhooks; "" until live updates first
	// ran.
	InstallID string
}

type StepState int

const (
	StepRunning StepState = iota
	StepDone
	StepFailed
	StepSkipped
)

// Step is one line of the Apply screen. A step is reported again (same ID)
// when its state changes.
type Step struct {
	ID     string
	Label  string
	State  StepState
	Detail string
	Fix    string // set on failure: one concrete thing to do
}

// Result is how Apply ended.
type Result struct {
	OK   bool
	URL  string // the page bonsai web serves, when it is running
	Port int    // the API port, when bonsai web is running
	// WebhookPort is the live-updates receiver's port, 0 without one.
	WebhookPort int
	Opened      bool     // the browser was opened
	Notes       []string // extra lines for the Done screen
}

type Backend interface {
	// Checks runs the checks engine for cfg.
	Checks(ctx context.Context, cfg config.WebConfig) []checks.Check
	// ScanFolder canonicalizes path and lists the repositories discovery
	// finds under it. An unusable folder is an error that names the cause.
	ScanFolder(ctx context.Context, path string) (canonical string, repos []setup.Repo, err error)
	// LiveRepositories lists the GitHub repositories of repos (and the
	// configured ones) with whether the gh login administers each. It only
	// reads from GitHub.
	LiveRepositories(ctx context.Context, repos []setup.Repo, configured []string) []setup.LiveRepo
	// FixCommand builds the process an inline fix runs.
	FixCommand(fix checks.Fix) *exec.Cmd
	// Apply writes the plan and starts or restarts what it needs,
	// reporting each step. It is the only call that changes anything.
	Apply(ctx context.Context, plan setup.Plan, progress func(Step)) Result
	Open(url string) error
	Copy(text string) error
}
