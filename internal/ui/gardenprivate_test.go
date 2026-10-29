package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// privBackend is the Garden fake plus a real private draft store on a temp file.
type privBackend struct {
	*gardenfake.Backend
	store *drafts.Store
	scope drafts.Scope

	mu          sync.Mutex
	saves       int
	saveErr     error
	unavailable bool
}

var _ gardenapi.PrivateDrafts = (*privBackend)(nil)

func newPrivStore(t *testing.T, path, prefix string) *drafts.Store {
	t.Helper()
	n := 0
	s, err := drafts.New(path, nil, func() drafts.ID {
		n++
		return drafts.ID(fmt.Sprintf("draft-%s%026d", prefix, n))
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newPrivBackend(t *testing.T, fb *gardenfake.Backend, file string) *privBackend {
	t.Helper()
	return &privBackend{Backend: fb, store: newPrivStore(t, file, "aa"), scope: drafts.Scope{Local: "clone-a", Repo: garden.RepoID(gardenfake.SampleRepoID)}}
}

func (b *privBackend) ListPrivateDrafts(ctx context.Context, _ garden.RepoID, all bool) ([]drafts.Draft, error) {
	if b.unavailable {
		return nil, drafts.ErrUnavailable
	}
	return b.store.List(ctx, b.scope, all)
}
func (b *privBackend) ReadPrivateDraft(ctx context.Context, _ garden.RepoID, id drafts.ID) (drafts.Draft, error) {
	return b.store.Read(ctx, b.scope, id)
}
func (b *privBackend) SavePrivateDraft(ctx context.Context, _ garden.RepoID, d drafts.Draft, expected uint64) (drafts.Draft, error) {
	b.mu.Lock()
	b.saves++
	err := b.saveErr
	b.mu.Unlock()
	if err != nil {
		return drafts.Draft{}, err
	}
	return b.store.Save(ctx, b.scope, d, expected)
}
func (b *privBackend) ArchivePrivateDraft(ctx context.Context, _ garden.RepoID, id drafts.ID, exp uint64) (drafts.Draft, error) {
	return b.store.Archive(ctx, b.scope, id, exp)
}
func (b *privBackend) UnarchivePrivateDraft(ctx context.Context, _ garden.RepoID, id drafts.ID, exp uint64) (drafts.Draft, error) {
	return b.store.Unarchive(ctx, b.scope, id, exp)
}

func (b *privBackend) saveCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

// privEnv holds what two models of one "machine" share: config dir, repo dir, store file.
type privEnv struct{ repo, file string }

func newPrivEnv(t *testing.T) privEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	return privEnv{repo: t.TempDir(), file: filepath.Join(t.TempDir(), "drafts.json")}
}

// open builds a model on the Drafts submenu, as a user reaches it.
func (e privEnv) open(t *testing.T, pb *privBackend, w, h int) Model {
	t.Helper()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	m := sized(t, New(e.repo, &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(pb)), w, h)
	m, _ = drainAll(t, m, m.Init())
	m = pressAll(t, m, "enter")
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Drafts did not open:\n%s", view(m))
	}
	return m
}

func (e privEnv) backend(t *testing.T) *privBackend {
	return newPrivBackend(t, draftsFake(t), e.file)
}

func pkey(s string) tea.KeyMsg {
	switch s {
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	case "ctrl+y":
		return tea.KeyMsg{Type: tea.KeyCtrlY}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+]":
		return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	if len(s) > 1 {
		return keyOf(s)
	}
	return runeKey(s)
}

// pk sends keys and runs every command they produce, except the autosave timer.
func pk(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = send(t, m, pkey(k))
		m, _ = drainAll(t, m, cmd)
	}
	return m
}

// ptype types text; "\n" is Enter.
func ptype(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		k := string(r)
		if r == '\n' {
			k = "enter"
		}
		m = pk(t, m, k)
	}
	return m
}

// ptick delivers the autosave timer of the current generation and runs the save.
func ptick(t *testing.T, m Model) Model {
	t.Helper()
	p := m.garden.drafts.private
	m, cmd := send(t, m, privTickMsg{p.token, p.gen})
	m, _ = drainAll(t, m, cmd)
	return m
}

