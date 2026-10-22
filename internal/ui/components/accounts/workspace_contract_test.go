package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// The only delayed command in this harness is the cosmetic Saved notice.
// Keep its reload, but leave the timer unstarted so no wall clock controls a test.
func accountDrain(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	var seen []tea.Msg
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			t.Fatal("account command loop did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		seen = append(seen, msg)
		var next tea.Cmd
		m, next = m.Update(msg)
		if saved, ok := msg.(savedMsg); ok && saved.err == nil && next != nil {
			batch := next().(tea.BatchMsg)
			queue = append(queue, batch[0])
			continue
		}
		queue = append(queue, next)
	}
	return m, seen
}

func accountKeys(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
	t.Helper()
	var seen []tea.Msg
	for _, key := range keys {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		switch key {
		case "enter":
			k.Type = tea.KeyEnter
		case "esc":
			k.Type = tea.KeyEsc
		case "tab":
			k.Type = tea.KeyTab
		case "down":
			k.Type = tea.KeyDown
		case "up":
			k.Type = tea.KeyUp
		case "left":
			k.Type = tea.KeyLeft
		case "right":
			k.Type = tea.KeyRight
		case "ctrl+s":
			k.Type = tea.KeyCtrlS
		}
		var cmd tea.Cmd
		m, cmd = m.Update(k)
		var msgs []tea.Msg
		m, msgs = accountDrain(t, m, cmd)
		seen = append(seen, msgs...)
	}
	return m, seen
}

func accountWorkspace(t *testing.T, api agentsapi.Accounts) Model {
	t.Helper()
	m := New(api, Options{Embedded: true, Now: func() time.Time { return testNow }})
	m.SetSize(120, 35)
	m, _ = accountDrain(t, m, m.Init())
	return m
}

func TestAccountsWorkspaceLongListSelectionAndViewportSurviveReload(t *testing.T) {
	api := newFake()
	api.Items = nil
	for i := 0; i < 45; i++ {
		id := agents.AccountID(fmt.Sprintf("profile-%02d", i))
		api.Items = append(api.Items, acct(string(id), []string{"claude", "codex", "antigravity"}[i%3], string(id), "logged_in"))
		api.Infos[id] = agents.AccountInfo{Identity: string(id)}
		api.Usages[id] = agentsapi.Usage{FetchedAt: testNow}
		api.LaunchDefaults[id] = agentsapi.Defaults{}
	}
	api.Now = func() time.Time { return testNow }
	m := accountWorkspace(t, api)
	m.SetASCII(true)
	m.SetSize(80, 18)
	ordered := m.Ordered()
	for i, a := range ordered {
		if m.Selected() != a.ID {
			t.Fatalf("row %d: selected %s, want %s", i, m.Selected(), a.ID)
		}
		if !strings.Contains(plain(m), "> "+a.Name) {
			t.Fatalf("selected row %s is scrolled offscreen:\n%s", a.ID, plain(m))
		}
		if i < len(ordered)-1 {
			m, _ = accountKeys(t, m, "down")
		}
	}
	want := m.Selected()
	before := plain(m)
	var cmd tea.Cmd
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if m.Selected() != want || plain(m) != before {
		t.Error("unchanged reload moved selection or the visible viewport")
	}
	slices.Reverse(api.Items)
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if m.Selected() != want || !strings.Contains(plain(m), "> "+string(want)) {
		t.Error("reordered reload lost selection or hid its row")
	}
	api.Items = slices.DeleteFunc(api.Items, func(a agentsapi.Account) bool { return a.ID == ordered[0].ID })
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if m.Selected() != want || !strings.Contains(plain(m), "> "+string(want)) {
		t.Error("removing another account displaced the selected row")
	}
	api.Items = slices.DeleteFunc(api.Items, func(a agentsapi.Account) bool { return a.ID == want })
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if _, ok := m.Current(); !ok || m.Selected() == want {
		t.Fatal("removed selection did not fall back to a listed account")
	}
	api.Items = nil
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if m.Selected() != "" || !strings.Contains(plain(m), "No accounts yet") {
		t.Error("successful empty list kept a phantom account")
	}
	// Only selection can request usage; sidebar descriptions never fetch it.
	if len(api.Fetches()) != len(ordered) {
		t.Errorf("provider fetches=%d, want %d distinct selected profiles", len(api.Fetches()), len(ordered))
	}
}

