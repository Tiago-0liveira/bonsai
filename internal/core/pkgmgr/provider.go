package pkgmgr

// Context is the read-only discovery context passed to providers.
type Context struct {
	Location Location
	Options  Options
}

// Detection is the provider-specific result of passive detection.
type Detection struct {
	Applicable    bool
	ID            string
	Name          string
	Root          string
	WorkspaceRoot string
	Data          any
}

// ProviderInfo is the public summary of an applicable provider.
type ProviderInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Root          string `json:"root"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
}

// FingerprintInput is one content input that affects discovery.
type FingerprintInput struct {
	Name    string
	Content []byte
}

// Provider discovers commands without executing project scripts.
type Provider interface {
	ID() string
	Detect(ctx Context) (Detection, error)
	Commands(ctx Context, detection Detection) ([]Command, error)
	FingerprintInputs(ctx Context, detection Detection) ([]FingerprintInput, error)
}

// Options controls discovery and optional safe provider introspection.
type Options struct {
	UseCache         bool
	Refresh          bool
	AllowProviderCLI bool
}