func (b *privBackend) all(t *testing.T) []drafts.Draft {
	t.Helper()
	l, err := b.store.List(context.Background(), b.scope, true)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPrivateDraftJourneyWithoutProvider(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "n")
	mustContain(t, "capture", view(m), "WHAT'S ON YOUR MIND?", "Continue writing →", "Discuss with AI…", "No GitHub issue yet", "Draft / Description")
	m = ptype(t, m, "Users lose the login when switching accounts")
	if got := view(m); strings.Contains(got, "Saved locally") || !strings.Contains(got, "Unsaved changes") {
		t.Fatalf("dirty text must not read as saved:\n%s", got)
	}
	m = ptick(t, m)
	mustContain(t, "saved", view(m), "✓ Saved locally")
	if l := pb.all(t); len(l) != 1 || l[0].Description != "Users lose the login when switching accounts" {
		t.Fatalf("store = %+v", l)
	}
	id := m.garden.drafts.private.draft.ID

	m = pk(t, m, "tab", "enter") // Continue writing
	p := m.garden.drafts.private
	if p.stage != privEditor || p.draft.ID != id {
		t.Fatalf("Continue must keep the same draft: stage %d, id %q vs %q", p.stage, p.draft.ID, id)
	}
	mustContain(t, "editor", view(m), "SECTIONS", "Problem", "Outcome", "Requirements", "Criteria", "Questions", "Assistant off", "Invite assistant…", "Write and approve", "without AI.", "Review & create issue →", "Private draft")
	if p.secs[0].text != "Users lose the login when switching accounts" {
		t.Fatalf("Problem was not seeded: %q", p.secs[0].text)
	}
	m = pk(t, m, "ctrl+t")
	m = ptype(t, m, "Keep sessions\n")
	m = pk(t, m, "tab", "enter") // Outcome, writing
	m = ptype(t, m, "Each account stays signed in.")
	m = pk(t, m, "tab", "tab") // Requirements, Criteria
	m = ptype(t, m, "[ ] Switch A to B and back")
	m = pk(t, m, "ctrl+s")
	if r := m.garden.drafts.private.assess(); !r.Ready() {
		t.Fatalf("not ready: %+v", r)
	}
	mustContain(t, "ready", view(m), "✓ Problem stated", "✓ Outcome stated", "✓ Criteria written", "✓ Questions resolved", "✓ Saved locally")
	saved, err := pb.store.Read(context.Background(), pb.scope, id)
	if err != nil || saved.Title != "Keep sessions" || saved.Spec.DesiredBehavior != "Each account stays signed in." || saved.Spec.AcceptanceCriteria != "[ ] Switch A to B and back" {
		t.Fatalf("saved = %+v %v", saved, err)
	}

	m = pk(t, m, "ctrl+r")
	out := view(m)
	mustContain(t, "review", out, "REVIEW & CREATE ISSUE", "Keep sessions", "Each account stays signed in.", "Create GitHub issue (unavailable)", privPublishUnavailable)
	m = pk(t, m, "enter")
	mustContain(t, "review", view(m), privPublishUnavailable)
	m = pk(t, m, "esc")
	if m.garden.drafts.private.stage != privEditor {
		t.Fatal("Esc must return to the editor")
	}
	if pb.Writes() != 0 || pb.Count("PreviewCapture") != 0 || pb.Count("Capture") != 0 || pb.Count("PreviewSave") != 0 {
		t.Fatalf("GitHub or provider was called: %v", pb.Calls())
	}
	for _, c := range pb.Calls() {
		if strings.Contains(c, "Assist") {
			t.Fatalf("assistant call %q", c)
		}
	}
}

