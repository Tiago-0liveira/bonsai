package pkgmgr

// ArgumentKind describes how an argument is encoded in argv.
type ArgumentKind string

const (
	ArgumentFlag        ArgumentKind = "flag"
	ArgumentPositional  ArgumentKind = "positional"
	ArgumentPassThrough ArgumentKind = "passthrough"
)

// ValueType describes a known argument value type.
type ValueType string

const (
	ValueBool    ValueType = "bool"
	ValueString  ValueType = "string"
	ValueInt     ValueType = "int"
	ValueFloat   ValueType = "float"
	ValuePath    ValueType = "path"
	ValueEnum    ValueType = "enum"
	ValueUnknown ValueType = "unknown"
)

// Argument is a typed argument schema when Bonsai can describe it reliably.
type Argument struct {
	ID          string       `json:"id"`
	Name        string       `json:"name,omitempty"`
	Description string       `json:"description,omitempty"`
	Kind        ArgumentKind `json:"kind"`
	Type        ValueType    `json:"type"`
	Required    bool         `json:"required,omitempty"`
	Variadic    bool         `json:"variadic,omitempty"`
	Flags       []string     `json:"flags,omitempty"`
	Position    int          `json:"position,omitempty"`
	Default     *string      `json:"default,omitempty"`
	Choices     []string     `json:"choices,omitempty"`
	Source      Source       `json:"source"`
	Confidence  Confidence   `json:"confidence"`
}

// ArgumentValues maps argument IDs to one or more UI/CLI-supplied values.
type ArgumentValues map[string][]string
