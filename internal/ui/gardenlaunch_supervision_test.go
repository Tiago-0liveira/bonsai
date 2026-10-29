package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func launchTestReview(escalated bool) gardenapi.LaunchReview {
	mode := "default"
	if escalated {
		mode = "bypassPermissions"
	}
	policy := reviewpass.Policy{Passes: 1, Reviewer: reviewpass.Reviewer{Provider: "claude", AccountID: "review", AccountName: "review", Model: "opus", Effort: "high", Escalated: escalated, Native: reviewpass.Native{PermissionMode: mode}}, Checks: []reviewpass.Check{reviewpass.CheckFrom(config.GardenCheck{Name: "unit tests", Executable: "go", Args: []string{"test", "./..."}})}}
	hash := policy.Hash()
	return gardenapi.LaunchReview{Enabled: true, Available: true, Hash: hash, Policy: policy}
}

func TestGardenLaunchReadyCachedRunAndNavigationNeverPreview(t *testing.T) {
	for _, variant := range []string{"off", "uncached", "enabled", "invalid", "preserved"} {
		t.Run(variant, func(t *testing.T) {
			m, rig, fb := fakeLaunchUIEnv(t, 120, 35)
			acc := m.agents.api.Accounts.(*fakeLaunchUIAccounts)
			acc.resolutions = map[agents.AccountID]agents.LaunchResolution{"c1": fb.resolution}
			if variant != "off" {
				m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: true, Account: "review"}
			}
			if variant == "enabled" {
				res := fb.resolution
				res.AccountID, res.AccountName = "review", "review"
				acc.resolutions["review"] = res
				rig.agents.Accounts.Items = append(rig.agents.Accounts.Items, agentsapi.Account{Account: agents.Account{ID: "review", Name: "review", Provider: "claude"}})
			}
			if variant == "invalid" {
				m.work.GardenAutoReview.Version = config.GardenAutoReviewVersion + 1
			}
			if variant == "preserved" {
				e := newSettingsEnv(t)
				e.writeState(t, `{"prefs":{"garden_auto_review":{"version":99,"enabled":true,"account":"review"}}}`)
				m.state = e.model(t, 120, 35).state
			}
			m = plannerPress(t, m, "enter")
			out := view(m)
			callsText := strings.Join(acc.calls, ",")
			if strings.Contains(callsText, "ResolveLaunch") {
				t.Fatalf("Ready resolved launch options: %v", acc.calls)
			}
			mustContain(t, variant, out, "READY TO BUILD", "Specification s1 and plan v1 are approved.", "Claude / work", "high", "Worktree", "bonsai/101-", "Permissions", "Permission mode: default")
			switch variant {
			case "off", "invalid", "preserved":
				mustContain(t, variant, out, "Off: stops at your review")
			case "uncached":
				mustContain(t, variant, out, "unknown / review", "Reviewer availability is checked in the preview")
			case "enabled":
				mustContain(t, variant, out, "Claude / review", "Review limit", "One review + fixes + checks", "Reviewer availability is checked in the preview", "model opus")
			}
			calls := len(acc.calls)
			for range 3 {
				_ = m.View()
			}
			if len(acc.calls) != calls {
				t.Fatal("rendering read accounts")
			}
			m = plannerPress(t, m, "right", "left", "r", "esc", "i", "esc")
			if len(fb.previews) != 0 || len(fb.starts) != 0 {
				t.Fatalf("navigation previewed or started: %v", fb.calls)
			}
		})
	}
}

func TestGardenLaunchReadyStartDirectPreviewAndSettingsForm(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = plannerPress(t, m, "enter", "s")
	if m.garden.launch.stage != launchStageConfirm || m.garden.launch.dlg != nil || len(fb.previews) != 1 || len(fb.starts) != 0 {
		t.Fatalf("direct preview: stage=%v calls=%v", m.garden.launch.stage, fb.calls)
	}
	if fb.previews[0].AccountID != "c1" || fb.previews[0].Launch.Model != "" {
		t.Fatalf("default preview changed options: %+v", fb.previews[0])
	}
	m = plannerPress(t, m, "esc", "e")
	if m.garden.launch.stage != launchStageChoose || m.garden.launch.dlg == nil || len(fb.previews) != 1 {
		t.Fatalf("settings did not open form: %v", fb.calls)
	}
}

func TestGardenLaunchReviewerConsentSeparateAndReset(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	fb.review = launchTestReview(true)
	fb.resolution.Launch.PermissionMode = "bypassPermissions"
	m = fakeLaunchUIReview(t, m)
	mustContain(t, "sequence", view(m), "Sequence", "Confirm authorizes:", "unit tests", "stop for your review", "[ ] r:", "[ ] x:")
	m = plannerPress(t, m, "x", "tab")
	if m.launchConfirmable() || m.garden.launch.btn != 0 {
		t.Fatal("implementer acknowledgment also authorized reviewer")
	}
	m = plannerPress(t, m, "r")
	if !m.launchConfirmable() {
		t.Fatal("separate reviewer acknowledgment did not enable confirmation")
	}
	// A new exact preview invalidates both consents before its result arrives.
	m, cmd := m.launchPreview()
	if m.garden.launch.escalate || m.garden.launch.reviewEscalate {
		t.Fatal("acknowledgments survived preview request")
	}
	m = plannerRun(t, m, cmd)
	if m.launchConfirmable() {
		t.Fatal("new preview reused acknowledgments")
	}
	m = plannerPress(t, m, "r", "tab")
	if m.launchConfirmable() {
		t.Fatal("reviewer acknowledgment authorized implementer")
	}
	m = plannerPress(t, m, "x", "tab", "enter")
	if len(fb.starts) != 1 || !fb.starts[0].ReviewEscalationConfirmed || !fb.starts[0].EscalationConfirmed {
		t.Fatalf("consent not sent: %+v", fb.starts)
	}
}