func TestPrivateDraftDebounceOrderAndTruthfulStatus(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "n")
	m = ptype(t, m, "abc")
	if pb.saveCount() != 0 {
		t.Fatal("typing must not save before the timer")
	}
	p := m.garden.drafts.private
	stale := privTickMsg{p.token, p.gen - 1}
	m, cmd := send(t, m, stale)
	if cmd != nil || pb.saveCount() != 0 {
		t.Fatal("a stale tick must be ignored")
	}
	m, save := send(t, m, privTickMsg{p.token, p.gen})
	if save == nil || !m.garden.drafts.private.inflight {
		t.Fatal("the current tick must start one save")
	}
	mustContain(t, "saving", view(m), "Saving…")
	// An edit while the save is in flight, and its own tick, start nothing.
	m, _ = send(t, m, runeKey("d"))
	p = m.garden.drafts.private
	m, cmd = send(t, m, privTickMsg{p.token, p.gen})
	if cmd != nil {
		t.Fatal("a tick during a save must not start a second one")
	}
	if strings.Contains(view(m), "Saved locally") {
		t.Fatal("never Saved while edits are unwritten")
	}
	reply := save()
	m, follow := send(t, m, reply)
	if follow == nil || strings.Contains(view(m), "✓ Saved locally") {
		t.Fatalf("the reply must start exactly one follow-up and not claim Saved:\n%s", view(m))
	}
	if pb.saveCount() != 1 {
		t.Fatalf("saves = %d before the follow-up runs", pb.saveCount())
	}
	m, _ = drainAll(t, m, follow)
	if pb.saveCount() != 2 {
		t.Fatalf("saves = %d, want 2", pb.saveCount())
	}
	mustContain(t, "clean", view(m), "✓ Saved locally")
	if l := pb.all(t); len(l) != 1 || l[0].Description != "abcd" || l[0].Revision != 2 {
		t.Fatalf("store = %+v", l)
	}
}

func TestPrivateDraftSaveFailureKeepsTextAndGuardsQuit(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	pb.saveErr = errors.New("disk full: /secret/path")
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "n")
	m = ptype(t, m, "precious text")
	m = ptick(t, m)
	out := view(m)
	mustContain(t, "failed", out, "Save failed — text kept")
	mustNotContain(t, "failed", out, "/secret/path")
	if m.garden.drafts.private.desc.Value() != "precious text" {
		t.Fatal("text lost")
	}
	m, cmd := send(t, m, pkey("ctrl+c"))
	if cmd != nil || m.modal == nil {
		t.Fatalf("quit with unwritten edits must ask first (modal %v)", m.modal)
	}
	mustContain(t, "dialog", view(m), "Unwritten draft edits", "Save and quit", "Quit without saving", "Cancel")
	// Save and quit retries and does not quit while the save fails.
	m, cmd = send(t, m, pkey("enter"))
	m, seen := drainAll(t, m, cmd)
	for _, s := range seen {
		if _, quit := s.(tea.QuitMsg); quit {
			t.Fatal("quit although the save failed")
		}
	}
	if m.garden.drafts.private.state != saveFailed {
		t.Fatalf("state = %d", m.garden.drafts.private.state)
	}
	pb.saveErr = nil
	m, cmd = send(t, m, pkey("ctrl+c"))
	m, cmd = send(t, m, pkey("enter"))
	m, seen = drainAll(t, m, cmd)
	quit := false
	for _, s := range seen {
		if _, ok := s.(tea.QuitMsg); ok {
			quit = true
		}
	}
	if !quit {
		t.Fatalf("Save and quit did not quit after a successful save; seen %v", seen)
	}
	if l := pb.all(t); len(l) != 1 || l[0].Description != "precious text" {
		t.Fatalf("store = %+v", l)
	}
}

func TestPrivateDraftQuitWithoutSavingAndCancel(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	pb.saveErr = errors.New("nope")
	m := e.open(t, pb, 120, 35)
	m = ptype(t, pk(t, m, "n"), "x")
	m = ptick(t, m)
	m, _ = send(t, m, pkey("ctrl+c"))
	m = pk(t, m, "esc") // Cancel
	if m.modal != nil || m.garden.drafts.private == nil {
		t.Fatal("Esc must cancel the dialog and keep the draft")
	}
	m, _ = send(t, m, pkey("ctrl+c"))
	m = pk(t, m, "down") // Quit without saving is the second item
	m, cmd := send(t, m, pkey("enter"))
	_, seen := drainAll(t, m, cmd)
	quit := false
	for _, s := range seen {
		if _, ok := s.(tea.QuitMsg); ok {
			quit = true
		}
	}
	if !quit {
		t.Fatalf("Quit without saving did not quit; seen %v", seen)
	}
}

