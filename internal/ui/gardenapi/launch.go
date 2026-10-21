package gardenapi

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
)

// LaunchPreviewRequest asks what starting an implementation locally would do.
// Empty Branch, BaseRef, RemoteName, Name and dimensions take the backend's
// documented defaults.
type LaunchPreviewRequest struct {
	IssueID                     uint64
	Branch, BaseRef, RemoteName string
	AccountID                   agents.AccountID
	Launch                      agents.LaunchOptions
	Name                        string
	Cols, Rows                  int
}

// LaunchPreviewView is what a confirmation covers, handed back unchanged to
// StartLaunch.
type LaunchPreviewView struct {
	Stamp   Stamp
	IssueID uint64
	Preview launch.Preview
}

// LaunchStartRequest confirms a preview. EscalationConfirmed is the separate
// acknowledgment of a dangerous native permission mode.
type LaunchStartRequest struct {
	Preview             LaunchPreviewView
	Confirmed           bool
	EscalationConfirmed bool
}

// LaunchAttemptView is one local attempt. After a failure that follows the
// reservation it is still populated, so the UI keeps the identity of what exists.
type LaunchAttemptView struct {
	Stamp   Stamp
	IssueID uint64
	Attempt launch.Attempt
}

type LaunchAttemptsView struct {
	Stamp    Stamp
	IssueID  uint64
	Attempts []launch.Attempt
}

type LaunchCleanupView struct {
	Stamp   Stamp
	IssueID uint64
	Preview launch.CleanupPreview
}

// LaunchBackend is the optional local implementation launch. Garden backends that
// cannot run it simply do not implement it; Backend is not widened.
type LaunchBackend interface {
	PreviewLaunch(context.Context, LaunchPreviewRequest) (LaunchPreviewView, error)
	StartLaunch(context.Context, LaunchStartRequest) (LaunchAttemptView, error)
	LaunchAttempts(context.Context, uint64) (LaunchAttemptsView, error)
	RetryLaunch(context.Context, launch.RetryRequest) (LaunchAttemptView, error)
	StopLaunch(context.Context, string) (LaunchAttemptView, error)
	PublishLaunch(context.Context, launch.PublishRequest) (LaunchAttemptView, error)
	PreviewLaunchCleanup(context.Context, string) (LaunchCleanupView, error)
	CleanupLaunch(context.Context, LaunchCleanupView, bool) (LaunchAttemptView, error)
}

const (
	CodeLaunchBlocked       Code = "launch-blocked"
	CodeLaunchChanged       Code = "launch-changed"
	CodeLaunchUnknown       Code = "launch-unknown"
	CodeLaunchActive        Code = "launch-active"
	CodeLaunchCleanupUnsafe Code = "launch-cleanup-unsafe"
)
