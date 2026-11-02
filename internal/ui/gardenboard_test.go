package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func boardItem(t *testing.T, id int, stage garden.Stage, plan bool) gardensync.Item {
	t.Helper()
	repo := garden.RepoID(gardenfake.StatusFor(gardenapi.StateConnected).Target.RepoID)
	spec := "Specification\n\n" + strings.Repeat("Keep 界 and é safe. ", 30)
	hash := garden.ContentHash(spec)
	meta, err := garden.EncodeMetadata(garden.Metadata{SchemaVersion: 1, Provenance: "fixture", Owner: "1001", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash})
	if err != nil {
		t.Fatal(err)
	}
	var comments []garden.Comment
	add := func(n int64, e garden.CommentEnvelope, text string) {
		t.Helper()
		e.SchemaVersion = 1
		e.RepoID = repo
		e.IssueID = garden.IssueID(id)
		raw, err := garden.EncodeCommentEnvelope(e, text)
		if err != nil {
			t.Fatal(err)
		}
		comments = append(comments, garden.Comment{ID: n, AuthorID: "1001", Body: raw})
	}
	add(1, garden.CommentEnvelope{Kind: garden.CommentRevision, RevisionKind: garden.RevisionSpec, ContentHash: hash}, spec)
	add(2, garden.CommentEnvelope{Kind: garden.CommentApproval, RevisionHash: hash, RevisionCommentID: 1, SourceSpecFingerprint: hash}, "Accepted")
	if plan {
		add(3, garden.CommentEnvelope{Kind: garden.CommentRevision, RevisionKind: garden.RevisionPlan, ContentHash: garden.ContentHash("Canonical plan text"), SourceSpecFingerprint: hash}, "Canonical plan text")
	}
	issue := garden.Issue{Repo: repo, ID: garden.IssueID(id), Number: int64(id + 100), Title: fmt.Sprintf("Task %d 界 é %s", id, strings.Repeat("long title ", 8)), Body: "<!-- bonsai:garden-metadata/v1 hidden -->\n# Overview\n" + strings.Repeat("Readable issue body 界 é. ", 40) + "\n## Open questions\nWhat should happen next?\n## Notes\nKeep context.", Metadata: meta, Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(stage), "priority:p1"}}
	v := garden.Verify(garden.IssueIdentity{Repo: repo, Issue: issue.ID}, comments, garden.ApprovalPolicy{AllowedActorIDs: []string{"1001"}})
	c := garden.Classify(issue.Labels)
	m, err := garden.DecodeMetadata(meta)
	if err != nil {
		t.Fatal(err)
	}
	lane := garden.Lane(c, m, v, nil)
	if lane.Lane != garden.LaneWork {
		t.Fatalf("fixture: %+v", lane)
	}
	return gardensync.Item{Issue: issue, Classification: c, Verification: v, Lane: lane}
}
func boardFake(t *testing.T) *gardenfake.Backend {
	t.Helper()
	fb := gardenFake(gardenapi.StateConnected)
	for _, spec := range []struct {
		stage garden.Stage
		plan  bool
	}{{garden.StageReady, false}, {garden.StageReady, true}, {garden.StageInProgress, false}, {garden.StageReview, false}, {garden.StageDone, false}, {garden.StageBlocked, false}} {
		for n := 0; n < 6; n++ {
			id := len(fb.WorkView.Snapshot.Items) + 1
			fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, boardItem(t, id, spec.stage, spec.plan))
		}
	}
	return fb
}
func TestGardenBoardKeyboardEveryColumnEveryCardAndRoundTrip(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			fb := boardFake(t)
			m := gardenEnv(t, fb, config.Prefs{}, sz[0], sz[1])
			m = pressAll(t, m, "d")
			expected := m.garden.board.collection
			for col := work.Column(0); col < work.ColumnCount; col++ {
				if m.garden.board.selection.Column != col {
					t.Fatalf("column %d: %+v", col, m.garden.board.selection)
				}
				records := expected.Columns[col]
				if len(records) == 0 {
					if m.garden.board.selection.ID != 0 {
						t.Fatal("empty column has selected item")
					}
					before := m.garden.board.selection
					m = pressAll(t, m, "enter")
					if m.garden.board.opened != 0 || !reflect.DeepEqual(before, m.garden.board.selection) {
						t.Fatal("empty Enter changed selection")
					}
				}
				for row, r := range records {
					if m.garden.board.selection.ID != r.Item.Issue.ID {
						t.Fatalf("column %d row %d: %+v", col, row, m.garden.board.selection)
					}
					s := m.garden.board.selection
					if int(col) < s.Window || int(col) >= s.Window+gardenBoardColumns(sz[0], work.ModeDetailed) || row < s.Scroll[col] || row >= s.Scroll[col]+gardenBoardCapacity(sz[1]-9) {
						t.Fatal("selected card is outside moving window/scroll")
					}
					before := s
					m = pressAll(t, m, "enter")
					if m.garden.board.opened != s.ID {
						t.Fatal("Enter opened another issue")
					}
					mustContain(t, "detail", view(m), fmt.Sprintf("Issue #%d", r.Item.Issue.Number), "esc Back to Board", "Issue / Overview")
					m = pressAll(t, m, "esc")
					if m.garden.board.opened != 0 || !reflect.DeepEqual(m.garden.board.selection, before) {
						t.Fatal("Escape did not restore exact card and scroll")
					}
					if row < len(records)-1 {
						m = pressAll(t, m, "down")
					}
				}
				assertBounded(t, "board", m.View(), sz[0], sz[1])
				if col < work.ColumnCount-1 {
					m = pressAll(t, m, "right")
				}
			}
			if fb.Writes() != 0 {
				t.Fatal("selection/open mutated backend")
			}
		})
	}
}
func TestGardenBoardRefreshReorderFilteringDeletionAndUnrelatedUpdates(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "down", "down", "down")
	before := m.garden.board.selection
	slices.Reverse(fb.WorkView.Snapshot.Items)
	m = pressAll(t, m, "R")
	if !reflect.DeepEqual(m.garden.board.selection, before) {
		t.Fatal("reorder changed stable identity or scroll")
	}
	m, _ = send(t, m, struct{ Unrelated bool }{true})
	if !reflect.DeepEqual(m.garden.board.selection, before) || m.shellFocus != shellBody {
		t.Fatal("unrelated event stole focus")
	}
	m = pressAll(t, m, "enter", "right", "down")
	opened, section, scroll := m.garden.board.opened, m.garden.board.section, m.garden.board.docScroll
	m = pressAll(t, m, "R")
	if m.garden.board.opened != opened || m.garden.board.section != section || m.garden.board.docScroll != scroll {
		t.Fatal("refresh lost document context")
	}
	fb.WorkView.Snapshot.Items = slices.DeleteFunc(fb.WorkView.Snapshot.Items, func(it gardensync.Item) bool { return it.Issue.ID == opened })
	m = pressAll(t, m, "R")
	if m.garden.board.opened != 0 || m.garden.board.selection.ID == opened || !strings.Contains(m.garden.board.notice, "left this view") {
		t.Fatal("deletion did not fall back from opened issue")
	}
	selected := m.garden.board.selection.ID
	m = pressAll(t, m, "t", "t")
	if m.garden.board.query.Type != garden.TypeBug || m.garden.board.selection.ID != 0 || len(m.garden.board.collection.Records) != 0 {
		t.Fatal("type filter did not exclude feature")
	}
	mustContain(t, "filtered", view(m), "filtered / 35", "(empty)")
	m = pressAll(t, m, "x")
	if m.garden.board.selection.ID == 0 || selected == 0 {
		t.Fatal("clear did not select available card")
	}
	m = pressAll(t, m, "p")
	if len(m.garden.board.collection.Records) != 0 {
		t.Fatal("P0 admitted P1 work")
	}
	m = pressAll(t, m, "p")
	if len(m.garden.board.collection.Records) != 35 {
		t.Fatal("P1 lost work")
	}
	if fb.Writes() != 0 {
		t.Fatal("refresh/filter mutated backend")
	}
}
func TestGardenBoardSearchCapturesKeysAndCancelRestoresSelection(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "down", "down")
	before := m.garden.board.selection
	m = pressAll(t, m, "/")
	if m.currentShellFocus() != shellInput {
		t.Fatal("search did not own input")
	}
	for _, r := range "q?v" {
		var cmd tea.Cmd
		m, cmd = send(t, m, runeKey(string(r)))
		if cmd != nil {
			if _, ok := cmd().(tea.QuitMsg); ok {
				t.Fatal("typed q quit")
			}
		}
	}
	if m.garden.board.query.Text != "q?v" || m.modal != nil {
		t.Fatal("search key escaped its scope")
	}
	m = pressAll(t, m, "esc")
	if m.garden.board.query.Active() || !reflect.DeepEqual(m.garden.board.selection, before) || m.shellFocus != shellBody {
		t.Fatal("cancel lost query/selection/focus")
	}
	m = pressAll(t, m, "/")
	m, _ = send(t, m, runeKey("#101"))
	m = pressAll(t, m, "enter")
	if len(m.garden.board.collection.Records) != 1 || m.garden.board.selection.ID != 1 {
		t.Fatal("text filter did not match issue number")
	}
	m = pressAll(t, m, "x")
	labels, _ := paletteTargets()
	for _, label := range []string{"Garden: Board", "Garden: Search approved work", "Garden: Refresh"} {
		if !slices.Contains(labels, label) {
			t.Fatal("missing palette entry", label)
		}
	}
	m = m.openShellHelp()
	mustContain(t, "help", ansi.Strip(m.modal.View()), "search", "type/priority")
}
func TestGardenBoardOfflineNeverLooksEmptyAndOnlySuccessClears(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	before := m.garden.board.selection
	last := m.garden.board.snapshot.SuccessfulAt
	fb.WorkErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub is offline."}
	m = pressAll(t, m, "R")
	if !reflect.DeepEqual(m.garden.board.selection, before) || len(m.garden.board.collection.Records) != 36 || !m.garden.board.snapshot.SuccessfulAt.Equal(last) {
		t.Fatal("failed refresh replaced last good data")
	}
	mustContain(t, "offline", view(m), "offline / stale", "GitHub is offline.", "Last good board retained.")
	fb.WorkErr = nil
	fb.WorkView.Snapshot.Items = nil
	m = pressAll(t, m, "R")
	if len(m.garden.board.collection.Records) != 0 || m.garden.board.selection.ID != 0 {
		t.Fatal("successful empty did not clear")
	}
	mustContain(t, "empty", view(m), "APPROVED WORK  0", "(empty)", "synced")
}
func TestGardenBoardLateForeignRepliesAndScopeChange(t *testing.T) {
	for _, kind := range []string{"request", "generation", "clone", "garden", "epoch", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			fb := boardFake(t)
			m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
			before := m.garden.board.selection
			var cmd tea.Cmd
			m, cmd = m.gardenLoadWork()
			msg := cmd().(gardenWorkMsg)
			switch kind {
			case "request":
				msg.seq--
			case "generation":
				msg.view.Stamp.Generation = 1
			case "clone":
				msg.view.Stamp.LocalRepoID = "other"
			case "garden":
				msg.view.Stamp.GardenID++
			case "epoch":
				msg.view.Stamp.Epoch--
			case "snapshot":
				msg.view.Snapshot.LocalRepoID = "other"
			}
			msg.view.Snapshot.Items = nil
			m, _ = send(t, m, msg)
			if !reflect.DeepEqual(m.garden.board.selection, before) || len(m.garden.board.collection.Records) != 36 || m.shellFocus != shellBody {
				t.Fatal("late/foreign reply changed board or focus")
			}
		})
	}
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	v := m.garden.status
	v.Stamp.Epoch++
	v.Stamp.GardenID++
	v.Target.RepoID++
	v.Stamp.Generation += 100
	m.garden.seq++
	m, _ = send(t, m, gardenStatusMsg{seq: m.garden.seq, view: v})
	if len(m.garden.board.collection.Records) != 0 || m.garden.board.opened != 0 {
		t.Fatal("former Garden leaked after reassociation")
	}
}
func TestGardenBoardDocumentAllSectionsAndLongText(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "right", "enter")
	for n, want := range []string{"Readable issue body", "Keep 界 and é safe.", "What should happen next?", "Latest", "Local implementation launch is unavailable in this runtime."} {
		m.garden.board.section = n
		lines := strings.Join(m.gardenDocument(80), "\n")
		mustContain(t, "document section", flat(ansi.Strip(lines)), want)
		mustNotContain(t, "metadata", lines, "hidden -->")
		m = pressAll(t, m, "end")
		if m.garden.board.docScroll < 0 {
			t.Fatal("negative document scroll")
		}
		m = pressAll(t, m, "home")
		if m.garden.board.docScroll != 0 {
			t.Fatal("Home did not return to start")
		}
		assertBounded(t, "detail", m.View(), 80, 24)
	}
}
func TestGardenBoardAllSizesThemesAndASCIIFitIndependentRegions(t *testing.T) {
	for _, preset := range theme.Presets() {
		for _, ascii := range []bool{false, true} {
			for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
				t.Run(fmt.Sprintf("%s/%t/%dx%d", preset, ascii, sz[0], sz[1]), func(t *testing.T) {
					fb := boardFake(t)
					glyphs := "unicode"
					if ascii {
						glyphs = "ascii"
					}
					m := gardenEnv(t, fb, config.Prefs{Theme: preset, AgentGlyphs: glyphs}, sz[0], sz[1])
					for col := 0; col < int(work.ColumnCount); col++ {
						m.gardenBoardVisible()
						out := m.View()
						assertBounded(t, "full screen", out, sz[0], sz[1])
						if len(strings.Split(out, "\n")) != sz[1] {
							t.Fatal("incorrect row reservation")
						}
						body := m.gsBoard(sz[0], sz[1]-9)
						for i, line := range body {
							if lipgloss.Width(line) != sz[0] {
								t.Fatalf("row %d has %d cells", i, lipgloss.Width(line))
							}
						}
						if sz[0] >= 120 {
							header := ansi.Strip(body[2])
							divider := "│"
							if ascii {
								divider = "|"
							}
							wantDividers := min(gardenBoardColumns(sz[0], work.ModePhases), 6) - 1
							if ascii {
								wantDividers *= 2
							}
							if strings.Count(header, divider) != wantDividers {
								t.Fatal("column boundary missing", header)
							}
						} else if m.garden.board.selection.Window > int(m.garden.board.selection.Column) {
							t.Fatal("window lost selected column")
						}
						if m.garden.board.selection.ID != 0 {
							m = pressAll(t, m, "enter")
							for n := range gardenDocumentSections {
								m.garden.board.section = n
								assertBounded(t, "detail", m.View(), sz[0], sz[1])
							}
							m = pressAll(t, m, "esc")
						}
						m = pressAll(t, m, "right")
					}
					if ascii {
						for _, line := range m.settingsASCII([]string{"‹ › – ✓ │ ▌ … ← →"}) {
							for _, r := range ansi.Strip(line) {
								if r > 127 {
									t.Fatalf("chrome lacks ASCII equivalent %q", r)
								}
							}
						}
					}
				})
			}
		}
	}
}
func TestGardenBoardViewMemoryPerRepositorySanitized(t *testing.T) {
	fb := gardenFake(gardenapi.StateReadOnly)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrBoard {
		t.Fatal("read-only board did not open")
	}
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.GardenView(m.repoDir) != "board" || st.GardenView(t.TempDir()) != "" {
		t.Fatal("Garden memory not repository scoped")
	}
	m2 := sized(t, New(m.repoDir, &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(gardenFake(gardenapi.StateReadOnly))), 80, 24)
	m2, _ = drainAll(t, m2, m2.Init())
	if m2.gardenScreen() != scrBoard {
		t.Fatal("remembered board did not reopen")
	}
	m = pressAll(t, m, "esc")
	st, err = config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.GardenView(m.repoDir) != "setup" {
		t.Fatal("parent view not remembered")
	}
}
func TestGardenBoardSelectionDoesNotAliasOtherModels(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	before := m.garden.board.selection
	next := pressAll(t, m, "down")
	if reflect.DeepEqual(next.garden.board.selection, before) || !reflect.DeepEqual(m.garden.board.selection, before) {
		t.Fatal("selection value mutated previous model")
	}
}
func TestGardenBoardRefreshInFlightAllowsNavigationAndDoesNotStealFocus(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m, cmd := m.gardenLoadWork()
	m = pressAll(t, m, "right", "right", "down")
	s := m.garden.board.selection
	m, _ = send(t, m, cmd())
	if !reflect.DeepEqual(m.garden.board.selection, s) || m.shellFocus != shellBody {
		t.Fatal("refresh stole cursor")
	}
	m, cmd = m.gardenLoadWork()
	m = pressAll(t, m, "esc", "esc")
	m, _ = send(t, m, cmd())
	if m.shellFocus != shellNav {
		t.Fatal("late refresh reclaimed body")
	}
}