func TestPrivateParkedFailedDraftConflictsWithNewerRevisionOnReopen(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "base"))
	id := m.garden.drafts.private.draft.ID
	pb.saveErr = errors.New("disk full")
	m = ptick(t, ptype(t, m, "+local"))
	m = pk(t, m, "esc") // leave with a failed save: the text stays parked in memory
	if m.garden.drafts.stage != draftsList || m.garden.drafts.private == nil {
		t.Fatalf("stage %d, parked %v", m.garden.drafts.stage, m.garden.drafts.private)
	}
	mustContain(t, "notice", view(m), "has edits that are not saved")
	pb.saveErr = nil
	other := newPrivStore(t, e.file, "bb")
	cur, _ := other.Read(context.Background(), pb.scope, id)
	cur.Title = "theirs"
	if _, err := other.Save(context.Background(), pb.scope, cur, cur.Revision); err != nil {
		t.Fatal(err)
	}
	m = pk(t, m, "enter")
	p := m.garden.drafts.private
	if p.state != saveConflict || p.desc.Value() != "base+local" {
		t.Fatalf("state %d text %q", p.state, p.desc.Value())
	}
	found := false
	for _, d := range pb.all(t) {
		found = found || (d.ConflictOf == id && d.Description == "base+local")
	}
	if !found {
		t.Fatal("unsaved text was not preserved as a recovery draft")
	}
}

func TestPrivateDraftConflictKeepMineAndRecovery(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "first idea"))
	id := m.garden.drafts.private.draft.ID
	// Another window saves a newer revision.
	other := newPrivStore(t, e.file, "bb")
	cur, err := other.Read(context.Background(), pb.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Title = "Theirs"
	if _, err := other.Save(context.Background(), pb.scope, cur, cur.Revision); err != nil {
		t.Fatal(err)
	}
	m = ptick(t, ptype(t, m, "!"))
	out := view(m)
	mustContain(t, "conflict", out, "Changed in another Bonsai window. Your text was kept as D-", "Conflict — choose a version")
	if m.garden.drafts.private.desc.Value() != "first idea!" {
		t.Fatal("text lost in conflict")
	}
	saves := pb.saveCount()
	m = ptype(t, m, "z") // editing is locked: letters are decisions here
	if m.garden.drafts.private.desc.Value() != "first idea!" || pb.saveCount() != saves {
		t.Fatal("nothing may auto-resolve or keep saving during a conflict")
	}
	m = pk(t, m, "k")
	got, err := pb.store.Read(context.Background(), pb.scope, id)
	if err != nil || got.Revision != 3 || got.Description != "first idea!" || got.Title != "Theirs" && got.Title != "" {
		t.Fatalf("keep mine: %+v %v", got, err)
	}
	if m.garden.drafts.private.state != saveClean {
		t.Fatalf("state = %d", m.garden.drafts.private.state)
	}
	var recovery int
	for _, d := range pb.all(t) {
		if d.ConflictOf == id {
			recovery++
		}
	}
	if recovery != 1 {
		t.Fatalf("recovery drafts = %d", recovery)
	}
	m = pk(t, m, "esc")
	mustContain(t, "inbox", view(m), "recovery of D-")
}

func TestPrivateDraftConflictOpenTheirs(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "mine"))
	id := m.garden.drafts.private.draft.ID
	other := newPrivStore(t, e.file, "bb")
	cur, _ := other.Read(context.Background(), pb.scope, id)
	cur.Description = "theirs"
	if _, err := other.Save(context.Background(), pb.scope, cur, cur.Revision); err != nil {
		t.Fatal(err)
	}
	m = ptick(t, ptype(t, m, "?"))
	m = pk(t, m, "o")
	p := m.garden.drafts.private
	if p.desc.Value() != "theirs" || p.state != saveClean {
		t.Fatalf("open theirs: %q state %d", p.desc.Value(), p.state)
	}
	mustContain(t, "notice", view(m), "Opened the other version")
}

