package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Prefs holds personal UI preferences that override the repo's .bonsai.yaml
// for the local user: theme preset, keybinding overrides, default sort, and
// the prune merge default. Project settings (hooks, aliases, upstream) stay
// in .bonsai.yaml.
type Prefs struct {
	// Theme names a palette preset chosen in-app ("" = use .bonsai.yaml).
	Theme string `json:"theme,omitempty"`
	// Sort is the default worktree list ordering (name/ahead/behind/pr/
	// activity/dirty; "" = name).
	Sort string `json:"sort,omitempty"`
	// Keys maps an action name to a personal key override.
	Keys map[string]string `json:"keys,omitempty"`
	// PruneMerge turns the prune modal's merge-PR step on by default.
	PruneMerge bool `json:"prune_merge,omitempty"`
	// PRStatus is the worktree-list PR status display mode: "full" (glyph
	// plus label), "compact" (glyph only) or "off" ("" = full).
	PRStatus string `json:"pr_status,omitempty"`
	// AgentGlyphs picks how agent providers are drawn: "unicode" (✹ ❃ ✦) or
	// "ascii" (C X A) for fonts that render the glyphs badly ("" = unicode).
	AgentGlyphs string `json:"agent_glyphs,omitempty"`
	// Editor is a personal override for the "open editor" command, taking
	// priority over .bonsai.yaml's editor setting ("" = defer to it).
	Editor string `json:"editor,omitempty"`
	// Contrast picks the neutral palette: "" or "layered" keeps distinct
	// surface layers, "flat" collapses them for low-colour terminals.
	Contrast string `json:"contrast,omitempty"`
	// Density is the list spacing: "" or "comfortable", or "compact".
	Density string `json:"density,omitempty"`
	// StartWorkspace is the workspace opened on launch (lowercase name such as
	// "garden" or "settings"); "" reopens the last visited one. Core stores the
	// string only; the UI owns the valid names.
	StartWorkspace string `json:"start_workspace,omitempty"`
	// NoWorkspaceMemory stops remembering the last workspace per repo. Inverted
	// so that the zero value keeps the default (remember).
	NoWorkspaceMemory bool `json:"no_workspace_memory,omitempty"`
	// GardenAssist overrides AI drafting options; nil inherits built-in defaults.
	GardenAssist *GardenAssistPrefs `json:"garden_assist,omitempty"`
	// GardenReview is the local review policy; nil means automatic reviews are off.
	GardenReview     *GardenReviewPrefs     `json:"garden_review,omitempty"`
	GardenAutoReview *GardenAutoReviewPrefs `json:"garden_auto_review,omitempty"`
	// DepthGlyph is the marker shown before the nesting depth in bonsai-
	// spawned shell prompts and the TUI header ("" = 🪴).
	DepthGlyph string `json:"depth_glyph,omitempty"`
	// ShellPrompt controls prompt injection into spawned shells: "" = auto,
	// "off" = never touch the prompt (the env vars are still exported).
	ShellPrompt string `json:"shell_prompt,omitempty"`
}