func TestGardenLaunchPolicyChangedRequiresExplicitFreshPreview(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	fb.startErr = &gardenapi.Error{Code: gardenapi.CodeLaunchChanged, Message: "unsafe backend prose"}
	m = plannerPress(t, m, "tab", "enter")
	mustContain(t, "changed policy", view(m), "The review policy changed since this preview. Preview again.")
	if len(fb.attempts) != 0 || len(fb.previews) != 1 || m.launchConfirmable() {
		t.Fatal("policy refusal started or auto-previewed")
	}
	m = plannerPress(t, m, "r")
	if len(fb.previews) != 2 || len(fb.starts) != 1 || m.garden.launch.btn != 0 {
		t.Fatal("fresh preview auto-started")
	}
}

func TestGardenLaunchSupervisionUsesFrozenEvidence(t *testing.T) {
	for _, size := range [][2]int{{120, 35}, {80, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _, fb, a := fakeLaunchUIHistory(t, size[0], size[1], false)
			now := time.Now()
			started := now.Add(-402 * time.Second)
			a.Runs[0].StartedAt = &started
			a.Revision++
			fb.attempts[0] = a
			check := launchTestReview(false).Policy.Checks[0]
			fb.reviews = map[string]reviewpass.Pass{a.ID: {Revision: 1, Snapshot: reviewpass.Snapshot{AttemptID: a.ID, Policy: launchTestReview(false).Policy}, Checks: []reviewpass.CheckResult{{CheckID: check.ID, Outcome: reviewpass.OutcomePassed}}}}
			fb.activity = map[string]gardenapi.LaunchActivity{a.ID: {State: "running", Activity: "idle", Alive: true, ObservedAt: now.Add(-4 * time.Second)}}
			m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: true, Account: "someone-else"}
			m = plannerPress(t, m, "enter")
			mustContain(t, "supervision", view(m), "IMPLEMENTING THE APPROVED PLAN", "Idle", "idle is not completion", "observed 4s ago", "06:42 elapsed", "GitHub (", "Next: one Claude review", "Open session", "Stop run")
			mustFit(t, "supervision", m)
			text := m.launchSupervision(a, now)
			mustContain(t, "checks evidence", text, "Check: unit tests", "passed", "stage update to In progress: pending")
			a.Runs[0].StartedAt = nil
			mustNotContain(t, "no invented elapsed", m.launchSupervision(a, now), "elapsed")
			delete(m.garden.launch.activity, a.ID)
			mustContain(t, "no activity", m.launchSupervision(a, now), "Activity unknown")
			delete(m.garden.launch.reviews, a.ID)
			mustContain(t, "no pass", m.launchNext(a), "Next: your review when the implementation ends.")
		})
	}
}

func launchTestStop(t *testing.T) (Model, *fakeLaunchUIBackend, launch.Attempt) {
	t.Helper()
	m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
	fb.stopPending = true
	m = plannerPress(t, m, "enter", "x", "tab", "enter")
	mustContain(t, "stop pending", view(m), "Stopping… waiting for the holder to close")
	return m, fb, a
}

func launchTestPoll(t *testing.T, m Model) Model {
	t.Helper()
	l := m.garden.launch
	m, cmd := send(t, m, gardenLaunchStopTickMsg{gen: l.stopGen, issue: uint64(m.garden.board.opened), attempt: l.stopAttempt})
	return plannerRun(t, m, cmd)
}

func TestGardenLaunchStopPollsProvenClosureAndDropsOldGeneration(t *testing.T) {
	m, fb, a := launchTestStop(t)
	oldGen := m.garden.launch.stopGen
	oldTick := gardenLaunchStopTickMsg{gen: oldGen, issue: 1, attempt: a.ID}
	m = launchTestPoll(t, m)
	if fb.stops != 1 || m.garden.launch.stopPolls != 1 {
		t.Fatalf("poll repeated stop: %v", fb.calls)
	}
	// Keep an older poll result while a newer stop observation supersedes it.
	oldView, _ := fb.LaunchAttempts(context.Background(), 1)
	m.garden.launch.stopGen++
	before := fb.attemptsReads
	m, cmd := send(t, m, oldTick)
	if cmd != nil || fb.attemptsReads != before {
		t.Fatal("obsolete tick queried backend")
	}
	late := oldView
	late.Attempts[0].Revision = 100
	late.Attempts[0].State = launch.StateFailed
	m, _ = send(t, m, gardenLaunchAttemptsMsg{seq: m.garden.launch.readSeq, stopGen: oldGen, issue: 1, view: late})
	if current, _ := m.garden.launch.current(); current.State != launch.StateRunning {
		t.Fatal("older generation changed current state")
	}
	fb.attempts[0].State, fb.attempts[0].Revision = launch.StateStopped, 3
	m = launchTestPoll(t, m)
	mustContain(t, "closed", view(m), "Stopped")
	if m.garden.launch.stopAttempt != "" || fb.stops != 1 {
		t.Fatal("proven closure kept polling or re-stopped")
	}
	if m.garden.launch.attempts[0].State != launch.StateStopped {
		t.Fatal("stop polling did not update overview attempts")
	}
	m = plannerPress(t, m, "esc", "esc")
	mustContain(t, "stopped overview", view(m), "READY TO BUILD", "Earlier attempt: stopped")
}