func TestGardenBoardStageMoveKeepsIdentityAndFocus(t *testing.T) {
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "down")
	id := m.garden.board.selection.ID
	for i := range fb.WorkView.Snapshot.Items {
		if fb.WorkView.Snapshot.Items[i].Issue.ID == id {
			it := &fb.WorkView.Snapshot.Items[i]
			it.Issue.Labels = slices.Clone(it.Issue.Labels)
			it.Issue.Labels[2] = garden.FormatStageLabel(garden.StageDone)
		}
	}
	m = pressAll(t, m, "R")
	if m.garden.board.selection.ID != id || m.garden.board.selection.Column != work.Column(work.DonePhase) || m.shellFocus != shellBody || m.garden.board.selection.Window != 3 {
		t.Fatalf("stage move lost selection: %+v", m.garden.board.selection)
	}
}
func TestGardenBoardIndicatorsAndTerminalControlSanitization(t *testing.T) {
	it := boardItem(t, 1, garden.StageReady, false)
	for _, tc := range []struct {
		name   string
		change func(*work.Record)
		want   string
	}{
		{"approved", func(*work.Record) {}, "spec approved"},
		{"projection conflict", func(r *work.Record) {
			r.Findings = []gardensync.Finding{{Code: gardensync.FindingProjectionDrift, IssueID: 1}}
		}, "conflict"},
		{"stale plan", func(r *work.Record) {
			r.Item.Verification.Heads = slices.Clone(r.Item.Verification.Heads)
			r.Item.Verification.Heads = append(r.Item.Verification.Heads, garden.Revision{Kind: garden.RevisionPlan, SourceSpecFingerprint: garden.ContentHash("old spec")})
		}, "stale plan"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := work.Record{Item: it}
			tc.change(&r)
			got, _ := gardenApprovalIndicator(r)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("%q != %q", got, tc.want)
			}
		})
	}
	fb := boardFake(t)
	fb.WorkView.Snapshot.Items[0].Issue.Title = "界é\n\x1b[2J\x1b]0;hostile\a" + strings.Repeat("界é", 100)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	assertBounded(t, "hostile title", m.View(), 80, 24)
	if len(strings.Split(m.View(), "\n")) != 24 {
		t.Fatal("title escaped measured row")
	}
	if strings.Contains(m.gardenBoardIdentity(), "\x1b") || strings.Contains(m.gardenBoardIdentity(), "\n") {
		t.Fatal("footer contains terminal control or newline")
	}
}
func TestGardenBoardNoSuccessfulReadIsUnknownRatherThanEmpty(t *testing.T) {
	fb := gardenFake(gardenapi.StateConnected)
	fb.WorkView.Snapshot = gardensync.Snapshot{Source: gardensync.SourceStale, Problems: []gardensync.Problem{{Code: "offline"}}}
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	mustContain(t, "unknown", view(m), "No successful read yet", "approved work is unknown", "offline / stale", "sync never")
}