// GardenAssistPrefs holds personal AI drafting defaults.
// Empty Account asks each time; empty Model and Effort inherit the account
// profile default, then provider default. Session choices override these defaults.
// Questions 0 means 5. Guidance is at most 300 runes; empty means none.
type GardenAssistPrefs struct {
	Account   string `json:"account,omitempty"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
	Questions int    `json:"questions,omitempty"`
	Guidance  string `json:"guidance,omitempty"`
}

// Clone returns a copy that shares no mutable data with p.
func (p Prefs) Clone() Prefs {
	p.GardenAutoReview = p.GardenAutoReview.Clone()
	if p.GardenAssist != nil {
		assist := *p.GardenAssist
		p.GardenAssist = &assist
	}
	if p.GardenReview != nil {
		review := *p.GardenReview
		p.GardenReview = &review
	}
	if p.Keys != nil {
		keys := make(map[string]string, len(p.Keys))
		for k, v := range p.Keys {
			keys[k] = v
		}
		p.Keys = keys
	}
	return p
}

// State is persisted mutable data: how often each main-repo file has been copied
// into a worktree, plus user-recorded aliases.
type State struct {
	// CopyCounts maps a main-repo relative file path to its copy frequency.
	CopyCounts map[string]int `json:"copy_counts"`
	Aliases    []Alias        `json:"aliases"`
	Prefs      Prefs          `json:"prefs"`
	// Workspaces maps a canonical repo identity (see RepoIdentity) to the last
	// workspace name visited there.
	Workspaces map[string]string `json:"workspaces,omitempty"`
	// GardenViews stores only the last Garden subview, scoped like Workspaces.
	GardenViews map[string]string `json:"garden_views,omitempty"`
	// GardenDrafts stores which private draft was open and at which section ("id|n"), never its text.
	GardenDrafts map[string]string `json:"garden_drafts,omitempty"`

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`

	// Generations keep late async writes from replacing newer preferences or views.
	prefsGen  uint64 `json:"-"`
	wsGen     uint64 `json:"-"`
	gardenGen uint64 `json:"-"`
	draftGen  uint64 `json:"-"`
	// extra* keep keys this version does not know, so a newer or hand-edited
	// state.json survives a rewrite.
	extra               map[string]json.RawMessage `json:"-"`
	extraPrefs          map[string]json.RawMessage `json:"-"`
	autoReviewPreserved bool                       `json:"-"`
}

// stateDisk mirrors State's persisted fields without the mutex so snapshots
// can be built and marshaled without copying a lock.
type stateDisk struct {
	CopyCounts   map[string]int    `json:"copy_counts"`
	Aliases      []Alias           `json:"aliases"`
	Prefs        Prefs             `json:"prefs"`
	Workspaces   map[string]string `json:"workspaces,omitempty"`
	GardenViews  map[string]string `json:"garden_views,omitempty"`
	GardenDrafts map[string]string `json:"garden_drafts,omitempty"`
}

// jsonKeys lists the JSON object keys of struct type t, derived from tags so
// the known set cannot go stale.
func jsonKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		keys[name] = true
	}
	return keys
}

// unknownKeys returns the entries of raw whose keys are not in known.
func unknownKeys(raw map[string]json.RawMessage, known map[string]bool) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	for k, v := range raw {
		if known[k] {
			continue
		}
		if out == nil {
			out = map[string]json.RawMessage{}
		}
		out[k] = v
	}
	return out
}

// RepoIdentity returns a canonical key for a repo directory: absolute,
// symlink-resolved when possible, cleaned, and case-folded on Windows.
func RepoIdentity(dir string) string {
	p, err := filepath.Abs(dir)
	if err != nil {
		p = dir
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// statePath returns ~/.config/bonsai/state.json (honoring $XDG_CONFIG_HOME).
func statePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "state.json"), nil
}

