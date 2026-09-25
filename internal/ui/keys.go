package ui

import (
	"sort"

	"github.com/charmbracelet/bubbles/key"
)

// keyMap holds every global binding. Modal-local and tab-local keys are handled
// inside their components / the focused-tab key routers.
type keyMap struct {
	Tab        key.Binding
	ShiftTab   key.Binding
	Enter      key.Binding
	Editor     key.Binding
	Create     key.Binding
	CreatePR   key.Binding
	ViewProcs  key.Binding
	LogTab     key.Binding
	PRTab      key.Binding
	DiffTab    key.Binding
	InspectTab key.Binding
	ChecksTab  key.Binding
	AgentTab   key.Binding
	Filter     key.Binding
	Sort       key.Binding
	Palette    key.Binding
	CopyFile   key.Binding
	Yank       key.Binding
	Scripts    key.Binding
	Aliases    key.Binding
	Pull       key.Binding
	Push       key.Binding
	Fetch      key.Binding
	Commit     key.Binding
	Rebase     key.Binding
	Update     key.Binding
	Prune      key.Binding
	BulkPrune  key.Binding
	Refresh    key.Binding
	Kill       key.Binding
	Restart    key.Binding
	SetPolicy  key.Binding
	ProcModal  key.Binding
	MultiView  key.Binding
	RenameProc key.Binding
	ProcSearch key.Binding
	Prefs      key.Binding
	Help       key.Binding
	Quit       key.Binding
}

// bindingSpec is the default key(s) and help text for one action. The action
// name is the config key users override under `keys:` in .bonsai.yaml.
type bindingSpec struct {
	keys []string
	desc string
}

// defaultBindings maps each action name to its default keys + help text. Order
// here is not significant; display grouping is defined by keymapSections.
var defaultBindings = map[string]bindingSpec{
	"focus_next":   {[]string{"tab"}, "focus"},
	"focus_prev":   {[]string{"shift+tab"}, "focus"},
	"shell":        {[]string{"enter"}, "shell"},
	"editor":       {[]string{"e"}, "editor"},
	"new_worktree": {[]string{"n"}, "new worktree"},
	"create_pr":    {[]string{"ctrl+n"}, "create PR"},
	"processes":    {[]string{"v"}, "processes"},
	"log_tab":      {[]string{"l"}, "git log"},
	"pr_tab":       {[]string{"P"}, "PR detail"},
	"diff_tab":     {[]string{"d"}, "diff vs base"},
	"inspect_tab":  {[]string{"i"}, "inspect"},
	"checks_tab":   {[]string{"b"}, "CI runs"},
	"agent_tab":    {[]string{"a"}, "agent"},
	"filter":       {[]string{"/"}, "filter"},
	"sort":         {[]string{"o"}, "sort"},
	"palette":      {[]string{"ctrl+k"}, "commands"},
	"copy_file":    {[]string{"c"}, "copy file"},
	"yank":         {[]string{"y"}, "yank/copy"},
	"scripts":      {[]string{"s"}, "scripts"},
	"aliases":      {[]string{"a"}, "aliases"},
	"pull":         {[]string{"ctrl+p"}, "pull"},
	"push":         {[]string{"ctrl+u"}, "push"},
	"fetch":        {[]string{"f"}, "fetch"},
	"commit":       {[]string{"C"}, "commit"},
	"rebase":       {[]string{"r"}, "rebase"},
	"update_base":  {[]string{"u"}, "update from base"},
	"prune":        {[]string{"x"}, "prune"},
	"bulk_prune":   {[]string{"X"}, "prune merged"},
	"refresh":      {[]string{"R"}, "refresh"},
	"kill_proc":    {[]string{"K"}, "kill proc"},
	"restart_proc": {[]string{"r"}, "restart proc"},
	"set_policy":   {[]string{"p"}, "restart policy"},
	"proc_modal":   {[]string{"V"}, "all processes"},
	"multi_view":   {[]string{"m"}, "view multiple processes together"},
	"rename_proc":  {[]string{"L"}, "tag process (multi-view label)"},
	"proc_search":  {[]string{"/"}, "search process output"},
	"prefs":        {[]string{","}, "preferences"},
	"help":         {[]string{"?"}, "keys"},
	"quit":         {[]string{"q", "ctrl+c"}, "quit"},
}

