package agym

import (
	"encoding/json"
	"time"
)

// SupportedProtocolMajor is the protocol version Bonsai supports.
const SupportedProtocolMajor = 1

// Capabilities supported or required by Bonsai.
const (
	CapProfilesRead = "profiles.read"
	CapUsageRead    = "usage.read"
	CapRunsHeadless = "runs.headless"
	CapRunsDurable  = "runs.durable"
	CapRunsStop     = "runs.stop"
	CapRunsEvents   = "runs.events"
	CapRunsFollow   = "runs.events.follow"
	CapProfilesAuto = "profiles.auto"
	CapLeasesRead   = "leases.read"
)

// Run states.
const (
	RunStateStarting       = "starting"
	RunStateRunning        = "running"
	RunStateStopping       = "stopping"
	RunStateSucceeded      = "succeeded"
	RunStateFailed         = "failed"
	RunStateStopped        = "stopped"
	RunStateNeedsAttention = "needs_attention"
)

// IsTerminal returns true if status represents an ended run.
func IsTerminal(status string) bool {
	switch status {
	case RunStateSucceeded, RunStateFailed, RunStateStopped:
		return true
	default:
		return false
	}
}

// ProtocolVersion tracks the integration wire protocol.
type ProtocolVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// Response is the standard envelope for one-shot AGYM CLI JSON responses.
type Response struct {
	Protocol ProtocolVersion `json:"protocol"`
	Ok       bool            `json:"ok"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    *ErrorPayload   `json:"error,omitempty"`
}

// ErrorPayload describes a structured error returned by AGYM.
type ErrorPayload struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Retryable         bool   `json:"retryable,omitempty"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
	RunID             string `json:"run_id,omitempty"`
}

// Limits defines boundaries reported by AGYM info.
type Limits struct {
	MaxEventBytes int `json:"max_event_bytes"`
	MaxPageEvents int `json:"max_page_events"`
}

// InfoData is the payload returned by `agym integration info`.
type InfoData struct {
	AGYMVersion             string   `json:"agym_version"`
	InstanceID              string   `json:"instance_id"`
	SupportedProtocolMajors []int    `json:"supported_protocol_majors"`
	Capabilities            []string `json:"capabilities"`
	Limits                  Limits   `json:"limits"`
}

// HasCapability checks whether a given capability flag is advertised.
func (i *InfoData) HasCapability(cap string) bool {
	if i == nil {
		return false
	}
	for _, c := range i.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// SupportsMajor checks if the given major version is supported.
func (i *InfoData) SupportsMajor(major int) bool {
	if i == nil {
		return false
	}
	for _, m := range i.SupportedProtocolMajors {
		if m == major {
			return true
		}
	}
	return false
}

// Profile represents a sanitized public AGYM profile projection.
type Profile struct {
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
	Readiness string `json:"readiness"` // ready, auth_required, busy, unavailable, unknown
	Reason    string `json:"reason,omitempty"`
}

// UsageWindow represents quota status in a specific time window.
type UsageWindow struct {
	Name      string     `json:"name"`
	Remaining *float64   `json:"remaining,omitempty"` // 0.0 to 1.0 (fraction)
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}

// Usage represents quota and model availability for a profile.
type Usage struct {
	ProfileID  string        `json:"profile_id"`
	ModelGroup string        `json:"model_group,omitempty"`
	Windows    []UsageWindow `json:"windows"`
	ObservedAt time.Time     `json:"observed_at"`
	Stale      bool          `json:"stale"`
	Error      string        `json:"error,omitempty"`
}

// WorkspaceIdentityPayload identifies the target workspace on the wire.
type WorkspaceIdentityPayload struct {
	Key           string `json:"key"`
	Cwd           string `json:"cwd"`
	RepositoryKey string `json:"repository_key,omitempty"`
}

// StartRequest is sent to AGYM over stdin to request a headless run.
type StartRequest struct {
	RequestID        string                   `json:"request_id"`
	Client           string                   `json:"client"`
	ClientID         string                   `json:"client_id"`
	Workspace        WorkspaceIdentityPayload `json:"workspace"`
	Profile          string                   `json:"profile"`
	Task             string                   `json:"task"`
	Execution        string                   `json:"execution"`
	PermissionPolicy string                   `json:"permission_policy"`
}

// Run represents a durable AGYM task run.
type Run struct {
	RunID           string                   `json:"run_id"`
	RequestID       string                   `json:"request_id"`
	Client          string                   `json:"client,omitempty"`
	ClientID        string                   `json:"client_id,omitempty"`
	Workspace       WorkspaceIdentityPayload `json:"workspace"`
	SelectedProfile string                   `json:"selected_profile"`
	LeaseID         string                   `json:"lease_id,omitempty"`
	Task            string                   `json:"task"`
	Status          string                   `json:"status"`
	SessionID       string                   `json:"session_id,omitempty"`
	Error           *ErrorPayload            `json:"error,omitempty"`
	ExitCode        *int                     `json:"exit_code,omitempty"`
	LastSeq         uint64                   `json:"last_seq"`
	CreatedAt       time.Time                `json:"created_at"`
	StartedAt       *time.Time               `json:"started_at,omitempty"`
	FinishedAt      *time.Time               `json:"finished_at,omitempty"`
	ElapsedSeconds  float64                  `json:"elapsed_seconds,omitempty"`
}

// Lease represents profile lease metadata.
type Lease struct {
	LeaseID     string     `json:"lease_id"`
	ProfileID   string     `json:"profile_id"`
	OwningRunID string     `json:"owning_run_id"`
	State       string     `json:"state"`
	AcquiredAt  time.Time  `json:"acquired_at"`
	HeartbeatAt time.Time  `json:"heartbeat_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Event represents a single streamable or stored run event.
type Event struct {
	RunID     string          `json:"run_id"`
	Seq       uint64          `json:"seq"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"` // e.g. "output", "status", "profile", "terminal"
	Payload   json.RawMessage `json:"payload"`
}

// OutputPayload is the payload of an "output" event.
type OutputPayload struct {
	Stream string `json:"stream"` // "stdout" or "stderr"
	Text   string `json:"text"`
}

// EventPage represents a paginated set of run events.
type EventPage struct {
	RunID             string  `json:"run_id"`
	Events            []Event `json:"events"`
	NextCursor        uint64  `json:"next_cursor"`
	HasMore           bool    `json:"has_more"`
	OldestRetainedSeq uint64  `json:"oldest_retained_seq"`
	Snapshot          *Run    `json:"snapshot,omitempty"`
}