func TestGardenStatusPreservesAdoptAndWorkCommands(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := Model{garden: gardenState{api: fb}}
	m, cmd := m.gardenLoadStatus()
	msg := cmd().(gardenStatusMsg)
	msg.view.Ambiguous, msg.view.Caps.Read = true, true
	msg.remembered = "board"
	m, cmd = m.onGardenStatus(msg)
	if m.gardenScreen() != scrBoard || cmd == nil {
		t.Fatal("board not loaded")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatal("adopt command was replaced by the work load")
	}
	for _, run := range batch {
		run()
	}
	if fb.Count("Candidates") != 1 || fb.Count("Work") != 1 {
		t.Fatal("both read commands must survive")
	}
}

func TestGardenStatusAndMemoryAllowNilState(t *testing.T) {
	m := Model{garden: gardenState{api: gardenFake(gardenapi.StateNone)}}
	m, cmd := m.gardenLoadStatus()
	msg := cmd().(gardenStatusMsg)
	if msg.err != nil || msg.remembered != "" {
		t.Fatalf("nil-state status: %+v", msg)
	}
	m, _ = m.onGardenStatus(msg)
	if !m.garden.hasStatus || m.gardenRememberView() != nil {
		t.Fatal("nil state prevented status or scheduled persistence")
	}
}