func TestGardenLaunchStopPollBoundAndCloseOnlyDetach(t *testing.T) {
	m, fb, _ := launchTestStop(t)
	for range 60 {
		m = launchTestPoll(t, m)
	}
	mustContain(t, "poll bound", view(m), "Stop requested; closure not yet proven")
	if m.garden.launch.stopPolls != 60 || m.garden.launch.stopAttempt != "" {
		t.Fatal("stop polling not bounded")
	}
	m, fb, _ = launchTestStop(t)
	old := gardenLaunchStopTickMsg{gen: m.garden.launch.stopGen, issue: 1, attempt: m.garden.launch.stopAttempt}
	m, cmd := m.launchClose()
	m = plannerRun(t, m, cmd)
	reads := fb.attemptsReads
	m, cmd = send(t, m, old)
	m = plannerRun(t, m, cmd)
	if fb.stops != 1 || fb.cleanups != 0 || fb.attemptsReads != reads {
		t.Fatalf("closing changed holder: %v", fb.calls)
	}
}

func TestGardenLaunchCtrlBracketReleasesWriterForObservation(t *testing.T) {
	m, rig, _, id := fakeLaunchUIHistory(t, 120, 35, true)
	m, _ = fakeLaunchUIOpenTerminal(t, m, rig)
	m = plannerPress(t, m, "enter")
	streams := rig.agents.Sessions.Streams(id.SessionID)
	writer := streams[len(streams)-1]
	if !writer.Writer() {
		t.Fatal("Enter did not take input control")
	}
	m = plannerPress(t, m, "ctrl+]")
	calls := rig.agents.Sessions.Attaches()
	streams = rig.agents.Sessions.Streams(id.SessionID)
	if !writer.Closed() || streams[len(streams)-1].Writer() || !calls[len(calls)-1].Opts.ReadOnly || calls[len(calls)-1].Opts.Take || m.agents.view.control {
		t.Fatal("Ctrl+] kept writer role instead of read-only reattach")
	}
	mustContain(t, "observation", view(m), "Observing", "Enter takes control")
}

func TestGardenLaunchBoardRemoteRunningWithoutLocalJob(t *testing.T) {
	r := work.Record{Column: work.Running}
	for _, detailed := range []bool{true, false} {
		if got := boardActivity(r, detailed, true); got != "GitHub says Running · no local job on this machine" {
			t.Fatal(got)
		}
	}
}

func TestGardenLaunchSequenceOffAndUnavailable(t *testing.T) {
	m := Model{}
	for _, review := range []gardenapi.LaunchReview{{}, {Enabled: true, Reason: "Reviewer missing"}} {
		got := m.launchSequence(gardenapi.LaunchPreviewView{Review: review})
		if !strings.Contains(got, "The implementation stops at your review.") {
			t.Fatal(got)
		}
	}
}

func TestGardenLaunchReadyUnknownCacheAndLateResultDoNotGuessOrLaunch(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m.agents.api.Accounts = nil
	m = plannerPress(t, m, "enter")
	mustContain(t, "unknown implementer", view(m), "Implementer     unknown / unknown", "Permissions     unknown")
	m = plannerPress(t, m, "s")
	if len(fb.previews) != 0 {
		t.Fatal("unknown default account was guessed")
	}
	m = plannerPress(t, m, "esc", "enter")
	cmd := m.launchReadyLoad()
	m.garden.launchReady.start = true
	m.workspace = wsSettings
	m = plannerRun(t, m, cmd)
	if len(fb.previews) != 0 || m.workspace != wsSettings {
		t.Fatal("late cached data stole focus or requested a preview")
	}
}

func TestGardenLaunchConfirmationAndOverviewFitCompactASCII(t *testing.T) {
	for _, glyphs := range []string{"unicode", "ascii"} {
		m, _, fb := fakeLaunchUIEnv(t, 80, 24)
		m.work.AgentGlyphs = glyphs
		m.cfg.Upstream = "origin/main"
		m = plannerPress(t, m, "enter")
		mustFit(t, "Ready", m)
		mustContain(t, "local base", m.launchReadyOverview(), "base origin/main")
		m = plannerPress(t, m, "esc")
		fb.review = launchTestReview(true)
		fb.resolution.Launch.PermissionMode = "bypassPermissions"
		m = fakeLaunchUIReview(t, m)
		mustFit(t, "both escalations", m)
		mustContain(t, "both acknowledgment lines", view(m), "[ ] r:", "[ ] x:")
		if glyphs == "ascii" {
			m.garden.launch.stage = launchStageAttempt
			a := fakeLaunchUIAttempt(fb.lastPreview.Preview)
			m.garden.launch.attempt = a.ID
			m.garden.launch.replaceAttempts([]launch.Attempt{a})
			for _, c := range strings.Join(m.gsLaunchAttempt(80, 18), "\n") {
				if c > 127 {
					t.Fatalf("non-ASCII run chrome: %q", c)
				}
			}
		}
	}
}

