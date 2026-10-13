package agents

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
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
	IO         SetupIO
}

type SetupResult struct {
	Account Account
	// Warnings are shown once after setup, such as a login another profile
	// already uses. The CLI prints each as a "warning: " line.
	Warnings []string
}

// SetupIO is how one setup or sign-in talks to the user. The CLI uses the
// terminal; the web runs login commands in a job terminal and asks for secrets
// in the browser. Providers never read the process TTY or write to stdio during
// setup: everything goes through these.
type SetupIO struct {
	Launcher Launcher       // runs interactive login commands
	Out      io.Writer      // progress lines, such as the seed report
	Prompt   SecretPrompter // hidden input for a pasted key or token
}

// SecretPrompter asks the user for a secret. label names it ("OpenAI API key").
// The answer is never echoed, logged or stored by the prompter.
type SecretPrompter interface {
	Secret(ctx context.Context, label string) (string, error)
}

// ReauthRequest signs an existing account in again. RuntimeDir and HomeDir
// belong to a throwaway session, as for setup. Options.AuthMode "" keeps the
// account's current mode.
type ReauthRequest struct {
	Account    Account
	RuntimeDir string
	HomeDir    string
	Options    SetupOptions
	IO         SetupIO
}

// AccountAuthenticator signs an existing account in again. The result's
// Account carries the settings to store (a changed auth mode); identity, name
// and timestamps are kept by the caller.
type AccountAuthenticator interface {
	Reauthenticate(context.Context, ReauthRequest) (SetupResult, error)
}

// SetupSpec declares how a provider's accounts are created, so the web can
// render the setup form without knowing the provider.
type SetupSpec struct {
	AuthModes []AuthModeOption `json:"auth_modes,omitempty"` // empty: one implicit mode
	Seed      *SeedSpec        `json:"seed,omitempty"`       // nil: no seeding
	Notes     []string         `json:"notes,omitempty"`
}

// AuthModeOption is one value of SetupOptions.AuthMode.
type AuthModeOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	// Recommended is decided per host, such as device code on WSL or SSH.
	Recommended bool        `json:"recommended,omitempty"`
	Secret      *SecretSpec `json:"secret,omitempty"` // the mode takes a pasted key or token
}

// SecretSpec describes a pasted secret. Optional means the provider can get it
// another way when none is given (Claude runs setup-token, then prompts).
type SecretSpec struct {
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

// SeedSpec says where a new profile is seeded from, home-relative for display
// ("~/.codex"). It is never an input: the web only toggles seeding.
type SeedSpec struct {
	Source string `json:"source"`
}

// SetupSchema declares a provider's setup choices.
type SetupSchema interface {
	SetupSpec(context.Context) SetupSpec
}

// LaunchOptions are per-launch choices from the UI or CLI. Providers map them
// to their own flags and reject the ones they do not support.
type LaunchOptions struct {
	Model          string
	Prompt         string
	DisplayName    string
	FullAccess     *bool  // Antigravity
	PermissionMode string // Claude: default|acceptEdits|plan|auto|dontAsk|bypassPermissions
	Effort         string // provider-defined level, from the chosen model's Efforts
	Sandbox        string // Codex: read-only|workspace-write|danger-full-access|bypass
	Approval       string // Codex: untrusted|on-request|never
}

// Values returns the accepted launch choices other than the model, keyed by
// their start request field (effort, permission_mode, sandbox, approval,
// full_access). Empty values are left out; the prompt and display name never
// appear. Nil when there are none.
func (o LaunchOptions) Values() map[string]string {
	values := map[string]string{}
	for key, value := range map[string]string{"permission_mode": o.PermissionMode, "effort": o.Effort, "sandbox": o.Sandbox, "approval": o.Approval} {
		if value != "" {
			values[key] = value
		}
	}
	if o.FullAccess != nil {
		values["full_access"] = strconv.FormatBool(*o.FullAccess)
	}
	if len(values) == 0 {
		return nil
	}
	return values
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
	// Hint is a short next step when the provider is not installed, such as
	// its install command.
	Hint string
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
	Kind    string        `json:"kind"` // LaunchFieldSelect, LaunchFieldToggle or (profile fields only) LaunchFieldModel
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
	// LaunchFieldModel is a free model name with ModelLister suggestions. Only
	// ProfileFields use it; the launch dialog has its own model input.
	LaunchFieldModel = "model"
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
// provider-defined shortcut, "api" for a model the provider's service listed and
// "bundled" for one read from the provider's own installed binary.
type ModelOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	// Efforts are the reasoning efforts this model accepts, when they vary by model.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	// NoEffort means the provider says this model has no effort control, as
	// opposed to Efforts being empty because nothing is known about it.
	NoEffort bool `json:"no_effort,omitempty"`
	// Default marks the model the provider uses when none is chosen.
	Default bool `json:"default,omitempty"`
}

// ModelLister suggests models for the launch dialog. Suggestions are advice only:
// any model the provider accepts can still be typed. Implementations are best
// effort and should fall back to a static list instead of failing.
type ModelLister interface {
	Models(context.Context, Account) ([]ModelOption, error)
}

// ProfileEditor declares and changes a profile's launch defaults, the values a
// launch uses when it does not choose its own. Fields reuse the LaunchField
// schema (plus LaunchFieldModel); an empty value means the provider default.
type ProfileEditor interface {
	ProfileFields() []LaunchField
	// ProfileDefaults returns the profile's current value of each field it sets,
	// keyed by field ID. Unset fields and values no field option describes are
	// left out.
	ProfileDefaults(Account) map[string]string
	// UpdateProfile merges the changes, keyed by field ID (a nil or empty value
	// unsets the field), into the account's settings and returns the new
	// settings. It validates the result with the provider's own settings parser
	// and never touches sign-in settings.
	UpdateProfile(Account, map[string]*string) (json.RawMessage, error)
}

type UsagePolicy interface {
	UsageTTL() time.Duration
}

// UsageLayout picks the headline limits of a provider's usage snapshot.
type UsageLayout interface {
	UsageHeadline(UsageSnapshot) UsageHeadline
}

// UsageNoter adds a short caveat to a provider's usage, such as where it is read from.
type UsageNoter interface {
	UsageNote() string
}