func TestAccountsWorkspaceDirtyGuardsEveryDestructiveJump(t *testing.T) {
	for _, action := range []string{"profile", "new", "show"} {
		for _, discard := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/discard=%v", action, discard), func(t *testing.T) {
				api := newFake()
				m := accountWorkspace(t, api)
				m, _ = accountKeys(t, m, "3", "enter", "down", "enter", "esc")
				changes := m.df.form.Changes()
				if !m.Dirty() {
					t.Fatal("setup did not edit defaults")
				}
				trigger := func(m Model) Model {
					if action == "show" {
						var c tea.Cmd
						m, c = m.Show("c2", TabOverview)
						m, _ = accountDrain(t, m, c)
						return m
					}
					key := "down"
					if action == "new" {
						key = "n"
					}
					m, _ = accountKeys(t, m, key)
					return m
				}
				m = trigger(m)
				if m.dlg == nil || m.Selected() != "c1" {
					t.Fatal("dirty jump must wait for confirmation")
				}
				if discard {
					m, _ = accountKeys(t, m, "right", "enter")
				} else {
					m, _ = accountKeys(t, m, "enter")
					if m.dlg != nil || !m.Dirty() || !reflect.DeepEqual(changes, m.df.form.Changes()) || m.Selected() != "c1" || m.add.active {
						t.Fatal("Keep editing changed the draft or performed the pending action")
					}
					m = trigger(m)
					m, _ = accountKeys(t, m, "esc")
					if m.dlg != nil || !m.Dirty() || m.Selected() != "c1" {
						t.Fatal("Esc did not keep the draft")
					}
					return
				}
				if m.dlg != nil || m.Dirty() {
					t.Fatal("Discard kept the old draft or dialog")
				}
				if action == "new" {
					if !m.add.active || m.Selected() != "c1" {
						t.Error("discard did not open add")
					}
				} else if m.Selected() != "c2" {
					t.Errorf("discard selected %s, want c2", m.Selected())
				}
				if len(api.Updates()) != 0 {
					t.Error("discard implicitly saved defaults")
				}
			})
		}
	}
}

func TestAccountsWorkspaceDraftSurvivesSectionsShowAndReload(t *testing.T) {
	api := newFake()
	m := accountWorkspace(t, api)
	m, _ = accountKeys(t, m, "3", "enter", "down", "enter")
	want := m.df.form.Changes()
	for _, tab := range []Tab{TabOverview, TabUsage, TabDefaults} {
		var cmd tea.Cmd
		m, cmd = m.Show("", tab)
		m, _ = accountDrain(t, m, cmd)
		if m.Selected() != "c1" || m.dlg != nil || !reflect.DeepEqual(m.df.form.Changes(), want) {
			t.Fatalf("Show(empty,%v) changed selection or draft", tab)
		}
	}
	d := api.LaunchDefaults["c1"]
	d.Values = map[string]string{"permissions": "read-only"}
	api.LaunchDefaults["c1"] = d
	var cmd tea.Cmd
	m, cmd = m.Update(ReloadMsg{})
	m, _ = accountDrain(t, m, cmd)
	if !reflect.DeepEqual(m.df.form.Changes(), want) {
		t.Fatal("background defaults reload overwrote draft")
	}
}