// LoadState reads the persisted state, returning an empty (but usable) State if
// the file does not yet exist.
func LoadState() (*State, error) {
	p, err := statePath()
	if err != nil {
		return nil, err
	}
	st := &State{CopyCounts: map[string]int{}, path: p}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	// Decode the original bytes in order, retaining encoding/json's merge and
	// case-folding semantics for unrelated preferences. Only the optional policy
	// is captured raw so a malformed shape cannot prevent those from loading.
	var decoded struct {
		*State
		Prefs struct {
			*Prefs
			GardenAutoReview json.RawMessage `json:"garden_auto_review"`
		} `json:"prefs"`
	}
	decoded.State = st
	decoded.Prefs.Prefs = &st.Prefs
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	if st.CopyCounts == nil {
		st.CopyCounts = map[string]int{}
	}
	st.path = p
	// A hand-edited invalid policy must not keep authorizing automatic runs: fail closed to "off".
	if st.Prefs.GardenReview != nil && st.Prefs.GardenReview.Validate() != nil {
		st.Prefs.GardenReview = nil
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	st.extra = unknownKeys(top, jsonKeys(reflect.TypeOf(stateDisk{})))
	if raw, ok := top["prefs"]; ok {
		var pm map[string]json.RawMessage
		if json.Unmarshal(raw, &pm) == nil {
			st.extraPrefs = unknownKeys(pm, jsonKeys(reflect.TypeOf(Prefs{})))
		}
	}
	var autoRaw json.RawMessage
	autoEntries := map[string]json.RawMessage{}
	prefsCount, policyCount := 0, 0
	prefsExact, policyExact := false, false
	walkJSONObject(data, func(key string, raw json.RawMessage) {
		if !strings.EqualFold(key, "prefs") {
			return
		}
		prefsCount++
		prefsExact = key == "prefs"
		walkJSONObject(raw, func(prefKey string, prefRaw json.RawMessage) {
			if !strings.EqualFold(prefKey, "garden_auto_review") {
				return
			}
			policyCount++
			policyExact = prefKey == "garden_auto_review"
			if key == "prefs" {
				// Maps can retain only the last occurrence of an exact duplicate.
				autoEntries[prefKey] = prefRaw
				if policyExact {
					autoRaw = prefRaw
				}
			}
		})
	})
	if policyCount > 0 {
		var policy GardenAutoReviewPrefs
		if prefsCount == 1 && prefsExact && policyCount == 1 && policyExact && canonical(autoRaw, &policy) && policy.Version <= GardenAutoReviewVersion && policy.Validate() == nil {
			st.Prefs.GardenAutoReview = &policy
		} else {
			st.Prefs.GardenAutoReview = nil
			st.autoReviewPreserved = true
			if st.extraPrefs == nil {
				st.extraPrefs = map[string]json.RawMessage{}
			}
			// Non-exact top-level prefs objects remain intact in extra.
			for key, raw := range autoEntries {
				st.extraPrefs[key] = raw
			}
		}
	}
	return st, nil
}

// walkJSONObject visits occurrences rather than map entries so duplicates still
// count when deciding whether a local policy has an unambiguous canonical key.
// The caller has already checked JSON syntax with the general state decoder.
func walkJSONObject(raw []byte, visit func(string, json.RawMessage)) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return
	}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return
		}
		key, ok := token.(string)
		if !ok {
			return
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return
		}
		visit(key, value)
	}
}

// GardenAutoReviewPreserved reports whether a rejected raw policy is protected
// from overwrites while unrelated preferences remain editable.
func (s *State) GardenAutoReviewPreserved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gardenAutoReviewPreservedLocked()
}

func (s *State) gardenAutoReviewPreservedLocked() bool {
	return s.autoReviewPreserved
}

