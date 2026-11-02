// Package agentterminal runs provider processes in PTYs with bounded terminal
// streams; package agenthost holds each agent session in its own process.
package agentterminal

import (
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const OutputLimit = 2 << 20
const InputLimit = 64 << 10

type Summary struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	WorktreeID  string            `json:"worktree_id"`
	AccountID   agents.AccountID  `json:"account_id"`
	Provider    agents.ProviderID `json:"provider"`
	ProfileName string            `json:"profile_name"`
	Name        string            `json:"name"`
	State       string            `json:"state"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	EndedAt     *time.Time        `json:"ended_at,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	Error       string            `json:"error,omitempty"`
	// ProviderSessionID is the provider's own session ID, for a later resume.
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	// Model and Options are the accepted launch choices (Options keyed by the
	// start request field, such as effort). The prompt is never recorded.
	Model   string            `json:"model,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	// Origin is the client that started the session (web, tui or cli). It
	// decides what stopping the web server may stop: only web-started
	// sessions; empty means a holder from before the field existed, which
	// the web started.
	Origin string `json:"origin,omitempty"`
	// OperationID and Task identify the durable operation that asked for the
	// session, so a restarted client can match a holder to its request.
	OperationID string          `json:"operation_id,omitempty"`
	Task        *agents.TaskRef `json:"task,omitempty"`
	// Activity is what the agent is doing. It is set only while State is
	// running and cleared on every lifecycle change away from it.
	Activity *agentactivity.Activity `json:"activity,omitempty"`
}

func (s Summary) Active() bool {
	return s.State == "starting" || s.State == "running" || s.State == "stopping"
}

// Frame is one agent terminal message; its status is the session summary.
type Frame = StreamFrame[Summary]

func Dimensions(cols, rows int) bool { return cols >= 1 && cols <= 500 && rows >= 1 && rows <= 500 }
