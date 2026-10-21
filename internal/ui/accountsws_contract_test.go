package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

// These tests never execute terminal handoffs or timed notice commands.
func accountShellDrain(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	var seen []tea.Msg
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			t.Fatal("Accounts shell commands did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		if strings.Contains(runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(), "pgWaitCmd") {
			continue // the Playground's change watch blocks until a session changes
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		seen = append(seen, msg)
		switch msg.(type) {
		case accounts.ExecMsg, tea.QuitMsg:
			continue
		}
		var next tea.Cmd
		m, next = send(t, m, msg)
		queue = append(queue, next)
	}
	return m, seen
}
func accountShellKeys(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		key := keyOf(k)
		if len(k) > 4 && strings.HasPrefix(k, "alt+") {
			key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k[4:]), Alt: true}
		}
		var cmd tea.Cmd
		m, cmd = send(t, m, key)
		m, _ = accountShellDrain(t, m, cmd)
	}
	return m
}
func accountShellOpen(t *testing.T, api agentsapi.Accounts, sessions agentsapi.Sessions) Model {
	t.Helper()
	m := accountsModel(t, api, sessions, 120, 35)
	var cmd tea.Cmd
	m, cmd = m.selectWorkspace(wsAccounts)
	m, _ = accountShellDrain(t, m, cmd)
	return accountShellKeys(t, m, "enter")
}
func stableAccountsFake() *fake.Accounts {
	api := accountsFake()
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	api.Now = func() time.Time { return now }
	for id, u := range api.Usages {
		u.FetchedAt = now
		if u.Headline.Primary != nil {
			reset := now.Add(3 * time.Hour)
			copy := *u.Headline.Primary
			copy.ResetsAt = &reset
			u.Headline.Primary = &copy
		}
		api.Usages[id] = u
	}
	return api
}

type accountShellBackend struct {
	*fake.Accounts
	lists int
}

func (a *accountShellBackend) List(ctx context.Context) ([]agentsapi.Account, error) {
	a.lists++
	return a.Accounts.List(ctx)
}

type accountSessionSpy struct {
	*fake.Sessions
	stops int
}

func (s *accountSessionSpy) Stop(ctx context.Context, id string) error {
	s.stops++
	return s.Sessions.Stop(ctx, id)
}

func TestAccountsShellStartupLoadsOnceAndRemembersWorkspace(t *testing.T) {
	for _, memoryOff := range []bool{false, true} {
		t.Run(fmt.Sprintf("memoryOff=%v", memoryOff), func(t *testing.T) {
			e := newSettingsEnv(t)
			e.writeState(t, fmt.Sprintf(`{"prefs":{"start_workspace":"accounts","no_workspace_memory":%v}}`, memoryOff))
			st, err := config.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			api := &accountShellBackend{Accounts: stableAccountsFake()}
			m := New(e.repo, &config.Config{}, st, WithLayout(LayoutRedesign), WithAgents(agentsapi.Backend{Accounts: api}))
			m = sized(t, m, 120, 35)
			if m.workspace != wsAccounts || !m.acct.started || api.lists != 0 {
				t.Fatal("startup should schedule Accounts without synchronously reading backend")
			}
			m, _ = accountShellDrain(t, m, m.Init())
			if api.lists != 1 || len(api.UsageCalls()) != 1 {
				t.Fatalf("startup list=%d usage=%v", api.lists, api.UsageCalls())
			}
			m = accountShellKeys(t, m, "tab", "tab", "tab", "tab", "tab")
			if m.workspace != wsAccounts || api.lists != 1 {
				t.Fatal("workspace cycling loaded Accounts again or lost tab order")
			}
			disk, err := config.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			want := "accounts"
			if memoryOff {
				want = ""
			}
			if disk.LastWorkspace(e.repo) != want {
				t.Errorf("last workspace=%q, want %q", disk.LastWorkspace(e.repo), want)
			}
		})
	}
}

