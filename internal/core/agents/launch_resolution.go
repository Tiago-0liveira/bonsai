package agents

// TaskRef names the task a session works on, so a later process can tie a
// holder back to the attempt that asked for it without reading its prompt.
type TaskRef struct {
	RepositoryID uint64 `json:"repository_id"`
	IssueID      uint64 `json:"issue_id"`
	AttemptID    string `json:"attempt_id"`
	Role         string `json:"role"` // implementation in this workflow
}

// LaunchResolution is the launch a profile would run with the caller's
// overrides applied, plus what the form needs to show it.
type LaunchResolution struct {
	AccountID   AccountID
	AccountName string
	Provider    ProviderID
	Launch      LaunchOptions
	Fields      []LaunchField
	Models      []ModelOption
	// Inherited marks the model, effort and native keys that came from the
	// profile instead of the caller. It is display only: the effective values
	// in Launch are what runs.
	Inherited map[string]bool
}

// LaunchResolver is implemented by providers that can say which options a
// launch really runs with. The result has the profile's defaults filled in and
// never carries the prompt or display name. It starts nothing and writes no
// settings.
type LaunchResolver interface {
	ResolveLaunch(Account, LaunchOptions) (LaunchOptions, error)
}

// ReservedSessionStore is implemented by stores that can create a session under
// an ID chosen beforehand, so a start can be recorded before its holder exists.
type ReservedSessionStore interface {
	SessionStore
	CreateWithID(Account, string, SessionID) (Session, error)
}

// CachedModelLister is implemented by providers that can offer the model choices
// they already know, without starting anything. Launch resolution uses it instead
// of ModelLister, which may run the provider.
type CachedModelLister interface {
	CachedModels(Account) []ModelOption
}