func TestGardenViewMemoryWritesOnlyOnChangeAndRetriesFailure(t *testing.T) {
	fb := gardenFake(gardenapi.StateReadOnly)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "enter")
	gen := m.garden.board.saveGen
	m = pressAll(t, m, "R", "R")
	if m.garden.board.saveGen != gen || m.gardenRememberView() != nil {
		t.Fatal("unchanged refresh scheduled state writes")
	}
	m = pressAll(t, m, "esc")
	if m.garden.board.saveGen != gen+1 || m.garden.board.rememberedView != "setup" {
		t.Fatal("changed view was not persisted")
	}
	m = m.gardenBoardSaved(gardenViewSavedMsg{gen: m.garden.board.saveGen, err: config.ErrInvalidGardenView})
	if m.gardenRememberView() == nil {
		t.Fatal("failed view write cannot retry")
	}
}

func TestGardenBoardPageMovementUsesBodyGeometry(t *testing.T) {
	for _, height := range []int{24, 35, 48} {
		m := gardenEnv(t, boardFake(t), config.Prefs{}, 120, height)
		regions, _ := shellGeometry(m.width, height, settingsSubmenuRows, 0)
		if m.gardenBoardRows() != regions.body {
			t.Fatal("board geometry disagrees with shell")
		}
		page := gardenBoardCapacity(regions.body)
		m = pressAll(t, m, "pgdown")
		if m.garden.board.selection.Rows[work.Approved] != page {
			t.Fatal("page down used different geometry")
		}
		m = pressAll(t, m, "pgup")
		if m.garden.board.selection.Rows[work.Approved] != 0 {
			t.Fatal("page up did not restore row")
		}
	}
}