// marshalDisk encodes d, merging preserved unknown keys; known fields win.
func (s *State) marshalDisk(d stateDisk) ([]byte, error) {
	if len(s.extra) == 0 && len(s.extraPrefs) == 0 {
		return json.MarshalIndent(d, "", "  ")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	for k, v := range s.extra {
		if _, ok := top[k]; !ok {
			top[k] = v
		}
	}
	if len(s.extraPrefs) > 0 {
		var pm map[string]json.RawMessage
		if err := json.Unmarshal(top["prefs"], &pm); err != nil {
			return nil, err
		}
		for k, v := range s.extraPrefs {
			if _, ok := pm[k]; !ok {
				pm[k] = v
			}
		}
		if top["prefs"], err = json.Marshal(pm); err != nil {
			return nil, err
		}
	}
	return json.MarshalIndent(top, "", "  ")
}

// writeLocked atomically writes d. Caller holds s.mu and the state file lock.
// The staging file is unique so independent writers never share it.
func (s *State) writeLocked(d stateDisk) error {
	data, err := s.marshalDisk(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o644)
	}
	if werr == nil {
		werr = gitstore.ReplaceFile(tmp, s.path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
	}
	return werr
}

// persistLocked serializes writers across processes with a lock file, rebases
// d on the lists and maps another process persisted since this one loaded,
// applies update, writes the result and adopts it in memory. update reports
// whether anything changed; if not, nothing is written. Preferences are never
// rebased: they are only replaced by the preference writers, whose newer
// policy handling a plain decode would lose. Caller holds s.mu.
func (s *State) persistLocked(update func(d *stateDisk) bool) error {
	if s.path == "" {
		return errors.New("state has no file path")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	lock, err := procstore.Lock(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()

	d := s.diskLocked()
	if data, err := os.ReadFile(s.path); err == nil {
		var latest struct {
			CopyCounts   map[string]int    `json:"copy_counts"`
			Aliases      []Alias           `json:"aliases"`
			Workspaces   map[string]string `json:"workspaces"`
			GardenViews  map[string]string `json:"garden_views"`
			GardenDrafts map[string]string `json:"garden_drafts"`
		}
		if json.Unmarshal(data, &latest) == nil {
			d.CopyCounts, d.Aliases, d.Workspaces = latest.CopyCounts, latest.Aliases, latest.Workspaces
			d.GardenViews, d.GardenDrafts = latest.GardenViews, latest.GardenDrafts
		}
	}
	if d.CopyCounts == nil {
		d.CopyCounts = map[string]int{}
	}
	if !update(&d) {
		return nil
	}
	if err := s.writeLocked(d); err != nil {
		return err
	}
	s.CopyCounts, s.Aliases, s.Prefs = d.CopyCounts, d.Aliases, d.Prefs
	s.Workspaces, s.GardenViews, s.GardenDrafts = d.Workspaces, d.GardenViews, d.GardenDrafts
	return nil
}

// diskLocked snapshots the in-memory fields. Caller holds s.mu.
func (s *State) diskLocked() stateDisk {
	return stateDisk{CopyCounts: s.CopyCounts, Aliases: s.Aliases, Prefs: s.Prefs, Workspaces: s.Workspaces, GardenViews: s.GardenViews, GardenDrafts: s.GardenDrafts}
}

// Save writes this process's whole in-memory state, so it replaces whatever
// another process persisted; the field-specific writers below merge instead.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.persistLocked(func(d *stateDisk) bool {
		*d = s.diskLocked()
		return true
	})
}

// RecordCopy increments the copy frequency for relPath and persists.
func (s *State) RecordCopy(relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked(func(d *stateDisk) bool {
		d.CopyCounts[relPath]++
		return true
	})
}

// CopyRank returns the recorded copy frequency for relPath (0 if never copied).
func (s *State) CopyRank(relPath string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CopyCounts[relPath]
}

// AddAlias appends an alias and persists.
func (s *State) AddAlias(a Alias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked(func(d *stateDisk) bool {
		d.Aliases = append(d.Aliases, a)
		return true
	})
}

// RemoveAlias deletes the alias with the given name and persists. Returns false
// if no such alias existed.
func (s *State) RemoveAlias(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	err := s.persistLocked(func(d *stateDisk) bool {
		out := make([]Alias, 0, len(d.Aliases))
		for _, a := range d.Aliases {
			if a.Name == name {
				found = true
				continue
			}
			out = append(out, a)
		}
		d.Aliases = out
		return found
	})
	return found, err
}

// SortedAliases returns aliases ordered by name.
func (s *State) SortedAliases() []Alias {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Alias, len(s.Aliases))
	copy(out, s.Aliases)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SetPrefs replaces the stored UI preferences and persists them.
func (s *State) SetPrefs(p Prefs) error {
	if err := p.GardenReview.validateIfSet(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gardenAutoReviewPreservedLocked() && p.GardenAutoReview != nil {
		return ErrGardenAutoReviewNewer
	}
	if p.GardenAutoReview != nil {
		if err := p.GardenAutoReview.Validate(); err != nil {
			return err
		}
	}
	return s.persistLocked(func(d *stateDisk) bool {
		d.Prefs = p.Clone()
		return true
	})
}

// PrefsSnapshot returns a copy of the preferences that shares no map with s.
func (s *State) PrefsSnapshot() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Prefs.Clone()
}

