package pkgmgr

// Source preserves where a discovered fact came from.
type Source struct {
	Kind    string `json:"kind,omitempty"`
	File    string `json:"file,omitempty"`
	Pointer string `json:"pointer,omitempty"`
	Line    int    `json:"line,omitempty"`
}
