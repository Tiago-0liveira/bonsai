package gym

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

const (
	CurrentSchemaVersion = 1

	SubmissionPending      = "pending"
	SubmissionAcknowledged = "acknowledged"
	SubmissionRejected     = "rejected"
)

// WorkspaceIdentity tracks identity across paths and gitdirs.
type WorkspaceIdentity struct {
	RepositoryID string `json:"repository_id"`
	WorktreeID   string `json:"worktree_id"`
	GitDir       string `json:"git_dir"` // canonical per-worktree git dir
	Path         string `json:"path"`    // last validated canonical working directory
}

// RunBinding persists durable relationships between a Bonsai worktree and an AGYM run.
type RunBinding struct {
	SchemaVersion  int               `json:"schema_version"`
	Workspace      WorkspaceIdentity `json:"workspace"`
	AGYMInstanceID string            `json:"agym_instance_id"`
	RequestID      string            `json:"request_id"`
	RunID          string            `json:"run_id,omitempty"`
	LeaseID        string            `json:"lease_id,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	Submission     string            `json:"submission"` // pending, acknowledged, rejected
}

// AgentView represents in-memory presentation state for an agent.
type AgentView struct {
	Binding    RunBinding
	Run        *agym.Run
	Usage      *agym.Usage
	LastSeq    uint64
	ObservedAt time.Time
	Stale      bool
	OutputTail []string
	Error      string
}

// ClientConfig persists the client-wide installation ID.
type ClientConfig struct {
	ClientID string `json:"client_id"`
}

// NewUUID generates a version 4 UUID using crypto/rand.
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