func TestPrivateDraftRestartRestoresSelectionSectionAndText(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "Restart me"))
	id := m.garden.drafts.private.draft.ID
	m = pk(t, m, "tab", "enter")               // editor
	m = pk(t, m, "tab", "tab", "tab", "enter") // Outcome, Requirements, Criteria? (rail), then text
	m = ptype(t, m, "criteria after restart")
	m = pk(t, m, "ctrl+s", "esc", "esc")
	if m.gardenScreen() != scrDrafts || m.garden.drafts.stage != draftsList {
		t.Fatalf("not back on the inbox: stage %d", m.garden.drafts.stage)
	}
	want := drafts.DraftLabel(id)
	mustContain(t, "inbox", view(m), want)

	pb2 := newPrivBackend(t, draftsFake(t), e.file) // a second process: new store handle, same file
	m2 := e.open(t, pb2, 120, 35)
	if m2.garden.drafts.selectedLocal != id || m2.garden.drafts.selected != 0 {
		t.Fatalf("selection = %q / %d", m2.garden.drafts.selectedLocal, m2.garden.drafts.selected)
	}
	m2 = pk(t, m2, "enter")
	p := m2.garden.drafts.private
	if p == nil || p.stage != privEditor || p.secs[p.cur].title != "Criteria" || p.ta.Value() != "criteria after restart" {
		t.Fatalf("restored: %+v", p)
	}
	mustContain(t, "restored", view(m2), "CRITERIA", "criteria after restart")
}

func TestPrivateDraftShortcutOwnership(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "n")
	for _, k := range []string{"q", "n", "i", "?", "/", "x", "t", "r", "1", "a", "u"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, pkey(k))
		m, seen := drainAll(t, m, cmd)
		for _, s := range seen {
			if _, quit := s.(tea.QuitMsg); quit {
				t.Fatalf("%q quit", k)
			}
		}
		if m.modal != nil {
			t.Fatalf("%q opened a modal", k)
		}
	}
	if got := m.garden.drafts.private.desc.Value(); got != "qni?/xtr1au" {
		t.Fatalf("typed text = %q", got)
	}
	before := m.garden.drafts.private.desc.Value()
	m = pk(t, m, "ctrl+]")
	if m.garden.drafts.private.desc.Value() != before || m.modal != nil {
		t.Fatal("ctrl+] must not be consumed as text")
	}
	if m.currentShellFocus() != shellInput {
		t.Fatal("text focus must give the body every key")
	}
	// The editor text too.
	m = pk(t, m, "tab", "enter", "enter") // Continue writing, then enter the Problem text
	m = ptype(t, m, "q?")
	if p := m.garden.drafts.private; !p.text || !strings.HasPrefix(p.ta.Value(), "q?") || m.modal != nil {
		t.Fatalf("editor text: %q text=%v", p.ta.Value(), p.text)
	}
	// On the rail letters are no longer text: q asks about quitting rather than typing.
	m = pk(t, m, "esc")
	if m.currentShellFocus() == shellInput {
		t.Fatal("rail focus must hand shortcuts back to the shell")
	}
}

func seedLocal(t *testing.T, pb *privBackend, d drafts.Draft) drafts.Draft {
	t.Helper()
	out, err := pb.store.Save(context.Background(), pb.scope, d, 0)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPrivateInboxMixesLocalAndGitHubRowsAndArchives(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	seeded := seedLocal(t, pb, drafts.Draft{Title: "Local idea", Type: garden.TypeBug, Spec: spec.Spec{CurrentBehavior: "p"}})
	for _, sz := range draftSizes {
		m := e.open(t, pb, sz[0], sz[1])
		mustContain(t, "inbox", view(m), "LOCAL DRAFTS", drafts.DraftLabel(seeded.ID), "Local idea", "GITHUB DRAFT ISSUES", "#142", "2 missing")
		mustFit(t, "inbox", m)
		if m.garden.drafts.selectedLocal != seeded.ID {
			t.Fatalf("local row not selected first: %q", m.garden.drafts.selectedLocal)
		}
	}
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "down")
	d := m.garden.drafts
	if d.selectedLocal != "" || d.selected == 0 {
		t.Fatalf("down must move to a GitHub row: %q / %d", d.selectedLocal, d.selected)
	}
	m = pk(t, m, "enter")
	if m.garden.drafts.stage != draftsDetail {
		t.Fatalf("GitHub row enter: stage %d", m.garden.drafts.stage)
	}
	m = pk(t, m, "esc", "up")
	if m.garden.drafts.selectedLocal != seeded.ID {
		t.Fatal("up must return to the local row")
	}
	m = pk(t, m, "a")
	mustContain(t, "archived", view(m), "Archived "+drafts.DraftLabel(seeded.ID)+". u to undo.")
	mustNotContain(t, "archived", view(m), "Local idea")
	m = pk(t, m, "u")
	mustContain(t, "restored", view(m), "Local idea")
	// A toggles archived rows.
	m = pk(t, m, "a", "A", "up")
	mustContain(t, "archived view", view(m), "Local idea", "archived")
	m = pk(t, m, "u")
	if l, ok := m.garden.drafts.selectedLocalDraft(); !ok || !l.ArchivedAt.IsZero() {
		t.Fatalf("u on an archived row must restore it: %+v", l)
	}
}