func TestAccountsShellInputCaptureOwnsGlobalKeys(t *testing.T) {
	for _, state := range []string{"add provider", "add name", "add continue", "dropdown", "dialog"} {
		t.Run(state, func(t *testing.T) {
			for _, key := range []string{"q", "?", "m", ",", "alt+1"} {
				t.Run(key, func(t *testing.T) {
					m := accountShellOpen(t, stableAccountsFake(), nil)
					switch state {
					case "add provider":
						m = accountShellKeys(t, m, "n")
					case "add name":
						m = accountShellKeys(t, m, "n", "enter")
					case "add continue":
						m = accountShellKeys(t, m, "n", "enter", "fresh", "enter")
					case "dropdown":
						m = accountShellKeys(t, m, "3", "enter")
					case "dialog":
						m = accountShellKeys(t, m, "enter", "right", "enter")
					}
					if got := m.currentShellFocus(); got != shellInput {
						t.Errorf("active %s focus=%s, want Input", state, got)
					}
					var cmd tea.Cmd
					k := runeKey(key)
					if key == "alt+1" {
						k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}
					}
					before := m
					m, cmd = send(t, m, k)
					m, msgs := accountShellDrain(t, m, cmd)
					for _, msg := range msgs {
						if _, ok := msg.(tea.QuitMsg); ok {
							t.Errorf("%s quit while %s active", key, state)
						}
					}
					if m.workspace != wsAccounts || m.modal != nil {
						t.Errorf("%s escaped %s: workspace=%s modal=%v", key, state, m.workspace.name(), m.modal != nil)
					}
					_, cmd = send(t, before, tea.KeyMsg{Type: tea.KeyCtrlC})
					if cmd == nil {
						t.Fatal("ctrl+c must quit")
					}
					if _, ok := cmd().(tea.QuitMsg); !ok {
						t.Error("ctrl+c did not emit QuitMsg")
					}
				})
			}
		})
	}
}

func TestAccountsShellNestedFocusAndCloseOnlyReturnToTabs(t *testing.T) {
	for _, open := range []string{"enter", "down"} {
		t.Run(open, func(t *testing.T) {
			m := accountShellOpen(t, stableAccountsFake(), nil)
			m = accountShellKeys(t, m, "esc", open)
			if m.shellFocus != shellBody || !m.acct.model.InProfiles() {
				t.Fatal("tab did not open Profiles")
			}
			m = accountShellKeys(t, m, "tab")
			if !m.acct.model.InProfiles() || m.acct.model.Section() != "Usage" {
				t.Fatal("Tab must cycle section while profile list keeps focus")
			}
			m = accountShellKeys(t, m, "enter", "esc")
			if !m.acct.model.InProfiles() || m.shellFocus != shellBody {
				t.Fatal("inspector Escape must return to Profiles")
			}
			selected := m.acct.model.Selected()
			before := m.acct.model.Section()
			m, cmd := send(t, m, accounts.CloseMsg{})
			m, _ = accountShellDrain(t, m, cmd)
			if m.workspace != wsAccounts || m.shellFocus != shellNav || m.acct.model.Selected() != selected || m.acct.model.Section() != before {
				t.Fatal("CloseMsg changed workspace, section or selection")
			}
			m, cmd = m.selectWorkspace(wsSettings)
			m, _ = accountShellDrain(t, m, cmd)
			m.shellFocus = shellBody
			m, _ = send(t, m, accounts.CloseMsg{})
			if m.workspace != wsSettings || m.shellFocus != shellBody {
				t.Fatal("late Accounts CloseMsg stole Settings focus")
			}
		})
	}
}