func TestGardenLaunchApprovingExactPairNeverPreviewsOrStarts(t *testing.T) {
	m, rig, fb := fakeLaunchUIEnv(t, 120, 35)
	rig.garden.WorkView.Snapshot.Items[0].Verification.PlanDecisions = nil
	m = plannerPress(t, m, "R")
	m = openReview(t, m)
	m = plannerPress(t, m, "a", "tab", "enter")
	if rig.garden.Count("DecidePlan") != 1 || !m.garden.planrev.view.Review.Ready {
		t.Fatal("fixture did not approve the exact pair")
	}
	m = plannerPress(t, m, "esc")
	_ = m.View()
	if len(fb.previews) != 0 || len(fb.starts) != 0 {
		t.Fatalf("approval requested launch: %v", fb.calls)
	}
}

func TestGardenLaunchDirectPreviewIsolationRefresh(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = plannerPress(t, m, "enter", "s", "e")
	if m.garden.launch.stage != launchStageIsolation || m.garden.launch.dlg != nil {
		t.Fatal("direct preview did not open isolation settings")
	}
	m.garden.launch.iso[0].SetValue("bonsai/101-edited")
	m = plannerPress(t, m, "enter")
	if m.garden.launch.stage != launchStageConfirm || len(fb.previews) != 2 || !m.launchConfirmable() {
		t.Fatalf("edit stranded direct preview: stage=%v calls=%v", m.garden.launch.stage, fb.calls)
	}
	if fb.previews[1].Branch != "bonsai/101-edited" || m.garden.launch.btn != 0 || len(fb.starts) != 0 {
		t.Fatal("isolation edit did not get a fresh cancel-first confirmation")
	}
	m = plannerPress(t, m, "tab", "enter")
	if len(fb.starts) != 1 || fb.starts[0].Preview.Preview.Request.Branch != "bonsai/101-edited" {
		t.Fatal("edited preview was not usable")
	}
}

func TestGardenLaunchOverviewRefreshesExistingHolder(t *testing.T) {
	for _, trigger := range []string{"agent change", "tick"} {
		t.Run(trigger, func(t *testing.T) {
			m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
			m = plannerPress(t, m, "esc")
			if m.garden.launch.stage != launchStageNone || !m.garden.launchSub || !m.garden.launch.overviewPending {
				t.Fatal("existing holder overview did not subscribe and arm polling")
			}
			fb.attempts[0].State, fb.attempts[0].Revision = launch.StateAwaitingReview, a.Revision+1
			var cmd tea.Cmd
			if trigger == "agent change" {
				m, cmd = send(t, m, agentsChangedMsg{ch: make(chan struct{})})
			} else {
				m, cmd = send(t, m, gardenLaunchOverviewTickMsg{gen: m.garden.launch.overviewGen, issue: 1})
			}
			m = plannerRun(t, m, cmd)
			if m.garden.launch.attempts[0].State != launch.StateAwaitingReview || m.garden.launch.overviewPending {
				t.Fatal("holder exit did not reconcile or polling did not stop")
			}
			mustNotContain(t, "exited", view(m), "IMPLEMENTING THE APPROVED PLAN")
			if fb.stops != 0 || fb.cleanups != 0 {
				t.Fatal("overview refresh changed holder")
			}
		})
	}
}

func TestGardenLaunchOverviewTickScopeAndLateReplies(t *testing.T) {
	for _, leave := range []string{"close", "tab", "quit", "workspace"} {
		t.Run(leave, func(t *testing.T) {
			m, _, fb, _ := fakeLaunchUIHistory(t, 120, 35, false)
			m = plannerPress(t, m, "esc")
			tick := gardenLaunchOverviewTickMsg{gen: m.garden.launch.overviewGen, issue: 1}
			m, cmd := send(t, m, tick)
			late := cmd().(gardenLaunchAttemptsMsg)
			late.view.Attempts[0].State, late.view.Attempts[0].Revision = launch.StateFailed, 100
			switch leave {
			case "close":
				m = plannerPress(t, m, "esc", "enter")
			case "tab":
				m = plannerPress(t, m, "right", "left")
			case "quit":
				updated, _ := m.quit()
				m = updated.(Model)
			case "workspace":
				m.launchDetach()
				m.workspace = wsSettings
			}
			before := fb.attemptsReads
			m, cmd = send(t, m, tick)
			if cmd != nil || before != fb.attemptsReads {
				t.Fatal("obsolete overview tick queried attempts")
			}
			m, _ = send(t, m, late)
			if m.garden.launch.attempts[0].State != launch.StateRunning || fb.stops != 0 || fb.cleanups != 0 {
				t.Fatal("leaving allowed a late poll or changed holder")
			}
		})
	}
}