func TestPrivateInboxSearchAndTypeFilterApplyToLocalRows(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	seedLocal(t, pb, drafts.Draft{Title: "Alpha idea", Type: garden.TypeBug})
	seedLocal(t, pb, drafts.Draft{Title: "Beta idea", Type: garden.TypeChore})
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "/")
	m = ptype(t, m, "beta")
	m = pk(t, m, "enter")
	out := view(m)
	mustContain(t, "search", out, "Beta idea")
	mustNotContain(t, "search", out, "Alpha idea")
	m = pk(t, m, "x", "t") // clear, then type filter cycles to the first type (Feature)
	if got := len(m.garden.drafts.locals()); got != 0 {
		t.Fatalf("Feature filter must hide the Bug and Chore drafts, got %d", got)
	}
}

func TestPrivateUnavailableKeepsLegacyCapture(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	pb.unavailable = true
	m := e.open(t, pb, 120, 35)
	mustContain(t, "notice", view(m), "Private drafts are unavailable")
	m = pk(t, m, "n")
	if m.garden.drafts.stage != draftsForm {
		t.Fatalf("n must open the legacy capture form, stage %d", m.garden.drafts.stage)
	}
	mustNotContain(t, "legacy", view(m), "WHAT'S ON YOUR MIND?")
}

func TestPrivateBackendWithoutStoreKeepsLegacyCapture(t *testing.T) {
	m := draftsEnv(t, draftsFake(t), 120, 35)
	if _, ok := m.privateDrafts(); ok {
		t.Fatal("a backend without the optional interface has no private drafts")
	}
	m = pressAll(t, m, "n")
	if m.garden.drafts.stage != draftsForm {
		t.Fatalf("stage %d", m.garden.drafts.stage)
	}
}