func TestAccountsShellNavigationKeepsDraftAndLiveHolders(t *testing.T) {
	sessions := &accountSessionSpy{Sessions: &fake.Sessions{Supports: true}}
	var live agentsapi.Session
	live.ID = "live-holder"
	live.State = "running"
	sessions.Add(live)
	stream, err := sessions.Attach(context.Background(), live.ID, agentsapi.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	api := stableAccountsFake()
	m := accountShellOpen(t, api, sessions)
	if m.agents.cancel != nil {
		t.Cleanup(m.agents.cancel)
	}
	m = accountShellKeys(t, m, "down", "3", "enter", "down", "enter", "esc", "esc")
	if !m.acct.model.Dirty() {
		t.Fatal("setup has no draft")
	}
	want := m.acct.model.Selected()
	for _, keys := range [][]string{{"tab", "tab", "tab", "tab", "tab"}, {"alt+1", "alt+2", "alt+3", "alt+5", "alt+4"}} {
		m = accountShellKeys(t, m, keys...)
		if m.workspace != wsAccounts || m.acct.model.Selected() != want || !m.acct.model.Dirty() || m.acct.model.Section() != "Defaults" {
			t.Fatal("workspace round trip dropped selection, section or draft")
		}
	}
	m = accountShellKeys(t, m, "enter", "down")
	if m.currentShellFocus() != shellInput {
		t.Fatal("profile change should guard dirty draft")
	}
	m = accountShellKeys(t, m, "right", "enter", "esc")
	if m.acct.model.Selected() == want {
		t.Error("discard did not switch profile")
	}
	got, err := sessions.List(context.Background())
	if err != nil || !reflect.DeepEqual(got, []agentsapi.Session{live}) {
		t.Errorf("live holder changed: %+v %v", got, err)
	}
	if sessions.stops != 0 || stream.(*fake.Stream).Closed() || !stream.Writer() {
		t.Errorf("navigation stopped or detached live holder: stops=%d", sessions.stops)
	}
	disk, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if disk.LastWorkspace(m.repoDir) != "accounts" {
		t.Error("Accounts not persisted as last workspace")
	}
}

func TestAccountsShellScopeIgnoresSelectedWorktree(t *testing.T) {
	api := &accountShellBackend{Accounts: stableAccountsFake()}
	m := accountShellOpen(t, api, nil)
	m = accountShellKeys(t, m, "down", "2")
	selected := m.acct.model.Selected()
	usage := len(api.UsageCalls())
	for _, path := range []string{t.TempDir(), t.TempDir()} {
		m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: path, Branch: "feature"}}})
		m = accountShellKeys(t, m, "esc", "esc", "alt+3", "alt+4", "enter")
		if m.acct.model.Selected() != selected || m.acct.model.Section() != "Usage" || api.lists != 1 || len(api.UsageCalls()) != usage {
			t.Error("worktree selection changed global account scope or fetched again")
		}
	}
}

func TestAccountsShellPaletteAndSettingsKeepExistingSelection(t *testing.T) {
	for _, tc := range []struct{ label, section string }{{"Go to Accounts", "Usage"}, {"Accounts: Usage", "Usage"}, {"Accounts: Defaults", "Defaults"}, {"settings link", "Defaults"}, {"m", "Overview"}} {
		t.Run(tc.label, func(t *testing.T) {
			m := accountShellOpen(t, stableAccountsFake(), nil)
			m = accountShellKeys(t, m, "down", "2", "esc", "esc", "alt+1")
			id := m.acct.model.Selected()
			var cmd tea.Cmd
			switch tc.label {
			case "settings link":
				m = openSettings(t, m, catAgents, 0, 0)
				m, cmd = send(t, m, keyOf("enter"))
			case "m":
				m, cmd = send(t, m, runeKey("m"))
			default:
				if _, ok := paletteTargetForLabel(tc.label); !ok {
					t.Fatal("missing palette target")
				}
				m = m.openShellPalette()
				m, cmd = send(t, m, modals.SubmitMsg{Kind: modals.KindPalette, Value: tc.label})
			}
			m, _ = accountShellDrain(t, m, cmd)
			if m.workspace != wsAccounts || m.acct.model.Selected() != id || m.acct.model.Section() != tc.section || m.modal != nil {
				t.Errorf("navigation lost target or selection: ws=%s id=%s section=%s", m.workspace.name(), m.acct.model.Selected(), m.acct.model.Section())
			}
		})
	}
}

func TestAccountsShellExecCompletionReloadsAndSurfacesFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			api := &accountShellBackend{Accounts: stableAccountsFake()}
			m := accountShellOpen(t, api, nil)
			api.Items = append(api.Items, wsAccount("new", "codex", "newly-added", "logged_in"))
			var err error
			if failure {
				err = errors.New("provider sign-in failed")
			}
			m, cmd := send(t, m, agentExecDoneMsg{err: err})
			m, _ = accountShellDrain(t, m, cmd)
			if api.lists != 2 || len(m.acct.model.Accounts()) != 4 {
				t.Fatal("terminal completion did not reload accounts exactly once")
			}
			if failure && !strings.Contains(view(m), "accounts: provider sign-in failed") {
				t.Error("terminal failure did not surface in status")
			}
		})
	}
}