func TestGardenLaunchOverviewReadyHistoryAndActiveSelection(t *testing.T) {
	for _, state := range []launch.State{launch.StateStopped, launch.StateFailed, launch.StateCleaned, launch.StateAwaitingReview} {
		t.Run(string(state), func(t *testing.T) {
			m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
			fb.attempts[0].State, fb.attempts[0].Revision = state, a.Revision+1
			m = plannerPress(t, m, "R", "esc")
			mustContain(t, "Ready history", view(m), "READY TO BUILD", "Earlier attempt: "+string(state), "Attempts (i)", "Start implementation", "Change run settings")
			if _, ok := m.launchOverviewAttempt(); ok {
				t.Fatal("Ready historical attempt took over supervision")
			}
			// An active attempt takes priority over newer historical records and foreign Issues.
			a.ID, a.State = "older-active", launch.StateRunning
			m.garden.launch.attempts = append([]launch.Attempt{a}, m.garden.launch.attempts...)
			foreign := a
			foreign.ID, foreign.Issue = "foreign", 99
			m.garden.launch.attempts = append(m.garden.launch.attempts, foreign)
			if got, ok := m.launchOverviewAttempt(); !ok || got.ID != a.ID {
				t.Fatal("active attempt of opened Issue not selected")
			}
			// Non-Ready tickets may supervise the newest stopped/failed/review attempt, never cleaned history.
			m.garden.launch.attempts = fb.attempts
			for col := range m.garden.board.collection.Columns {
				for i := range m.garden.board.collection.Columns[col] {
					m.garden.board.collection.Columns[col][i].Item.Classification.Stage = garden.StageInProgress
				}
			}
			_, ok := m.launchOverviewAttempt()
			if ok != (state != launch.StateCleaned) {
				t.Fatalf("non-Ready history selection for %s = %v", state, ok)
			}
		})
	}
}