func TestAccountsWorkspaceDefaultsExactPatchAndSaveRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want map[string]*string
	}{
		{"clear", []string{"r"}, map[string]*string{"permissions": nil}},
		{"explicit edit", []string{"left"}, map[string]*string{"permissions": ptrString("auto")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newFake()
			d := api.LaunchDefaults["c1"]
			d.Fields = d.Fields[:1]
			d.Values = map[string]string{"permissions": "full-access", "private_option": "preserve", "model": "unlisted-model"}
			api.LaunchDefaults["c1"] = d
			m := accountWorkspace(t, api)
			m, _ = accountKeys(t, m, "3")
			m, _ = accountKeys(t, m, tc.keys...)
			if strings.Contains(plain(m), "private_option") {
				t.Error("undeclared field exposed for editing")
			}
			api.UpdateErr = errors.New("account locked")
			m, _ = accountKeys(t, m, "ctrl+s")
			if !m.Dirty() || !strings.Contains(plain(m), "Couldn't save: account locked") {
				t.Fatal("failed save lost draft or failure reason")
			}
			if got := api.Updates(); len(got) != 1 || got[0].ID != "c1" || !reflect.DeepEqual(got[0].Values, tc.want) {
				t.Fatalf("failed save patch=%+v, want %+v", got, tc.want)
			}
			api.UpdateErr = nil
			m, _ = accountKeys(t, m, "ctrl+s")
			got := api.Updates()
			if len(got) != 2 || !reflect.DeepEqual(got[1].Values, tc.want) || m.Dirty() {
				t.Fatalf("retry changed patch or stayed dirty: %+v", got)
			}
			stored := api.LaunchDefaults["c1"].Values
			if stored["private_option"] != "preserve" || stored["model"] != "unlisted-model" {
				t.Fatal("save changed undeclared stored values")
			}
			m, _ = accountKeys(t, m, "ctrl+s")
			if len(api.Updates()) != 2 {
				t.Error("clean save issued another mutation")
			}
		})
	}
}

func ptrString(s string) *string { return &s }

func TestAccountsWorkspaceCapabilityReasons(t *testing.T) {
	for _, tc := range []struct {
		name, section, want string
		setup               func(*fake.Accounts)
	}{
		{"describe failure", "1", "Unavailable · missing provider CLI", func(a *fake.Accounts) { a.DescribeErr = errors.New("missing provider CLI") }},
		{"provider warning", "1", "Provider is read only", func(a *fake.Accounts) {
			i := a.Infos["c1"]
			i.Warnings = []string{"Provider is read only"}
			a.Infos["c1"] = i
		}},
		{"defaults unavailable", "3", "Defaults unavailable", func(a *fake.Accounts) { a.DefaultsErr = errors.New("schema unavailable") }},
		{"no fields", "3", "No editable defaults", func(a *fake.Accounts) { a.LaunchDefaults["c1"] = agentsapi.Defaults{} }},
		{"no usage", "2", "Usage unavailable", func(a *fake.Accounts) { a.UsageErr = errors.New("usage is read only") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newFake()
			tc.setup(api)
			m := accountWorkspace(t, api)
			m, _ = accountKeys(t, m, tc.section)
			if !strings.Contains(plain(m), tc.want) {
				t.Errorf("missing %q:\n%s", tc.want, plain(m))
			}
			if tc.name == "no usage" && strings.Contains(plain(m), "% left") {
				t.Error("fabricated usage after failure")
			}
		})
	}
	m := New(newFake(), Options{Embedded: true})
	m.SetSize(80, 18)
	if out := plain(m); !strings.Contains(out, "Loading accounts") || strings.Contains(out, "work@example") || strings.Contains(out, "% left") {
		t.Fatalf("loading fabricated a profile or meter:\n%s", out)
	}
	// Pause after the list, before detail commands run.
	m, _ = m.Update(m.Init()())
	m.tab = TabUsage
	if !strings.Contains(plain(m), "Loading usage") || strings.Contains(plain(m), "% left") {
		t.Error("pending usage fabricated a meter")
	}
	m.tab = TabDefaults
	if !strings.Contains(plain(m), "Loading defaults") {
		t.Error("pending defaults lack loading reason")
	}
}

