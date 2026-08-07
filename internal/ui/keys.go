package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every global binding. Modal-local keys are handled inside the
// modal component.
type keyMap struct {
	Tab       key.Binding
	ShiftTab  key.Binding
	Enter     key.Binding
	Create    key.Binding
	ViewProcs key.Binding
	Filter    key.Binding
	CopyFile  key.Binding
	Scripts   key.Binding
	Aliases   key.Binding
	Pull      key.Binding
	Push      key.Binding
	Commit    key.Binding
	Rebase    key.Binding
	Prune     key.Binding
	Refresh   key.Binding
	Kill      key.Binding
	Restart   key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		ShiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "focus")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "shell")),
		Create:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new worktree")),
		ViewProcs: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "processes")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		CopyFile:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy file")),
		Scripts:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "scripts")),
		Aliases:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "aliases")),
		Pull:      key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "pull")),
		Push:      key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "push")),
		Commit:    key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "commit")),
		Rebase:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rebase")),
		Prune:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "prune")),
		Refresh:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
		Kill:      key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "kill proc")),
		Restart:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart proc")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Create, k.Filter, k.CopyFile, k.Commit, k.Prune, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab, k.ShiftTab, k.Enter, k.Create, k.ViewProcs, k.Filter, k.Refresh},
		{k.CopyFile, k.Scripts, k.Aliases},
		{k.Pull, k.Push, k.Commit, k.Rebase, k.Prune},
		{k.Kill, k.Restart, k.Help, k.Quit},
	}
}