func TestGardenLaunchNextLifecycleAndReviewState(t *testing.T) {
	for _, tc := range []struct {
		state               launch.State
		stop, pass, unknown bool
		review              reviewpass.State
		want                string
	}{
		{state: launch.StateRunning, pass: true, want: "Next: one Claude review, fixes, checks, then your review."},
		{state: launch.StateRunning, want: "Next: your review when the implementation ends."},
		{state: launch.StateStartUnknown, pass: true, want: "Next: wait for the holder to be known; nothing starts again."},
		{state: launch.StateRunning, stop: true, pass: true, want: "Next: waiting for the holder to close."},
		{state: launch.StateStopped, pass: true, want: "Next: retry (r) or clean up the worktree (w) from the attempt."},
		{state: launch.StateFailed, want: "Next: retry (r) or clean up the worktree (w) from the attempt."},
		{state: launch.StateAwaitingReview, want: "Next: your review."},
		{state: launch.StateAwaitingReview, pass: true, want: "Next: automatic review status unknown; your review is still required."},
		{state: launch.StateAwaitingReview, pass: true, review: reviewpass.StateAwaitingHumanReview, want: "Next: your review (automatic review done)."},
		{state: launch.StateAwaitingReview, pass: true, review: reviewpass.StateFailed, want: "Next: your review (automatic review failed)."},
		{state: launch.StateAwaitingReview, pass: true, review: reviewpass.StateBlocked, want: "Next: your review (automatic review blocked)."},
		{state: launch.StateRunning, pass: true, unknown: true, want: "Next: automatic review status unknown; your review is still required."},
		{state: launch.StateRunning, unknown: true, want: "Next: automatic review status unknown; your review is still required."},
	} {
		t.Run(tc.want, func(t *testing.T) {
			m := Model{}
			a := launch.Attempt{ID: "attempt", State: tc.state, StopRequested: tc.stop}
			if tc.pass {
				m.garden.launch.keepReview(a.ID, &reviewpass.Pass{Revision: 1, Snapshot: reviewpass.Snapshot{AttemptID: a.ID, Policy: launchTestReview(false).Policy}, State: tc.review})
			}
			m.garden.launch.setReviewUnknown(a.ID, tc.unknown)
			if got := m.launchNext(a); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGardenLaunchReadyUsesOnlyCachedImplementerDefaults(t *testing.T) {
	for _, cached := range []bool{true, false} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			m, _, fb := fakeLaunchUIEnv(t, 120, 35)
			acc := m.agents.api.Accounts.(*fakeLaunchUIAccounts)
			acc.calls = nil
			if !cached {
				acc.resolutions = map[agents.AccountID]agents.LaunchResolution{}
			}
			m = plannerPress(t, m, "enter")
			if strings.Contains(strings.Join(acc.calls, ","), "ResolveLaunch") {
				t.Fatalf("Ready resolved implementer: %v", acc.calls)
			}
			if !cached {
				mustContain(t, "uncached", m.launchReadyOverview(), "Claude / work", "unknown", "not cached yet")
			}
			before := len(acc.calls)
			for range 3 {
				_ = m.View()
			}
			m = plannerPress(t, m, "right", "left")
			if len(acc.calls) != before {
				t.Fatal("render or tab switch re-read Ready defaults")
			}
			id := m.garden.launchReady.implementer.AccountID
			m = plannerPress(t, m, "s")
			if len(fb.previews) != 1 || fb.previews[0].AccountID != id {
				t.Fatal("direct preview used a different account than Ready")
			}
		})
	}
}

func TestGardenLaunchObservationsAndReviewsKeepLastGoodAndOrder(t *testing.T) {
	m, _, _, a := fakeLaunchUIHistory(t, 120, 35, false)
	now := time.Now()
	pass := reviewpass.Pass{Revision: 5, State: reviewpass.StatePending, Snapshot: reviewpass.Snapshot{AttemptID: a.ID, Policy: launchTestReview(false).Policy}}
	obs := gardenapi.LaunchActivity{State: "running", Activity: "working", Alive: true, ObservedAt: now.Add(-40 * time.Second)}
	read := func(activity map[string]gardenapi.LaunchActivity, reviews map[string]reviewpass.Pass, unknown bool, err error) {
		t.Helper()
		st := m.garden.gate.Last()
		st.Generation = m.garden.launch.readGen + 1
		m.garden.launch.readSeq++
		m, _ = m.onGardenLaunchAttempts(gardenLaunchAttemptsMsg{seq: m.garden.launch.readSeq, issue: 1, err: err, view: gardenapi.LaunchAttemptsView{Stamp: st, IssueID: 1, Attempts: []launch.Attempt{a}, Activity: activity, Reviews: reviews, ReviewsErr: unknown}})
	}
	read(map[string]gardenapi.LaunchActivity{a.ID: obs}, map[string]reviewpass.Pass{a.ID: pass}, false, nil)
	read(nil, nil, true, nil)
	if !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) || m.garden.launch.activity[a.ID] != obs {
		t.Fatal("failed queries discarded last good data")
	}
	mustContain(t, "last observation", m.launchSupervision(a, now), "last observed 40s ago", "Automatic review status unknown")
	mustContain(t, "unknown review", m.launchNext(a), "automatic review status unknown; your review is still required")
	olderPass, olderObs := pass, obs
	olderPass.Revision, olderPass.State = 4, reviewpass.StateFailed
	olderObs.ObservedAt, olderObs.State = now.Add(-time.Minute), "idle"
	read(map[string]gardenapi.LaunchActivity{a.ID: olderObs, "foreign": obs}, map[string]reviewpass.Pass{a.ID: olderPass}, false, nil)
	if !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) || m.garden.launch.activity[a.ID] != obs || len(m.garden.launch.activity) != 1 {
		t.Fatal("out-of-order or foreign observations were merged")
	}
	wrong := pass
	wrong.Revision, wrong.Snapshot.AttemptID = 6, "foreign"
	read(nil, map[string]reviewpass.Pass{a.ID: wrong}, false, nil)
	if !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) {
		t.Fatal("foreign pass replaced the known review")
	}
	pass.Revision, pass.State = 6, reviewpass.StateBlocked
	obs.ObservedAt, obs.State = now, "idle"
	read(map[string]gardenapi.LaunchActivity{a.ID: obs}, map[string]reviewpass.Pass{a.ID: pass}, false, nil)
	if !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) || m.garden.launch.activityStale[a.ID] {
		t.Fatal("newer data was not accepted")
	}
	read(nil, nil, false, errors.New("query unavailable"))
	mustContain(t, "failed local read", m.launchSupervision(a, now), "last observed 0s ago")
	// A late reply from a prior request cannot replace either stream.
	st := m.garden.gate.Last()
	st.Generation = m.garden.launch.readGen + 100
	oldSeq := m.garden.launch.readSeq - 1
	m, _ = m.onGardenLaunchAttempts(gardenLaunchAttemptsMsg{seq: oldSeq, issue: 1, view: gardenapi.LaunchAttemptsView{Stamp: st, IssueID: 1, Attempts: []launch.Attempt{a}, Reviews: map[string]reviewpass.Pass{a.ID: olderPass}, Activity: map[string]gardenapi.LaunchActivity{a.ID: olderObs}}})
	if m.garden.launch.activity[a.ID] != obs || !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) {
		t.Fatal("late reply replaced observations")
	}
	// Action reads also report uncertainty without deleting the frozen pass.
	m, _ = m.onGardenLaunchAction(gardenLaunchActionMsg{kind: launchActStop, issue: 1, scope: scopeOfAttempt(a), attemptID: a.ID, view: gardenapi.LaunchAttemptView{Stamp: m.garden.gate.Last(), IssueID: 1, Attempt: a, ReviewUnknown: true}})
	if !m.garden.launch.reviewUnknown[a.ID] || !reflect.DeepEqual(m.garden.launch.reviews[a.ID], pass) {
		t.Fatal("action dropped a known pass on failed read")
	}
}

func TestGardenLaunchBoardRequiresFreshSuccessfulLocalRead(t *testing.T) {
	for _, tc := range []struct {
		loaded, stale bool
		want          string
	}{
		{false, false, "GitHub says Running · local status unknown"},
		{true, true, "GitHub says Running · local status unknown"},
		{true, false, "GitHub says Running · no local job on this machine"},
	} {
		m := Model{}
		m.garden.local.loaded, m.garden.local.stale = tc.loaded, tc.stale
		r := work.Record{Column: work.Running}
		known := m.garden.local.loaded && !m.garden.local.stale
		if got := boardActivity(r, false, known); got != tc.want {
			t.Fatal(got)
		}
		if got := gardenTableSignal(r, false, known); got != tc.want {
			t.Fatal(got)
		}
	}
}