func TestPrivateExtraSectionsRoundTrip(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	text := "## Summary\nShort.\n\n## Current behavior\nBroken.\n\n## Constraints\nNo new deps.\n\n## Rollout\nBehind a flag.\n\n## Notes\nTabs\tkept?"
	parsed := spec.Parse(text)
	if len(parsed.Extra) != 2 {
		t.Fatalf("parse: %+v", parsed.Extra)
	}
	seeded := seedLocal(t, pb, drafts.Draft{Title: "Round trip", Spec: parsed})
	m := e.open(t, pb, 120, 35)
	m = pk(t, m, "enter")
	out := view(m)
	mustContain(t, "more", out, "Summary", "Constraints", "Rollout", "Notes")
	m = pk(t, m, "enter") // Problem text
	m = ptick(t, ptype(t, m, "X"))
	got, err := pb.store.Read(context.Background(), pb.scope, seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.CurrentBehavior != "XBroken." || got.Spec.Summary != parsed.Summary || got.Spec.Constraints != parsed.Constraints {
		t.Fatalf("fixed sections changed: %+v", got.Spec)
	}
	if len(got.Spec.Extra) != 2 || got.Spec.Extra[0] != parsed.Extra[0] || got.Spec.Extra[1] != parsed.Extra[1] {
		t.Fatalf("extra sections changed: %+v vs %+v", got.Spec.Extra, parsed.Extra)
	}
	if spec.Render(got.Spec) != spec.Render(spec.Spec{Summary: parsed.Summary, CurrentBehavior: "XBroken.", Constraints: parsed.Constraints, Extra: parsed.Extra}) {
		t.Fatal("render differs")
	}
}

func TestPrivateScreensFitEverySize(t *testing.T) {
	for _, sz := range draftSizes {
		w, h := sz[0], sz[1]
		label := fmt.Sprintf("%dx%d", w, h)
		e := newPrivEnv(t)
		pb := e.backend(t)
		m := e.open(t, pb, w, h)
		m = pk(t, m, "n")
		mustFit(t, label+" capture", m)
		out := view(m)
		mustContain(t, label+" capture", out, "WHAT'S ON YOUR MIND?", "Continue writing →", "Discuss with AI…", "Ctrl+S save draft", "Draft / Description")
		focusLine := lineWith(out, "›")
		if focusLine == "" {
			t.Fatalf("%s: no focus marker in capture", label)
		}
		m = ptick(t, ptype(t, m, "Switching accounts loses the login I was using."))
		m = pk(t, m, "tab")
		if l := lineWith(view(m), "▸Continue writing"); l == "" {
			t.Fatalf("%s: focus did not move to Continue:\n%s", label, view(m))
		}
		m = pk(t, m, "enter")
		mustFit(t, label+" editor", m)
		out = view(m)
		mustContain(t, label+" editor", out, "Review & create issue →", "Tab section", "Ctrl+S save draft", "Ctrl+Enter review", "Draft / D-")
		if w >= 100 {
			mustContain(t, label+" editor", out, "SECTIONS", "Invite assistant…", "○ Outcome missing")
			if !strings.Contains(out, "▸ Problem") {
				t.Fatalf("%s: rail focus marker missing:\n%s", label, out)
			}
		} else {
			mustNotContain(t, label+" editor", out, "SECTIONS")
			mustContain(t, label+" editor", out, "Problem", "Outcome", "Req", "Criteria", "Questions", "▸Problem", "1/3 ready")
		}
		m = pk(t, m, "enter")
		mustFit(t, label+" writing", m)
		if !strings.Contains(view(m), "▌") && w >= 100 {
			t.Fatalf("%s: text focus marker missing", label)
		}
		m = pk(t, m, "esc", "ctrl+r")
		mustFit(t, label+" review", m)
		mustContain(t, label+" review", view(m), "REVIEW & CREATE ISSUE", "unavailable", "Not ready:")
		m = pk(t, m, "esc", "esc")
		mustFit(t, label+" inbox", m)
		mustContain(t, label+" inbox", view(m), "LOCAL DRAFTS", "Switching accounts")
	}
}

func lineWith(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(l), needle) {
			return l
		}
	}
	return ""
}

func TestPrivateReadinessReasonsAreConcrete(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	seedLocal(t, pb, drafts.Draft{Title: "T", Spec: spec.Spec{CurrentBehavior: "p", DesiredBehavior: "o", AcceptanceCriteria: "c", OpenQuestions: "- one\n- two\n- None"}})
	m := e.open(t, pb, 120, 35)
	mustContain(t, "inbox", view(m), "2 questions")
	m = pk(t, m, "enter")
	mustContain(t, "editor", view(m), "○ 2 open questions", "✓ Problem stated")
}

func TestPrivateStateFileHoldsNoText(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "secret idea text"))
	m = pk(t, m, "tab", "enter", "esc", "esc")
	cfg := os.Getenv("XDG_CONFIG_HOME")
	b, err := os.ReadFile(filepath.Join(cfg, "bonsai", "state.json"))
	if err != nil {
		t.Skipf("no state file: %v", err)
	}
	if strings.Contains(string(b), "secret idea text") {
		t.Fatal("authored text leaked into state.json")
	}
	if !strings.Contains(string(b), "garden_drafts") {
		t.Fatalf("selection was not remembered: %s", b)
	}
}

