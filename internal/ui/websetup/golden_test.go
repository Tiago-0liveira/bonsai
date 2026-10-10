package websetup

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// TestGolden snapshots every screen at 80×24 and 120×40 with colors off, so a
// change in layout or copy shows up in review as a text diff. Regenerate
// with: go test ./internal/ui/websetup -run Golden -update
func TestGolden(t *testing.T) {
	type scene struct {
		name  string
		build func(t *testing.T, w, h int) *harness
	}
	wizard := func(f *fakeBackend, keys ...string) func(*testing.T, int, int) *harness {
		return func(t *testing.T, w, h int) *harness {
			hs := newHarness(t, f, Wizard, firstRunDraft(), testInfo())
			hs.resize(w, h)
			hs.keys(keys...)
			return hs
		}
	}
	running := testInfo()
	running.Running = &setup.Running{APIPort: 7001, Hosted: true}
	edit := func(f *fakeBackend, info Info, keys ...string) func(*testing.T, int, int) *harness {
		return func(t *testing.T, w, h int) *harness {
			hs := newHarness(t, f, Edit, editDraft(), info)
			hs.resize(w, h)
			hs.keys(keys...)
			return hs
		}
	}
	problems := func() *fakeBackend {
		f := newFake()
		f.checks = loggedOut()
		f.checks[4] = checks.Check{ID: checks.IDPort, Title: "port 7001", State: checks.Fail, Detail: "port 7001 is used by another program (pid 4242, node)", Fix: &checks.Fix{Command: "bonsai web --port 7011   or pick another port in: bonsai web setup → Advanced"}}
		return f
	}
	failing := newFake()
	failing.failApply = true
	liveRunning := testInfo()
	liveRunning.Running = &setup.Running{APIPort: 7001, Hosted: true, WebhookPort: 7002}
	liveRunning.InstallID = "3f9c2a71d04b8e65"
	liveEdit := func(f *fakeBackend, keys ...string) func(*testing.T, int, int) *harness {
		return func(t *testing.T, w, h int) *harness {
			hs := newHarness(t, f, Edit, liveEditDraft("octo/bonsai", "octo/api"), liveRunning)
			hs.resize(w, h)
			hs.keys(keys...)
			return hs
		}
	}
	liveProblems := func() *fakeBackend {
		f := newFake()
		f.checks[5] = checks.Check{ID: checks.IDTunnelCloudflared, Title: "cloudflared", State: checks.OK, Detail: "installed (2026.9.3) · not logged in (only a custom domain needs it)", Fix: &checks.Fix{Command: "cloudflared tunnel login", Inline: true}}
		f.checks[6].Detail = "not installed · not used by your tunnel"
		f.checks[7].Detail = "not installed · not used by your tunnel"
		f.checks = append(f.checks,
			checks.Check{ID: checks.IDUpdatesLive, Title: "live updates", State: checks.Warn, Detail: "1 of 2 repos live · octo/api cannot be managed with your gh login", Fix: &checks.Fix{Command: "gh auth refresh -h github.com -s admin:repo_hook", Inline: true}},
			checks.Check{ID: checks.IDLiveHooks, Title: "Bonsai webhooks", State: checks.Warn, Detail: "acme/old still has a Bonsai webhook that live updates no longer use", Fix: &checks.Fix{Command: "gh api -X DELETE repos/acme/old/hooks/41"}},
		)
		return f
	}
	wizardTo := func(keys ...string) []string {
		return append([]string{"enter", "enter", "enter", "enter"}, keys...)
	}

	scenes := []scene{
		{"01-welcome", wizard(newFake())},
		{"02-projects", wizard(newFake(), "enter")},
		{"02-projects-add-error", func(t *testing.T, w, h int) *harness {
			hs := wizard(newFake(), "enter", "a")(t, w, h)
			hs.typeText("~/nope")
			hs.keys("enter")
			return hs
		}},
		{"03-open-in", wizard(newFake(), "enter", "enter")},
		{"03-open-in-hosted", wizard(newFake(), "enter", "enter", "down")},
		{"04-github", wizard(newFake(), "enter", "enter", "enter")},
		{"04-github-logged-out", wizard(problems(), "enter", "enter", "enter")},
		{"05-updates", wizard(newFake(), "enter", "enter", "enter", "enter")},
		{"05-updates-cloudflare-quick", wizard(newFake(), "enter", "enter", "enter", "enter", "down")},
		{"05-updates-compare", wizard(newFake(), "enter", "enter", "enter", "enter", "?")},
		{"06-review", wizard(newFake(), "d")},
		{"07-apply-failed", wizard(failing, "d", "enter")},
		{"08-done", wizard(newFake(), "d", "enter")},
		{"09-confirm-discard", wizard(newFake(), "enter", "space", "ctrl+c")},
		{"10-dashboard", edit(newFake(), running)},
		{"10-dashboard-not-running", edit(problems(), testInfo())},
		{"11-dashboard-edited", edit(newFake(), running, "enter", "down", "space", "enter")},
		{"12-review-edit", edit(newFake(), running, "enter", "down", "space", "enter", "down", "enter", "up", "enter", "a")},
		{"13-done-edit", edit(newFake(), running, "enter", "down", "space", "enter", "a", "enter")},
		{"14-advanced", edit(newFake(), running, "down", "down", "down", "down", "enter")},
		{"15-doctor", edit(problems(), running, "d")},
		{"16-updates-named", wizard(newFake(), wizardTo("down", "down")...)},
		{"17-tunnel-named", func(t *testing.T, w, h int) *harness {
			hs := wizard(newFake(), wizardTo("down", "down", "enter")...)(t, w, h)
			hs.typeText("bonsai")
			hs.keys("enter")
			hs.typeText("http://hooks.example.com")
			hs.keys("enter")
			return hs
		}},
		{"18-tunnel-custom", wizard(newFake(), wizardTo("down", "down", "down", "down", "down", "down", "enter")...)},
		{"19-live-repos", wizard(newFake(), wizardTo("down", "enter", "space", "down", "down")...)},
		{"20-review-live", wizard(newFake(), wizardTo("down", "enter", "space", "enter")...)},
		{"21-apply-live", func(t *testing.T, w, h int) *harness {
			hs := wizard(newFake(), wizardTo("down", "enter", "space", "enter")...)(t, w, h)
			// Mid-apply: the fake backend finishes at once, so set the steps
			// the real one reports while it waits for GitHub.
			m := hs.model().push(screenApply)
			m.applying = true
			m.steps = []Step{
				{ID: "save", Label: "Saved settings", State: StepDone, Detail: "~/.config/bonsai/web.json"},
				{ID: "start", Label: "Started Bonsai", State: StepDone, Detail: "http://127.0.0.1:7001"},
				{ID: "receiver", Label: "Change receiver ready", State: StepDone, Detail: "127.0.0.1:7002"},
				{ID: "tunnel", Label: "Tunnel address", State: StepDone, Detail: "https://quiet-river.trycloudflare.com"},
				{ID: "hooks", Label: "GitHub webhooks", State: StepDone, Detail: "octo/bonsai added"},
				{ID: "ping", Label: "Waiting for GitHub's ping", State: StepRunning, Detail: "octo/bonsai"},
			}
			hs.m = m
			return hs
		}},
		{"21-done-live", wizard(newFake(), wizardTo("down", "enter", "space", "enter", "enter")...)},
		{"22-dashboard-live", liveEdit(liveProblems())},
		{"23-review-live-off", liveEdit(newFake(), "down", "down", "down", "enter", "up", "enter", "a")},
		{"24-advanced-live", liveEdit(newFake(), "down", "down", "down", "down", "enter", "down", "down", "space")},
		{"25-review-rotate", liveEdit(newFake(), "down", "down", "down", "down", "enter", "down", "down", "space", "enter", "a")},
		{"26-doctor-live", liveEdit(liveProblems(), "d")},
	}
	for _, sc := range scenes {
		for _, size := range [][2]int{{80, 24}, {120, 40}} {
			name := sc.name + "-" + itoa(size[0]) + "x" + itoa(size[1])
			t.Run(name, func(t *testing.T) {
				hs := sc.build(t, size[0], size[1])
				got := hs.model().View()
				assertFits(t, got, size[0], size[1])
				compareGolden(t, name, got)
			})
		}
	}
}

func (h *harness) resize(w, ht int) {
	h.update(tea.WindowSizeMsg{Width: w, Height: ht})
}

func assertFits(t *testing.T, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("view has %d lines, want exactly %d", len(lines), h)
	}
	for i, line := range lines {
		if n := lipgloss.Width(line); n > w {
			t.Errorf("line %d is %d columns wide (max %d): %q", i+1, n, w, line)
		}
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	// Goldens are written on Unix; Windows paths render with backslashes.
	if runtime.GOOS == "windows" {
		got = strings.ReplaceAll(got, `\`, "/")
	}
	// Trailing spaces are layout noise; keep the files diff-friendly.
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	// A checkout with core.autocrlf rewrites the goldens with CRLF.
	if string(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) != got {
		t.Errorf("%s differs from the golden file (run with -update to accept):\n--- got\n%s--- want\n%s", name, got, want)
	}
}