func TestGardenBoardAcceptedProblemsRenderAsConflict(t *testing.T) {
	fb := boardFake(t)
	fb.WorkView.Snapshot.Items[0].Lane.Lane = garden.LaneAttention
	fb.WorkView.Snapshot.Items[0].Problems = []gardensync.Problem{{Code: "display-conflict", Message: "Display projection needs attention"}}
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	if len(m.garden.board.collection.Records) != 36 {
		t.Fatal("accepted conflict disappeared")
	}
	mustContain(t, "conflict card", view(m), "! conflict")
	m = pressAll(t, m, "enter")
	mustContain(t, "conflict document", flat(ansi.Strip(strings.Join(m.gardenDocument(80), "\n"))), "Attention: Display projection needs attention")
}

func TestBoardSafeStripsFormatControlsAndPreservesGraphemes(t *testing.T) {
	text := "界e\u0301 👩\u200d💻 ❤️"
	controls := "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\u200b\u00ad\u2060\ufeff"
	got := boardSafe(controls + text + controls)
	if got != text || lipgloss.Width(got) != lipgloss.Width(text) {
		t.Fatalf("graphemes changed: %q", got)
	}
	if boardTitle(controls+text) != text {
		t.Fatal("title retained format controls")
	}
}

func TestGardenBoardASCIIComponentsContainOnlyASCIIChrome(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		fb := boardFake(t)
		for i := range fb.WorkView.Snapshot.Items {
			it := &fb.WorkView.Snapshot.Items[i]
			it.Issue.Title = strings.Repeat("Long ASCII title ", 10)
			it.Issue.Body = "## Questions\nASCII question?"
			for j := range it.Verification.Revisions {
				it.Verification.Revisions[j].Content = "ASCII content"
			}
		}
		m := gardenEnv(t, fb, config.Prefs{AgentGlyphs: "ascii"}, width, 35)
		check := func(lines []string) {
			t.Helper()
			for _, r := range ansi.Strip(strings.Join(lines, "\n")) {
				if r > 127 {
					t.Fatalf("non-ASCII chrome %q in %d-col render", r, width)
				}
			}
		}
		check(m.gsBoard(width, m.gardenBoardRows()))
		check(strings.Split(m.View(), "\n"))
		m = pressAll(t, m, "enter")
		for section := range gardenDocumentSections {
			m.garden.board.section = section
			check(m.gardenDocument(width))
			check(strings.Split(m.View(), "\n"))
		}
	}
}