func TestAccountsShellWithAgentsAvailabilityAndLegacy(t *testing.T) {
	for _, layout := range []Layout{LayoutLegacy, LayoutRedesign} {
		for _, support := range []string{"nil", "unsupported", "supported"} {
			t.Run(fmt.Sprintf("%s/%s", layout, support), func(t *testing.T) {
				base := shellModel(t, config.Prefs{}, nil)
				var sessions agentsapi.Sessions
				if support != "nil" {
					sessions = &fake.Sessions{Supports: support == "supported"}
				}
				api := stableAccountsFake()
				m := New(base.repoDir, base.cfg, base.state, WithLayout(layout), WithAgents(agentsapi.Backend{Accounts: api, Sessions: sessions}))
				if m.agents.cancel != nil {
					t.Cleanup(m.agents.cancel)
				}
				if m.agents.api.Accounts != api || m.agents.active() != (support == "supported") {
					t.Error("account availability coupled to terminal support")
				}
				if layout == LayoutLegacy {
					if m.acct.present || m.acct.started {
						t.Error("legacy unexpectedly initialized workspace")
					}
					if m.Init() == nil {
						t.Error("legacy lost startup behavior")
					}
				} else {
					m = sized(t, m, 120, 35)
					var cmd tea.Cmd
					m, cmd = m.selectWorkspace(wsAccounts)
					m, _ = accountShellDrain(t, m, cmd)
					if !m.acct.present || !strings.Contains(view(m), "personal") {
						t.Error("Accounts hidden without GitHub or terminal capability")
					}
				}
			})
		}
	}
}

func TestAccountsShellViewportAndCompleteFooterHints(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {63, 9}, {20, 3}, {1, 1}, {0, 0}} {
			for _, state := range []string{"overview", "usage", "defaults", "dropdown", "dialog", "add", "empty", "no backend"} {
				t.Run(fmt.Sprintf("ascii=%v/%dx%d/%s", ascii, size[0], size[1], state), func(t *testing.T) {
					api := stableAccountsFake()
					var backend agentsapi.Accounts = api
					if state == "empty" {
						backend = &fake.Accounts{}
					}
					if state == "no backend" {
						backend = nil
					}
					m := accountsModel(t, backend, nil, 120, 35)
					if ascii {
						m.work.AgentGlyphs = "ascii"
						m.acct.model.SetASCII(true)
					}
					var cmd tea.Cmd
					m, cmd = m.selectWorkspace(wsAccounts)
					m, _ = accountShellDrain(t, m, cmd)
					m = accountShellKeys(t, m, "enter")
					switch state {
					case "usage":
						m = accountShellKeys(t, m, "2")
					case "defaults":
						m = accountShellKeys(t, m, "3")
					case "dropdown":
						m = accountShellKeys(t, m, "3", "enter")
					case "dialog":
						m = accountShellKeys(t, m, "enter", "right", "enter")
					case "add":
						m = accountShellKeys(t, m, "n", "enter")
					}
					m = sized(t, m, size[0], size[1])
					out := m.View()
					lines := strings.Split(out, "\n")
					if size[0] >= shellMinWidth && size[1] >= shellMinHeight {
						if len(lines) != size[1] {
							t.Errorf("height=%d want %d", len(lines), size[1])
						}
						for i, l := range lines {
							if w := ansi.StringWidth(l); w != size[0] {
								t.Errorf("line %d width=%d want %d", i, w, size[0])
							}
						}
						if state == "overview" || state == "usage" || state == "defaults" {
							if !strings.Contains(ansi.Strip(out), "work") {
								t.Error("selected profile not visible")
							}
						}
						footer := strings.TrimSpace(ansi.Strip(lines[len(lines)-2]))
						hints := m.shellHints()
						valid := false
						for n := len(hints); n >= 0; n-- {
							var parts []string
							for _, hint := range hints[:n] {
								parts = append(parts, m.chrome(hint.key)+" "+hint.desc)
							}
							candidate := strings.Join(parts, "  ")
							candidate = strings.TrimSpace(ansi.Strip(strings.Join(m.settingsASCII([]string{candidate}), "")))
							if footer == candidate {
								valid = true
								break
							}
						}
						if !valid || footer == "" {
							t.Errorf("footer cuts a key or hint: %q", footer)
						}
					} else if size[0] > 0 && size[1] > 0 {
						assertBounded(t, "tiny Accounts", out, size[0], size[1])
					}
					if ascii {
						for _, r := range ansi.Strip(out) {
							if r > 127 {
								t.Errorf("ASCII mode rendered %q", r)
								break
							}
						}
					}
				})
			}
		}
	}
}

func TestAccountsShellASCIIChangeReformatsLoadedDefaults(t *testing.T) {
	m := accountShellOpen(t, stableAccountsFake(), nil)
	m = accountShellKeys(t, m, "3")
	// Settings can change glyphs while the workspace and its draft stay alive.
	m.work.AgentGlyphs = "ascii"
	out := view(m)
	for _, r := range out {
		if r > 127 {
			t.Errorf("loaded Defaults kept non-ASCII glyph %q after switching to ASCII mode", r)
			break
		}
	}
}