// binding builds a key.Binding for an action, applying a config override to the
// key(s) while preserving the default help description.
func binding(action string, overrides map[string]string) key.Binding {
	spec := defaultBindings[action]
	keys := spec.keys
	if ov, ok := overrides[action]; ok && ov != "" {
		keys = []string{ov}
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], spec.desc))
}

// keyCollisions reports user-introduced key conflicts: an override whose key
// lands on another action's key. It ignores the intentional overlaps in the
// defaults (tab-local keys such as restart vs the global rebase share a letter),
// only flagging a collision when at least one side was overridden by the user.
func keyCollisions(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return nil
	}
	primary := func(action string) string {
		if ov, ok := overrides[action]; ok && ov != "" {
			return ov
		}
		return defaultBindings[action].keys[0]
	}
	names := make([]string, 0, len(defaultBindings))
	for n := range defaultBindings {
		names = append(names, n)
	}
	sort.Strings(names)

	var collisions []string
	for _, a := range names {
		ov, overridden := overrides[a]
		if !overridden || ov == "" {
			continue
		}
		for _, b := range names {
			if b == a {
				continue
			}
			if primary(a) == primary(b) {
				collisions = append(collisions, a+" vs "+b+" ("+ov+")")
				break
			}
		}
	}
	return collisions
}

// newKeyMap builds the binding set from defaults, applying any config overrides
// (action name -> key). A nil map yields the defaults.
func newKeyMap(overrides map[string]string) keyMap {
	b := func(a string) key.Binding { return binding(a, overrides) }
	return keyMap{
		Tab:        b("focus_next"),
		ShiftTab:   b("focus_prev"),
		Enter:      b("shell"),
		Editor:     b("editor"),
		Create:     b("new_worktree"),
		CreatePR:   b("create_pr"),
		ViewProcs:  b("processes"),
		LogTab:     b("log_tab"),
		PRTab:      b("pr_tab"),
		DiffTab:    b("diff_tab"),
		InspectTab: b("inspect_tab"),
		ChecksTab:  b("checks_tab"),
		AgentTab:   b("agent_tab"),
		Filter:     b("filter"),
		Sort:       b("sort"),
		Palette:    b("palette"),
		CopyFile:   b("copy_file"),
		Yank:       b("yank"),
		Scripts:    b("scripts"),
		Aliases:    b("aliases"),
		Pull:       b("pull"),
		Push:       b("push"),
		Fetch:      b("fetch"),
		Commit:     b("commit"),
		Rebase:     b("rebase"),
		Update:     b("update_base"),
		Prune:      b("prune"),
		BulkPrune:  b("bulk_prune"),
		Refresh:    b("refresh"),
		Kill:       b("kill_proc"),
		Restart:    b("restart_proc"),
		SetPolicy:  b("set_policy"),
		ProcModal:  b("proc_modal"),
		MultiView:  b("multi_view"),
		RenameProc: b("rename_proc"),
		ProcSearch: b("proc_search"),
		Prefs:      b("prefs"),
		Help:       b("help"),
		Quit:       b("quit"),
	}
}

// keymapSection groups actions for the keymap modal and the preferences key
// editor. Every defaultBindings action should appear exactly once.
type keymapSection struct {
	name    string
	actions []string
}

var keymapSections = []keymapSection{
	{"Navigation", []string{"focus_next", "focus_prev", "palette", "prefs", "help", "quit"}},
	{"Worktree", []string{"shell", "editor", "new_worktree", "create_pr", "filter", "sort", "refresh", "prune", "bulk_prune"}},
	{"Tabs", []string{"log_tab", "processes", "inspect_tab", "diff_tab", "checks_tab", "pr_tab", "agent_tab"}},
	{"Files & clipboard", []string{"copy_file", "yank"}},
	{"Run", []string{"scripts", "aliases"}},
	{"Git", []string{"pull", "push", "fetch", "commit", "rebase", "update_base"}},
	{"Processes tab", []string{"kill_proc", "restart_proc", "set_policy", "proc_modal", "multi_view", "rename_proc", "proc_search"}},
}

// effectiveKey returns the key in effect for an action given user overrides.
func effectiveKey(action string, overrides map[string]string) string {
	if ov, ok := overrides[action]; ok && ov != "" {
		return ov
	}
	return defaultBindings[action].keys[0]
}