// Embedding the shared fake preserves its contract while recording every attempt,
// including failed removals which fake.Accounts.Removed intentionally omits.
type accountLifecycleFake struct {
	*fake.Accounts
	removes      int
	adds, logins int
	command      *exec.Cmd
}

func (a *accountLifecycleFake) Remove(ctx context.Context, id agents.AccountID) error {
	a.removes++
	return a.Accounts.Remove(ctx, id)
}
func (a *accountLifecycleFake) AddCommand(agents.ProviderID, string) *exec.Cmd {
	a.adds++
	return a.command
}
func (a *accountLifecycleFake) LoginCommand(agents.AccountID) *exec.Cmd { a.logins++; return a.command }

func TestAccountsWorkspaceLifecycleUsesOnlyExplicitCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, flow := range []string{"add", "login"} {
		t.Run(flow, func(t *testing.T) {
			// The sentinel is inspected, never executed; no external binary is needed.
			command := &exec.Cmd{Path: "unused-fake-provider", Args: []string{"unused-fake-provider", "interactive"}, Dir: home, Env: []string{"CODEX_HOME=" + home}}
			api := &accountLifecycleFake{Accounts: newFake(), command: command}
			m := accountWorkspace(t, api)
			if api.adds+api.logins != 0 {
				t.Fatal("loading automatically requested sign-in")
			}
			var msgs []tea.Msg
			if flow == "add" {
				m, msgs = accountKeys(t, m, "n", "enter", "fresh", "enter", "enter")
			} else {
				m, msgs = accountKeys(t, m, "enter", "enter")
			}
			var got *exec.Cmd
			for _, msg := range msgs {
				if e, ok := msg.(ExecMsg); ok {
					if got != nil {
						t.Fatal("duplicate ExecMsg")
					}
					got = e.Cmd
				}
			}
			if got != command {
				t.Fatal("ExecMsg did not wrap the backend command unchanged")
			}
			if api.adds+api.logins != 1 || (flow == "add" && api.adds != 1) || (flow == "login" && api.logins != 1) {
				t.Fatalf("add=%d login=%d", api.adds, api.logins)
			}
			if os.Getenv("HOME") != home || command.Dir != home || !reflect.DeepEqual(command.Env, []string{"CODEX_HOME=" + home}) {
				t.Fatal("interactive handoff changed HOME or command isolation")
			}
			if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
				t.Fatalf("handoff wrote files: %v %v", entries, err)
			}
			if flow == "add" {
				api.Items = append(api.Items, acct("fresh-id", "claude", "fresh", "logged_in"))
			}
			var cmd tea.Cmd
			m, cmd = m.Update(ReloadMsg{})
			m, _ = accountDrain(t, m, cmd)
			if flow == "add" && m.Selected() != "fresh-id" {
				t.Errorf("reload selected %s, want new account", m.Selected())
			}
			if api.adds+api.logins != 1 {
				t.Error("reload automatically requested login")
			}
		})
	}
}

