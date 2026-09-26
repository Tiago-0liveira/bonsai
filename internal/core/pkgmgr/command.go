package pkgmgr

// CommandKind describes how a command entered the catalog.
type CommandKind string

const (
	CommandBuiltin   CommandKind = "builtin"
	CommandProject   CommandKind = "project"
	CommandInstalled CommandKind = "installed"
	CommandOverride  CommandKind = "override"
)

// Command is a normalized project/build command.
type Command struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Provider    string         `json:"provider"`
	ProjectRoot string         `json:"project_root,omitempty"`
	Kind        CommandKind    `json:"kind"`
	Args        []Argument     `json:"args,omitempty"`
	Invocation  InvocationSpec `json:"invocation"`
	Source      Source         `json:"source"`
	Confidence  Confidence     `json:"confidence"`
	Raw         string         `json:"raw,omitempty"`
}