func TestPrivateEpochChangeKeepsUnwrittenDraftAndIgnoresLateReplies(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	pb.saveErr = errors.New("disk full")
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "unwritten"))
	p := m.garden.drafts.private
	old := privSavedMsg{repo: p.repo, token: p.token, gen: p.gen, draft: drafts.Draft{ID: "draft-late", Revision: 9}}
	v := m.garden.status
	v.Stamp.Epoch += 5
	m, _ = m.gardenAdopt(v, true)
	if m.garden.drafts.private != p || p.desc.Value() != "unwritten" {
		t.Fatal("unwritten text must survive an epoch change")
	}
	pb.saveErr = nil
	m = pk(t, m, "n") // parked text is reopened, never replaced
	if m.garden.drafts.private != p {
		t.Fatal("a fresh draft replaced unwritten text")
	}
	m, _ = send(t, m, privSavedMsg{repo: old.repo, token: old.token + 100, gen: old.gen, draft: old.draft})
	if p.draft.ID == "draft-late" {
		t.Fatal("late reply from another token applied")
	}
	m, _ = send(t, m, old) // same token, but the draft is not the one saved: counters never reset
	m.garden.drafts.privToken++
	if m.garden.drafts.privToken <= p.token {
		t.Fatal("token counter restarted")
	}
}

func TestPrivateQuitAfterClearedByConflictAndSecondCtrlCQuits(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	m := e.open(t, pb, 120, 35)
	m = ptick(t, ptype(t, pk(t, m, "n"), "one"))
	id := m.garden.drafts.private.draft.ID
	other := newPrivStore(t, e.file, "bb")
	cur, _ := other.Read(context.Background(), pb.scope, id)
	cur.Title = "theirs"
	if _, err := other.Save(context.Background(), pb.scope, cur, cur.Revision); err != nil {
		t.Fatal(err)
	}
	m, _ = send(t, m, runeKey("!"))
	m, _ = send(t, m, pkey("ctrl+c"))
	m, cmd := send(t, m, pkey("enter")) // Save and quit
	m, seen := drainAll(t, m, cmd)
	for _, s := range seen {
		if _, q := s.(tea.QuitMsg); q {
			t.Fatal("quit on conflict")
		}
	}
	p := m.garden.drafts.private
	if p.state != saveConflict || p.quitAfter {
		t.Fatalf("state %d quitAfter %v", p.state, p.quitAfter)
	}
	m = pk(t, m, "k")
	if m.modal != nil || m.garden.drafts.private.state != saveClean {
		t.Fatal("k must keep mine without quitting")
	}
	// Second ctrl+c while the dialog is open quits without saving.
	pb.saveErr = errors.New("x")
	m = ptick(t, ptype(t, m, "?"))
	m, _ = send(t, m, pkey("ctrl+c"))
	if m.modal == nil {
		t.Fatal("dialog expected")
	}
	m, cmd = send(t, m, pkey("ctrl+c"))
	_, seen = drainAll(t, m, cmd)
	quit := false
	for _, s := range seen {
		if _, q := s.(tea.QuitMsg); q {
			quit = true
		}
	}
	if !quit {
		t.Fatalf("second ctrl+c did not quit: %v", seen)
	}
}

func TestPrivateArchiveStaleReplyAndBlankDuringFirstSave(t *testing.T) {
	e := newPrivEnv(t)
	pb := e.backend(t)
	seeded := seedLocal(t, pb, drafts.Draft{Title: "A"})
	m := e.open(t, pb, 120, 35)
	newer := seeded
	newer.Revision = 5
	m.garden.drafts.privList = privUpsert(m.garden.drafts.privList, newer)
	stale := seeded
	stale.Revision = 3
	stale.ArchivedAt = newer.CreatedAt.Add(1)
	m, _ = send(t, m, privArchiveMsg{repo: m.privRepo(), draft: stale})
	if m.garden.drafts.notice != "" && strings.Contains(m.garden.drafts.notice, "Archived") {
		t.Fatal("stale archive reply applied")
	}
	// Deleting the text while the first save is in flight must not read as saved.
	m = pk(t, m, "n")
	m = ptype(t, m, "a")
	p := m.garden.drafts.private
	m, _ = send(t, m, privTickMsg{p.token, p.gen})
	m = pk(t, m, "backspace")
	if strings.Contains(view(m), "✓ Saved") || p.state == saveClean {
		t.Fatalf("pending write shown as saved: state %d", p.state)
	}
}