func TestAccountsWorkspaceRemoveConfirmationAndBackendErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		running int
		confirm bool
		err     error
		calls   int
		want    string
	}{
		{name: "cancel", want: "work"},
		{name: "confirm", confirm: true, calls: 1, want: "personal"},
		{name: "running", running: 3, confirm: true, want: "Stop 3 running sessions first"},
		{name: "lock", confirm: true, err: errors.New("account locked"), calls: 1, want: "Couldn't remove: account locked"},
		{name: "ownership", confirm: true, err: errors.New("account ownership mismatch"), calls: 1, want: "Couldn't remove: account ownership mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &accountLifecycleFake{Accounts: newFake()}
			api.Items[0].Running = tc.running
			api.RemoveErr = tc.err
			m := accountWorkspace(t, api)
			m, _ = accountKeys(t, m, "enter", "right", "enter")
			if api.removes != 0 {
				t.Fatal("remove called before confirmation")
			}
			if tc.running == 0 && m.dlg == nil {
				t.Fatal("remove did not ask for confirmation")
			}
			if tc.running > 0 && m.dlg != nil {
				t.Fatal("running account must block removal")
			}
			if tc.running == 0 {
				if tc.confirm {
					m, _ = accountKeys(t, m, "right", "enter")
				} else {
					m, _ = accountKeys(t, m, "enter")
				}
			}
			if api.removes != tc.calls {
				t.Errorf("Remove calls=%d, want %d", api.removes, tc.calls)
			}
			if !strings.Contains(plain(m), tc.want) {
				t.Errorf("missing %q:\n%s", tc.want, plain(m))
			}
			if tc.calls == 0 || tc.err != nil {
				if m.Selected() != "c1" {
					t.Error("blocked or failed remove changed selection")
				}
			}
		})
	}
}

func TestAccountsWorkspaceUsageRemainingBoundaries(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			fraction *float64
			want     string
		}{
			{"zero", ptr(0), "0% left"}, {"half", ptr(.5), "50% left"}, {"full", ptr(1), "100% left"}, {"unknown", nil, "Unknown"},
		} {
			t.Run(fmt.Sprintf("%s/ascii=%v", tc.name, ascii), func(t *testing.T) {
				api := newFake()
				api.Usages["c1"] = agentsapi.Usage{Headline: agents.UsageHeadline{Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: tc.fraction}}, FetchedAt: testNow}
				m := accountWorkspace(t, api)
				m.SetASCII(ascii)
				m, _ = accountKeys(t, m, "2")
				out := plain(m)
				if !strings.Contains(out, tc.want) || strings.Contains(out, "% used") {
					t.Errorf("capacity lacks %q:\n%s", tc.want, out)
				}
				if tc.fraction == nil && strings.Contains(out, "% left") {
					t.Error("unknown became a numeric percentage")
				}
				if tc.fraction != nil && strings.Count(out, "% left") != 1 {
					t.Error("one reported window must have exactly one percentage")
				}
			})
		}
	}
}

func TestAccountsWorkspaceCapacitySeverityThresholds(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	for _, tc := range []struct {
		f    float64
		want lipgloss.Color
	}{{0, theme.Current.Danger}, {.249, theme.Current.Danger}, {.25, theme.Current.Warning}, {.499, theme.Current.Warning}, {.5, theme.Current.Success}, {1, theme.Current.Success}} {
		t.Run(fmt.Sprint(tc.f), func(t *testing.T) {
			if got := rankStyle(usagefmt.RankOf(tc.f)).GetForeground(); got != tc.want {
				t.Errorf("remaining=%v color=%v, want %v", tc.f, got, tc.want)
			}
			if !strings.Contains(meter(tc.f, true), lipgloss.NewStyle().Foreground(tc.want).Render(ansi.Strip(meter(tc.f, true)))) {
				t.Error("meter did not apply remaining-capacity severity")
			}
		})
	}
}