func TestGardenSearchApplyAndCancelClearReselectNotice(t *testing.T) {
	for _, exit := range []string{"enter", "esc"} {
		m := gardenEnv(t, boardFake(t), config.Prefs{}, 80, 24)
		m, cmd := m.gardenStartSearch()
		if cmd != nil {
			t.Fatal("static search scheduled a blink")
		}
		m = pressAll(t, m, "missing")
		if m.garden.board.notice == "" {
			t.Fatal("test did not filter selection out")
		}
		m = pressAll(t, m, exit)
		if m.garden.board.notice != "" || m.garden.board.searching {
			t.Fatal("search exit retained stale notice")
		}
	}
}

func TestGardenQuestionsIgnoreFencedHeadings(t *testing.T) {
	for _, fence := range []string{"```", "~~~~"} {
		fb := boardFake(t)
		fb.WorkView.Snapshot.Items[0].Issue.Body = fence + "markdown\n# Questions\nFalse question\n" + fence + "\n## Notes\nNot a question"
		m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
		m = pressAll(t, m, "enter")
		m.garden.board.section = 2
		doc := ansi.Strip(strings.Join(m.gardenDocument(80), "\n"))
		mustContain(t, "fenced heading", flat(doc), "No Questions section recorded")
		mustNotContain(t, "fenced heading", doc, "False question")
		// A heading inside a question's code sample must not close the real section.
		fb.WorkView.Snapshot.Items[0].Issue.Body = "## Questions\nReal question\n" + fence + "\n# Notes\nCode sample\n" + fence + "\nStill a question\n## Notes\nOutside"
		m = pressAll(t, m, "R")
		m.garden.board.section = 2
		doc = ansi.Strip(strings.Join(m.gardenDocument(80), "\n"))
		mustContain(t, "question code", flat(doc), "Real question", "Code sample", "Still a question")
		mustNotContain(t, "question code", doc, "Outside")
	}
}

