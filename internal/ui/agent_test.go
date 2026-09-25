package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/gym"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

func TestFormatElapsed(t *testing.T) {
	if got := formatElapsed(34 * time.Second); got != "00:34" {
		t.Errorf("formatElapsed(34s) = %q, want 00:34", got)
	}
	if got := formatElapsed(3*time.Minute + 24*time.Second); got != "03:24" {
		t.Errorf("formatElapsed(3m24s) = %q, want 03:24", got)
	}
	if got := formatElapsed(1*time.Hour + 2*time.Minute + 3*time.Second); got != "01:02:03" {
		t.Errorf("formatElapsed(1h2m3s) = %q, want 01:02:03", got)
	}
}

func TestFormatQuotaLine(t *testing.T) {
	if got := formatQuotaLine(nil); got != "unknown" {
		t.Errorf("formatQuotaLine(nil) = %q, want unknown", got)
	}
	rem5h := 0.64
	remWeekly := 0.81
	usage := &agym.Usage{
		Windows: []agym.UsageWindow{
			{Name: "5h", Remaining: &rem5h},
			{Name: "weekly", Remaining: &remWeekly},
		},
		ObservedAt: time.Now().Add(-2 * time.Minute),
	}
	got := formatQuotaLine(usage)
	if !strings.Contains(got, "5h 64%") || !strings.Contains(got, "weekly 81%") {
		t.Errorf("unexpected quota line: %q", got)
	}
}

func TestRenderAgentHeader(t *testing.T) {
	view := &gym.AgentView{
		Binding: gym.RunBinding{
			RunID: "run-test-1",
		},
		Run: &agym.Run{
			RunID:           "run-test-1",
			SelectedProfile: "personal",
			Status:          agym.RunStateRunning,
			Task:            "Fix the parser",
		},
	}
	header := renderAgentHeader(view, 80)
	if !strings.Contains(header, "personal") || !strings.Contains(header, "running") || !strings.Contains(header, "Fix the parser") {
		t.Errorf("unexpected header rendering: %s", header)
	}
}

func TestAgentAutoStartModalAndMessage(t *testing.T) {
	m := Model{
		width:   80,
		height:  24,
		cfg:     &config.Config{},
		repoDir: t.TempDir(),
	}

	nm, _ := m.openAgentAutoStartModal()
	mod := nm.(Model)
	if mod.modal == nil || mod.modal.Kind() != modals.KindAgentAutoTask {
		t.Fatalf("expected KindAgentAutoTask modal, got %v", mod.modal)
	}

	// Submit modal with a task
	nm, cmd := mod.Update(modals.SubmitMsg{Kind: modals.KindAgentAutoTask, Value: "Fix memory leak"})
	mod = nm.(Model)
	if cmd == nil {
		t.Fatalf("expected non-nil tea.Cmd on submitting agent task")
	}
	if mod.status != "generating branch & starting agent…" {
		t.Errorf("status = %q, want generating branch & starting agent…", mod.status)
	}

	// Receive gymAutoRunMsg
	nm, _ = mod.Update(gymAutoRunMsg{
		result: &gym.AutoRunResult{
			Branch:       "fix/memory-leak",
			WorktreePath: "/tmp/wt-fix-memory-leak",
		},
	})
	mod = nm.(Model)
	if mod.rightTab != tabAgent {
		t.Errorf("rightTab = %v, want tabAgent", mod.rightTab)
	}
	if !strings.Contains(mod.status, "fix/memory-leak") {
		t.Errorf("status = %q, want fix/memory-leak", mod.status)
	}
}