func TestAccountsWorkspaceUsageDropsLateRefreshAndKeepsLastGood(t *testing.T) {
	api := newFake()
	m := accountWorkspace(t, api)
	m, _ = accountKeys(t, m, "2")
	original := m.usage["c1"].Usage
	var oldSeq, newSeq uint64
	m.usage, oldSeq = m.usage.Begin("c1")
	m.usage, newSeq = m.usage.Begin("c1")
	newer := agentsapi.Usage{Headline: agents.UsageHeadline{Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(.91)}}, FetchedAt: testNow.Add(time.Minute)}
	var cmd tea.Cmd
	m, cmd = m.Update(usagestore.Result{Account: "c1", Seq: newSeq, Usage: newer})
	m, _ = accountDrain(t, m, cmd)
	for _, late := range []usagestore.Result{{Account: "c1", Seq: oldSeq, Usage: original}, {Account: "c1", Seq: oldSeq, Err: errors.New("late failure")}, {Account: "c1", Stored: true, Found: true, Usage: original}} {
		m, cmd = m.Update(late)
		if cmd != nil || !reflect.DeepEqual(m.usage["c1"].Usage, newer) {
			t.Fatal("late result changed data or emitted an observation")
		}
	}
	m.usage, newSeq = m.usage.Begin("c1")
	m, cmd = m.Update(usagestore.Result{Account: "c1", Seq: newSeq, Err: errors.New("provider offline")})
	m, _ = accountDrain(t, m, cmd)
	out := plain(m)
	for _, want := range []string{"91% left", "Stale", "provider offline"} {
		if !strings.Contains(out, want) {
			t.Errorf("stale data lacks %q:\n%s", want, out)
		}
	}
	if m.usage["c1"].Refreshing {
		t.Error("failed refresh stayed in flight")
	}
}

func TestAccountsWorkspaceRefreshCoalescesAndUsesCache(t *testing.T) {
	api := newFake()
	api.Now = func() time.Time { return testNow }
	m := accountWorkspace(t, api)
	m, _ = accountKeys(t, m, "2")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	for range 20 {
		var duplicate tea.Cmd
		m, duplicate = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
		if duplicate != nil {
			t.Fatal("duplicate refresh while request pending")
		}
	}
	m, _ = accountDrain(t, m, cmd)
	if calls := api.UsageCalls(); len(calls) != 2 || calls[0].ID != "c1" || !calls[1].Refresh {
		t.Fatalf("refresh calls=%+v", calls)
	}
	m, _ = accountKeys(t, m, "esc", "down", "up")
	if got := api.Fetches(); !reflect.DeepEqual(got, []agents.AccountID{"c1", "c1", "c2"}) {
		t.Errorf("cache did not bound selected-only fetches: %v", got)
	}
	for range 10 {
		_ = m.View()
	}
	if len(api.UsageCalls()) != 4 {
		t.Error("rendering started independent row refreshes")
	}
}

func TestAccountsWorkspaceUsageWindowsPoolsCreditsAndGrantsStayDistinct(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			api := newFake()
			api.Items = api.Items[:1]
			api.Items[0].Provider = agents.ProviderID(provider)
			reset := testNow.Add(3 * time.Hour)
			expires := testNow.Add(24 * time.Hour)
			primary := agents.UsageLimit{ID: "default-5h", Group: "Gemini Models", Window: "5h", RemainingFraction: ptr(.25), ResetsAt: &reset}
			secondary := agents.UsageLimit{ID: "default-week", Group: "Gemini Models", Window: "weekly", RemainingFraction: ptr(.7)}
			extra := []agents.UsageLimit{{ID: "model-pool", Group: "Reported model Z", Window: "daily", RemainingFraction: ptr(.13)}, {ID: "unavailable-window", Label: "Unavailable window", Window: "monthly"}}
			api.Usages["c1"] = agentsapi.Usage{
				FetchedAt: testNow,
				Headline:  agents.UsageHeadline{Primary: &primary, Secondary: &secondary, Extra: extra},
				Snapshot: agents.UsageSnapshot{
					Limits:       append([]agents.UsageLimit{primary, secondary}, extra...),
					Credits:      []agents.UsageCredit{{ID: "credits", Label: "Extra credits", Enabled: true, Unit: agents.CreditUnitUSD, Balance: ptr(12.34), ResetsAt: &reset}},
					ResetCredits: []agents.ResetCredit{{Title: "Grant", Status: "available", Count: 2, ExpiresAt: &expires}},
				},
			}
			m := accountWorkspace(t, api)
			m.SetSize(160, 48)
			m, _ = accountKeys(t, m, "2")
			out := plain(m)
			for _, want := range []string{"25% left", "70% left", "13% left", "Reported model Z", "Unavailable window", "Unavailable", "Extra credits", "$12.34", "2 resets available", "First expires", "Resets"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing usage fact %q:\n%s", want, out)
				}
			}
			for _, value := range []string{"25% left", "70% left", "13% left"} {
				if strings.Count(out, value) != 1 {
					t.Errorf("duplicated window %s:\n%s", value, out)
				}
			}
			for _, bad := range []string{"95% left", "108% left", "0% left"} {
				// Match complete percentages: "70%" must not be confused with "0%".
				for _, word := range strings.Fields(out) {
					if word == strings.Fields(bad)[0] {
						t.Errorf("invented or summed window percentage %s", bad)
					}
				}
			}
			if !hasRow(out, "Reported model Z", "13% left") {
				t.Error("model-specific capacity not attributed to its reported model")
			}
		})
	}
}