func TestGardenWorkSyncUsesCanonicalStatusProjection(t *testing.T) {
	for _, tc := range []struct {
		state    gardenapi.State
		snapshot gardensync.Snapshot
		want     string
	}{
		{gardenapi.StateConnected, gardensync.Snapshot{Source: gardensync.SourceLive, Complete: true}, "connected"},
		{gardenapi.StateReadOnly, gardensync.Snapshot{Source: gardensync.SourceLive, Complete: true}, "read-only"},
		{gardenapi.StateConnected, gardensync.Snapshot{Source: gardensync.SourceStale, BackoffHint: time.Second}, "offline"},
		{gardenapi.StateConnected, gardensync.Snapshot{Source: gardensync.SourceStale, RetryAfter: time.Minute}, "rate-limited"},
		{gardenapi.StateConnected, gardensync.Snapshot{Source: gardensync.SourceStale, AccessRevoked: true}, "access-revoked"},
	} {
		fb := gardenFake(tc.state)
		fb.WorkView.Snapshot = tc.snapshot
		m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
		if m.gardenScreen() != scrBoard {
			m = pressAll(t, m, "enter")
		}
		if m.garden.status.Sync != tc.want {
			t.Fatalf("sync=%q want %q", m.garden.status.Sync, tc.want)
		}
	}
}