func TestGardenLaunchGitHubStageIsLastSyncedAndMirrorSeparate(t *testing.T) {
	m, _, _, a := fakeLaunchUIHistory(t, 120, 35, false)
	now := time.Now()
	m.garden.board.snapshot.SuccessfulAt = now.Add(-12 * time.Second)
	for _, tc := range []struct {
		step launch.StepState
		code string
		want string
	}{
		{launch.StepPending, "remote-pending", "pending"},
		{launch.StepDone, "", "written"},
		{launch.StepFailed, "remote-conflict", "failed"},
	} {
		a.Steps[launch.StepMirror] = launch.StepRecord{State: tc.step, Code: tc.code}
		if tc.step == launch.StepDone {
			a.MirroredStage = garden.StageInProgress
		} else {
			a.MirroredStage = ""
		}
		mustContain(t, "cached remote", m.launchSupervision(a, now), "GitHub (synced 12s ago): Ready", "stage update to In progress: "+tc.want)
	}
	m.garden.board.snapshot.SuccessfulAt = time.Time{}
	mustContain(t, "unknown sync time", m.launchSupervision(a, now), "GitHub (sync time unknown)")
}

func TestGardenLaunchOverviewFinalAttemptRefreshesOnAgentChanges(t *testing.T) {
	m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
	fb.attempts[0].State, fb.attempts[0].Revision = launch.StateAwaitingReview, a.Revision+1
	m = plannerPress(t, m, "R", "esc")
	for col := range m.garden.board.collection.Columns {
		for i := range m.garden.board.collection.Columns[col] {
			m.garden.board.collection.Columns[col][i].Item.Classification.Stage = garden.StageReview
		}
	}
	if _, ok := m.launchOverviewAttempt(); !ok || m.garden.launch.overviewPending {
		t.Fatal("final attempt should show supervision without a timer")
	}
	fb.reviews = map[string]reviewpass.Pass{a.ID: {Revision: 3, State: reviewpass.StateFailed, Snapshot: reviewpass.Snapshot{AttemptID: a.ID, Policy: launchTestReview(false).Policy}}}
	before := fb.attemptsReads
	m, cmd := send(t, m, agentsChangedMsg{ch: make(chan struct{})})
	m = plannerRun(t, m, cmd)
	if fb.attemptsReads != before+1 || m.garden.launch.overviewPending {
		t.Fatal("supervision did not reconcile once on agent change")
	}
	mustContain(t, "review failure", view(m), "Next: your review (automatic review failed).")
}

func TestGardenLaunchSupervisionActivityWording(t *testing.T) {
	for _, tc := range []struct{ activity, want string }{
		{"working", "Working"},
		{"idle", "Idle · process still running (idle is not completion)"},
		{"waiting-permission", "Waiting for permission in the session"},
		{"unknown", "Activity unknown"},
		{"", "Activity unknown"},
		{"private\x1b", "Activity unknown"},
	} {
		t.Run(tc.activity, func(t *testing.T) {
			m := Model{layoutKind: LayoutRedesign}
			a := launch.Attempt{ID: "attempt", State: launch.StateRunning}
			m.garden.launch.activity = map[string]gardenapi.LaunchActivity{a.ID: {State: "running", Activity: tc.activity, Alive: true}}
			out := m.launchSupervision(a, time.Now())
			mustContain(t, "activity", out, "IMPLEMENTING THE APPROVED PLAN", tc.want)
			mustNotContain(t, "activity", out, "Activity: running", "private")
		})
	}
}

func TestGardenLaunchStopConfirmedTwiceBeforeTick(t *testing.T) {
	m, fb, a := launchTestStop(t)
	old := gardenLaunchStopTickMsg{gen: m.garden.launch.stopGen, issue: 1, attempt: a.ID}
	m = plannerPress(t, m, "x", "tab", "enter")
	if fb.stops != 2 || !m.garden.launch.stopPending || m.garden.launch.stopGen == old.gen {
		t.Fatal("second confirmation did not arm a new stop poll")
	}
	m, cmd := send(t, m, old)
	if cmd != nil || !m.garden.launch.stopPending {
		t.Fatal("old tick disturbed the new poll")
	}
	fb.attempts[0].State, fb.attempts[0].Revision = launch.StateStopped, fb.attempts[0].Revision+1
	m = launchTestPoll(t, m)
	mustContain(t, "second stop closed", view(m), "Stopped")
	if m.garden.launch.stopAttempt != "" || m.garden.launch.stopPending || m.garden.launch.attempts[0].State != launch.StateStopped {
		t.Fatal("second stop did not observe proven closure")
	}
}