func TestAccountsWorkspaceAntigravityModelPoolKeepsReportedModel(t *testing.T) {
	api := newFake()
	api.Items = api.Items[:1]
	api.Items[0].Provider = "antigravity"
	api.Usages["c1"] = agentsapi.Usage{FetchedAt: testNow, Headline: agents.UsageHeadline{Extra: []agents.UsageLimit{
		{ID: "opus-weekly", Group: "Claude Opus 4", Label: "Claude Opus 4 weekly", Window: "weekly", RemainingFraction: ptr(.37)},
	}}}
	m := accountWorkspace(t, api)
	m.SetSize(160, 48)
	m, _ = accountKeys(t, m, "2")
	if out := plain(m); !hasRow(out, "Claude Opus 4", "37% left") {
		t.Errorf("model-specific pool lost its reported model:\n%s", out)
	}
}

func TestAccountsWorkspaceDefaultsDistinguishInheritedAndResolved(t *testing.T) {
	api := newFake()
	api.LaunchDefaults["c1"] = agentsapi.Defaults{
		Fields: []agents.LaunchField{{ID: "model", Label: "Model", Kind: agents.LaunchFieldModel}},
		Models: []agents.ModelOption{{ID: "resolved-model", Label: "Resolved Model", Default: true}}, Values: map[string]string{},
	}
	m := accountWorkspace(t, api)
	m, _ = accountKeys(t, m, "3")
	out := plain(m)
	if !strings.Contains(out, "Provider default") || !strings.Contains(out, "inherited") || m.Dirty() {
		t.Fatalf("inherited default was resolved into an explicit edit:\n%s", out)
	}
	m, _ = accountKeys(t, m, "enter")
	if !strings.Contains(plain(m), "Resolved Model (default)") {
		t.Error("model dropdown does not show the resolved provider default")
	}
	m, _ = accountKeys(t, m, "down", "enter", "ctrl+s")
	want := map[string]*string{"model": ptrString("resolved-model")}
	if updates := api.Updates(); len(updates) != 1 || !reflect.DeepEqual(updates[0].Values, want) {
		t.Errorf("explicit resolved model patch=%+v", updates)
	}
	if strings.Contains(plain(m), "> Model  [ Provider default") {
		t.Error("explicit resolved model still displayed as inherited")
	}
}

func TestAccountsWorkspaceDiscardAddCancelRestoresSavedDefaults(t *testing.T) {
	api := newFake()
	m := accountWorkspace(t, api)
	m, _ = accountKeys(t, m, "3", "enter", "down", "enter", "esc", "n", "right", "enter")
	if !m.add.active || m.Dirty() {
		t.Fatal("discard did not enter add with a clean draft")
	}
	m, _ = accountKeys(t, m, "esc", "3")
	if !m.df.loaded || m.Dirty() || !strings.Contains(plain(m), "Permissions") || strings.Contains(plain(m), "Loading defaults") {
		t.Error("canceling add after discarding a draft must restore editable saved defaults; the inspector stays at Loading defaults")
	}
	if len(api.Updates()) != 0 {
		t.Error("discard or cancel implicitly saved defaults")
	}
}
