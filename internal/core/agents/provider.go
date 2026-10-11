package agents

import (
	"context"
	"io"
	"time"
)

type Capabilities struct {
	Interactive            bool
	Usage                  bool
	MultiAccount           bool
	ConcurrentSameAccount  bool
	ConcurrentCrossAccount bool
}

// SetupOptions carries provider-defined account setup choices. Providers reject
// options they do not support instead of ignoring them.
type SetupOptions struct {
	AuthMode string    // provider-defined; "" selects the provider default
	Seed     *bool     // nil selects the provider default
	SeedFrom string    // optional seed source directory
	Secret   io.Reader // token input (stdin or tests); never logged
}

// Empty reports whether no setup option was supplied.
func (o SetupOptions) Empty() bool {
	return o.AuthMode == "" && o.Seed == nil && o.SeedFrom == "" && o.Secret == nil
}

type SetupRequest struct {
	Account    Account
	RuntimeDir string
	HomeDir    string
	Options    SetupOptions
}

type SetupResult struct {
	Account Account
}

// LaunchOptions are per-launch choices from the UI or CLI. Providers map them
// to their own flags and reject the ones they do not support.
type LaunchOptions struct {
	Model          string
	Prompt         string
	DisplayName    string
	FullAccess     *bool  // Antigravity
	PermissionMode string // Claude: default|acceptEdits|plan|auto|dontAsk|bypassPermissions
	Effort         string // Claude: low|medium|high|xhigh|max
	Sandbox        string // Codex: read-only|workspace-write|danger-full-access|bypass
	Approval       string // Codex: untrusted|on-request|never
}

type PrepareSessionRequest struct {
	Account Account
	Session Session
	Args    []string
	Launch  LaunchOptions
}

type PreparedSession struct {
	Executable string
	Args       []string
	Dir        string
	EnvSet     map[string]string
	EnvUnset   []string
	// EnvUnsetPrefixes removes every inherited variable whose name starts with
	// one of these prefixes. EnvSet still wins.
	EnvUnsetPrefixes []string
	// ProviderSessionID is the provider's own session identifier, when it has one.
	ProviderSessionID string
}

// Environment builds the process environment from base.
func (p PreparedSession) Environment(base []string) []string {
	return buildEnvironment(base, p.EnvSet, p.EnvUnset, p.EnvUnsetPrefixes)
}

type FinalizeSessionRequest struct {
	Account Account
	Session Session
}

type Provider interface {
	ID() ProviderID
	Capabilities() Capabilities
	SetupAccount(context.Context, SetupRequest) (SetupResult, error)
	PrepareSession(context.Context, PrepareSessionRequest) (PreparedSession, error)
	FinalizeSession(context.Context, FinalizeSessionRequest) error
	Usage(context.Context, Account, UsageOptions) (UsageSnapshot, error)
}

// Optional provider interfaces, discovered with type assertions.

type Availability struct {
	Available bool
	Reason    string
	Version   string
}

// Describer names a provider and reports whether it can run on this host.
type Describer interface {
	Label() string
	Availability(context.Context) Availability
}

// LaunchValidator rejects launch options synchronously, before any session exists.
type LaunchValidator interface {
	ValidateLaunch(Account, LaunchOptions) error
}

// AccountInfo holds display-safe account fields. It never carries tokens,
// paths or raw settings.
type AccountInfo struct {
	AuthMode string
	Identity string
	Options  map[string]any
	Warnings []string
}

type AccountDescriber interface {
	DescribeAccount(context.Context, Account) AccountInfo
}

// AccountRemover releases provider-side state before Bonsai deletes the account.
// Failures are reported as warnings and never block removal.
type AccountRemover interface {
	RemoveAccount(context.Context, Account) error
}

// LaunchField describes one launch option a provider accepts, so the launch
// dialog can render it without knowing the provider. ID is the start request's
// JSON field name, except for presets: a select whose options carry Set sends
// those fields instead of its own.
type LaunchField struct {
	ID      string        `json:"id"`
	Label   string        `json:"label"`
	Kind    string        `json:"kind"` // LaunchFieldSelect or LaunchFieldToggle
	Options []FieldOption `json:"options,omitempty"`
	// Default is the initially selected value; empty selects the first option.
	// A toggle starts from the profile's option of the same ID instead.
	Default string `json:"default,omitempty"`
	// PerModel means the values come from the chosen model's Efforts; Options
	// remain the fallback for typed or unlisted models.
	PerModel bool   `json:"per_model,omitempty"`
	Help     string `json:"help,omitempty"`
}

const (
	LaunchFieldSelect = "select"
	LaunchFieldToggle = "toggle"
	// ToneDanger marks options that let the agent act without asking.
	ToneDanger = "danger"
)

// FieldOption is one value of a launch field. A toggle has two options, for
// "false" and "true", which name both states.
type FieldOption struct {
	Value string            `json:"value"`
	Label string            `json:"label"`
	Tone  string            `json:"tone,omitempty"`
	Help  string            `json:"help,omitempty"` // shown when a danger option is chosen
	Set   map[string]string `json:"set,omitempty"`  // preset: request fields this option sends
}

// LaunchSchema declares the launch options a provider accepts, in display order.
type LaunchSchema interface {
	LaunchFields() []LaunchField
}

// ModelOption is one model a provider can launch with. Source is "alias" for a
// provider-defined shortcut and "api" for a model the provider's service listed.
type ModelOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	// Efforts are the reasoning efforts this model accepts, when they vary by model.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	// Default marks the model the provider uses when none is chosen.
	Default bool `json:"default,omitempty"`
}

// ModelLister suggests models for the launch dialog. Suggestions are advice only:
// any model the provider accepts can still be typed. Implementations are best
// effort and should fall back to a static list instead of failing.
type ModelLister interface {
	Models(context.Context, Account) ([]ModelOption, error)
}

type UsagePolicy interface {
	UsageTTL() time.Duration
}
