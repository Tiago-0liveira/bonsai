package pkgmgr

// Project is the normalized result of command discovery for one directory.
type Project struct {
	Location    Location       `json:"location"`
	Providers   []ProviderInfo `json:"providers"`
	Commands    []Command      `json:"commands"`
	Fingerprint string         `json:"fingerprint"`
	Warnings    []Warning      `json:"warnings,omitempty"`
}

// Warning records a non-fatal discovery problem.
type Warning struct {
	Provider string `json:"provider,omitempty"`
	Message  string `json:"message"`
	Source   Source `json:"source,omitempty"`
}