func TestGardenLaunchNextAttemptPassMatrix(t *testing.T) {
	passes := []struct {
		state    reviewpass.State
		awaiting string
		running  string
	}{
		{reviewpass.StatePending, "Next: the automatic review (Claude), fixes and checks, then your review.", "Next: one Claude review, fixes, checks, then your review."},
		{reviewpass.StateStarting, "Next: the automatic review (Claude), fixes and checks, then your review.", "Next: one Claude review, fixes, checks, then your review."},
		{reviewpass.StateStartUnknown, "Next: the automatic review (Claude), fixes and checks, then your review.", "Next: one Claude review, fixes, checks, then your review."},
		{reviewpass.StateRunning, "Next: the automatic review (Claude), fixes and checks, then your review.", "Next: one Claude review, fixes, checks, then your review."},
		{reviewpass.StateChecking, "Next: the automatic review (Claude), fixes and checks, then your review.", "Next: one Claude review, fixes, checks, then your review."},
		{reviewpass.StateAwaitingHumanReview, "Next: your review (automatic review done).", "Automatic review awaiting-human-review; your review next."},
		{reviewpass.StateFailed, "Next: your review (automatic review failed).", "Automatic review failed; your review next."},
		{reviewpass.StateBlocked, "Next: your review (automatic review blocked).", "Automatic review blocked; your review next."},
		{reviewpass.StateStopped, "Next: your review (automatic review stopped).", "Next: one Claude review, fixes, checks, then your review."},
		{"unknown", "Next: automatic review status unknown; your review is still required.", "Next: one Claude review, fixes, checks, then your review."},
		{"", "Next: your review.", "Next: your review when the implementation ends."},
	}
	for _, state := range []launch.State{launch.StatePreparing, launch.StateStarting, launch.StateStartUnknown, launch.StateRecoverable, launch.StateRunning, launch.StateAwaitingReview, launch.StateStopped, launch.StateFailed, launch.StateCleaned} {
		for _, pass := range passes {
			for _, unknown := range []bool{false, true} {
				for _, stop := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/unknown=%v/stop=%v", state, pass.state, unknown, stop), func(t *testing.T) {
						m := Model{layoutKind: LayoutRedesign}
						a := launch.Attempt{ID: "attempt", State: state, StopRequested: stop}
						if pass.state != "" {
							m.garden.launch.keepReview(a.ID, &reviewpass.Pass{Revision: 1, State: pass.state, Snapshot: reviewpass.Snapshot{AttemptID: a.ID, Policy: launchTestReview(false).Policy}})
						}
						m.garden.launch.setReviewUnknown(a.ID, unknown)
						want := pass.running
						switch {
						case stop && state.Active():
							want = "Next: waiting for the holder to close."
						case state == launch.StateStartUnknown:
							want = "Next: wait for the holder to be known; nothing starts again."
						case state == launch.StateRecoverable:
							want = "Next: retry (r) once the holder is closed; nothing restarts on its own."
						case state == launch.StateStopped || state == launch.StateFailed:
							want = "Next: retry (r) or clean up the worktree (w) from the attempt."
						case state == launch.StateCleaned:
							want = "Next: nothing; the worktree was cleaned up."
						case unknown:
							want = "Next: automatic review status unknown; your review is still required."
						case state == launch.StateAwaitingReview:
							want = pass.awaiting
						}
						if got := m.launchNext(a); got != want {
							t.Fatalf("got %q, want %q", got, want)
						}
					})
				}
			}
		}
	}
}

func TestGardenLaunchReadyReviewerSavedOverrides(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			m, _, _ := fakeLaunchUIEnv(t, 120, 35)
			m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: true, Account: "c1"}
			if override {
				m.work.GardenAutoReview.Model, m.work.GardenAutoReview.Effort = "saved-model", "low"
			}
			m = plannerPress(t, m, "enter")
			r := m.garden.launchReady.reviewer
			model, effort := "opus", "high"
			if override {
				model, effort = "saved-model", "low"
			}
			if r.Provider != "claude" || r.AccountName != "work" || r.Launch.Model != model || r.Launch.Effort != effort {
				t.Fatalf("cached reviewer: %+v", r)
			}
			acc := m.agents.api.Accounts.(*fakeLaunchUIAccounts)
			if strings.Contains(strings.Join(acc.calls, ","), "ResolveLaunch") {
				t.Fatalf("Ready resolved launch: %v", acc.calls)
			}
		})
	}
}

func TestGardenLaunchSupervisionCompactAges(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Second, "0s"}, {0, "0s"}, {59 * time.Second, "59s"},
		{time.Minute, "1m"}, {59 * time.Minute, "59m"},
		{time.Hour, "1h"}, {24*time.Hour - time.Second, "23h"}, {24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"}, {773401 * time.Second, "8d"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			now := time.Now()
			m := Model{layoutKind: LayoutRedesign}
			a := launch.Attempt{ID: "attempt", State: launch.StateRunning}
			m.garden.board.snapshot.SuccessfulAt = now.Add(-tc.age)
			m.garden.launch.activity = map[string]gardenapi.LaunchActivity{a.ID: {State: "running", Activity: "working", Alive: true, ObservedAt: now.Add(-tc.age)}}
			mustContain(t, "ages", m.launchSupervision(a, now), "observed "+tc.want+" ago", "GitHub (synced "+tc.want+" ago)")
		})
	}
}