// PersistPrefs writes p to disk and, only if that succeeds, adopts it. gen
// orders concurrent callers: a gen below the last applied one returns
// ErrSuperseded without touching disk or memory.
func (s *State) PersistPrefs(gen uint64, p Prefs) error {
	if err := p.GardenReview.validateIfSet(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gardenAutoReviewPreservedLocked() && p.GardenAutoReview != nil {
		return ErrGardenAutoReviewNewer
	}
	if p.GardenAutoReview != nil {
		if err := p.GardenAutoReview.Validate(); err != nil {
			return err
		}
	}
	if gen < s.prefsGen {
		return ErrSuperseded
	}
	p = p.Clone()
	if err := s.persistLocked(func(d *stateDisk) bool {
		d.Prefs = p
		return true
	}); err != nil {
		return fmt.Errorf("persist prefs: %w", err)
	}
	s.prefsGen = gen
	return nil
}

// LastWorkspace returns the remembered workspace for repoDir ("" if none).
func (s *State) LastWorkspace(repoDir string) string {
	id := RepoIdentity(repoDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Workspaces[id]
}

// PersistLastWorkspace records name as repoDir's last workspace (empty name
// deletes the entry), ordered by gen like PersistPrefs.
func (s *State) PersistLastWorkspace(gen uint64, repoDir, name string) error {
	id := RepoIdentity(repoDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.wsGen {
		return ErrSuperseded
	}
	if err := s.persistLocked(func(d *stateDisk) bool {
		d.Workspaces = withEntry(d.Workspaces, id, name)
		return true
	}); err != nil {
		return fmt.Errorf("persist workspace: %w", err)
	}
	s.wsGen = gen
	return nil
}

// withEntry returns a copy of m with key set to value, or removed when value is
// empty; an emptied map becomes nil so it stays out of the file.
func withEntry(m map[string]string, key, value string) map[string]string {
	next := make(map[string]string, len(m)+1)
	for k, v := range m {
		next[k] = v
	}
	if value == "" {
		delete(next, key)
	} else {
		next[key] = value
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

// GardenView remembers presentation only, never issue content or search text.
func (s *State) GardenView(repoDir string) string {
	id := RepoIdentity(repoDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.GardenViews[id]
}

// PersistGardenView follows the same ordered atomic state writes as workspace memory.
func (s *State) PersistGardenView(gen uint64, repoDir, view string) error {
	if view != "board" && view != "table" && view != "attention" && view != "setup" {
		return ErrInvalidGardenView
	}
	id := RepoIdentity(repoDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.gardenGen {
		return ErrSuperseded
	}
	if err := s.persistLocked(func(d *stateDisk) bool {
		d.GardenViews = withEntry(d.GardenViews, id, view)
		return true
	}); err != nil {
		return fmt.Errorf("persist Garden view: %w", err)
	}
	s.gardenGen = gen
	return nil
}

// GardenDraft returns the private draft remembered for repoDir and its section index.
func (s *State) GardenDraft(repoDir string) (id string, section int) {
	s.mu.Lock()
	raw := s.GardenDrafts[RepoIdentity(repoDir)]
	s.mu.Unlock()
	id, n, ok := strings.Cut(raw, "|")
	if !ok || !validDraftRef(id) {
		return "", 0
	}
	section, err := strconv.Atoi(n)
	if err != nil || section < 0 || section > 1000 {
		return "", 0
	}
	return id, section
}

// validDraftRef accepts only an opaque ID, so the file can never carry authored text.
func validDraftRef(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// PersistGardenDraft remembers the open private draft and section; an empty id forgets it.
// Writes are ordered like the other view memory.
func (s *State) PersistGardenDraft(gen uint64, repoDir, id string, section int) error {
	if id != "" && (!validDraftRef(id) || section < 0 || section > 1000) {
		return ErrInvalidGardenDraft
	}
	ident := RepoIdentity(repoDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.draftGen {
		return ErrSuperseded
	}
	value := ""
	if id != "" {
		value = id + "|" + strconv.Itoa(section)
	}
	if err := s.persistLocked(func(d *stateDisk) bool {
		d.GardenDrafts = withEntry(d.GardenDrafts, ident, value)
		return true
	}); err != nil {
		return fmt.Errorf("persist Garden draft: %w", err)
	}
	s.draftGen = gen
	return nil
}
