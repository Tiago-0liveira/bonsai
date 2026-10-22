//go:build linux || darwin

package localapi

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

func activityEvent(project string) agentactivity.Event {
	return agentactivity.Event{Type: agentactivity.ActivityChanged, At: time.Now(), Session: agentactivity.SessionRef{ID: "sess_1", ProjectID: project}}
}

// recordedCommits replaces the publisher's commit with a channel, so tests see
// each debounced commit without a snapshot.
func recordedCommits(s *Server, delay time.Duration) chan string {
	commits := make(chan string, 64)
	s.watchAgentActivity(delay)
	s.agents.activity.commit = func(project string) { commits <- project }
	return commits
}

func TestActivityBurstCommitsOnce(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	commits := recordedCommits(s, 30*time.Millisecond)
	defer s.agents.activity.stop()
	bus := s.agents.manager.Activity()
	for range 40 {
		bus.Publish(activityEvent("repo"))
	}
	bus.Publish(activityEvent("other"))
	// Non-activity events never schedule a commit.
	bus.Publish(agentactivity.Event{Type: agentactivity.LifecycleChanged, Session: agentactivity.SessionRef{ProjectID: "third"}})
	got := map[string]int{}
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case project := <-commits:
			got[project]++
		case <-timeout:
			t.Fatalf("commits = %v", got)
		}
	}
	// A late commit would be a second one for the burst.
	time.Sleep(100 * time.Millisecond)
	for len(commits) > 0 {
		got[<-commits]++
	}
	if got["repo"] != 1 || got["other"] != 1 || len(got) != 2 {
		t.Fatalf("commits = %v", got)
	}
	// The window is over: the next transition commits again.
	bus.Publish(activityEvent("repo"))
	select {
	case <-commits:
	case <-time.After(2 * time.Second):
		t.Fatal("no commit after the window")
	}
}

func TestActivityShutdownCancelsPendingTimers(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	commits := recordedCommits(s, 50*time.Millisecond)
	s.agents.manager.Activity().Publish(activityEvent("repo"))
	// Wait until the timer is armed, then close before it fires.
	waitFor(t, func() bool {
		p := s.agents.activity
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.timers) == 1
	})
	s.agents.activity.stop()
	s.agents.activity.stop() // idempotent
	s.agents.manager.Activity().Publish(activityEvent("repo"))
	select {
	case project := <-commits:
		t.Fatalf("commit for %q after stop", project)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestActivityCommitReachesSnapshotWithoutJournalOrAgentsChanged(t *testing.T) {
	s, account, provider := terminalTestServer(t)
	// A terminal busy indicator (OSC 9;4) after the first line makes the holder infer "working".
	provider.script = `read line; printf '\033]9;4;3\007'; read line`
	runtime := s.agents.runtime
	paths := testAgentHostPaths(t)
	s.agents.manager.Close(false, 10*time.Second)
	var published atomic.Int32
	s.agents.manager = testAgentManager(t, paths, runtime.Accounts, runtime.Sessions, runtime.Registry, func(project string) {
		published.Add(1)
		s.publishAgents(project)
	})
	s.watchAgentActivity(20 * time.Millisecond)
	defer s.agents.activity.stop()

	project := s.registry.Default()
	summary, err := s.agents.manager.Start("repo", gitlocal.ID(localRepositoryID, project.info.Path), agents.AccountID(account), "", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		current, _ := s.agents.manager.Get("repo", summary.ID)
		return current.State == "running" && current.Activity != nil
	})
	record, err := agenthost.Read(paths, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	writer, _, err := agenthost.Dial(record, agenthost.Request{Mode: agenthost.ModeTerminal, Client: "web"})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	// A journal row the heavy path would rewrite with the manager's summary.
	const key = "seeded"
	seeded := agentMutation{Fingerprint: "seed", Summary: agentterminal.Summary{ID: summary.ID, ProjectID: "repo", State: "running", Error: "seeded"}}
	if err := project.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "api_agent_mutations", key, seeded)
	}); err != nil {
		t.Fatal(err)
	}
	baselineNotify, baselineRevision := published.Load(), s.agents.revision.Load()
	subscription, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscription)
	before, _ := s.stateSync.CachedSnapshot("repo")

	// The script reports busy once it reads a line.
	if err := writer.Send(agenthost.Request{Type: "input", Data: []byte("go\r")}); err != nil {
		t.Fatal(err)
	}
	var update localEvent
	timeout := time.After(5 * time.Second)
	for update.Snapshot == nil {
		select {
		case event := <-events:
			if event.Type == agentsChangedEvent {
				t.Fatal("activity published agents_changed")
			}
			if event.Type == "project_update" && event.Component == "agents" && len(event.Snapshot.Agents) == 1 {
				if a := event.Snapshot.Agents[0].Activity; a != nil && a.State == agentactivity.Working {
					update = event
				}
			}
		case <-timeout:
			t.Fatal("no project_update carrying activity")
		}
	}
	if update.ProjectID != "repo" || update.Epoch != before.Epoch || update.Sequence <= before.Sequence {
		t.Fatalf("ordering: update %s/%d, before %s/%d", update.Epoch, update.Sequence, before.Epoch, before.Sequence)
	}
	raw, _ := json.Marshal(update.Snapshot.Agents[0])
	if !strings.Contains(string(raw), `"activity":{"state":"working"`) || !strings.Contains(string(raw), `"inferred":true`) {
		t.Fatalf("agent json: %s", raw)
	}
	if cached, _ := s.stateSync.CachedSnapshot("repo"); cached.Sequence != update.Sequence || cached.Agents[0].Activity == nil {
		t.Fatalf("bootstrap snapshot behind the event: %+v", cached)
	}
	if published.Load() != baselineNotify || s.agents.revision.Load() != baselineRevision {
		t.Fatal("activity took the publishAgents path")
	}
	if err := project.state.View(func(data gitstore.Data) error {
		got, _ := gitstore.Get[agentMutation](data, "api_agent_mutations", key)
		if got.Summary.Error != "seeded" || got.Summary.Activity != nil {
			t.Errorf("journal rewritten: %+v", got.Summary)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A commit built from an older list must not replace a newer snapshot, and a
// late timer only reads the manager's current list.
func TestActivityCommitUsesCurrentListAndKeepsSequenceMonotonic(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	now := time.Now().UTC()
	s.agents.manager.Restore(agentterminal.Summary{ID: "sess_old", ProjectID: "repo", State: "exited", CreatedAt: now, Activity: &agentactivity.Activity{State: agentactivity.Working}})
	s.publishAgents("repo")
	first, _ := s.stateSync.CachedSnapshot("repo")
	if len(first.Agents) != 1 || first.Agents[0].Activity != nil {
		t.Fatalf("ended session carries activity: %+v", first.Agents)
	}
	// No change: semantically equal commits are dropped, the sequence stays.
	s.publishAgentActivity("repo")
	s.publishAgentActivity("repo")
	second, _ := s.stateSync.CachedSnapshot("repo")
	if second.Sequence != first.Sequence {
		t.Fatalf("sequence %d -> %d for a no-op", first.Sequence, second.Sequence)
	}
	// An unknown project is ignored.
	s.publishAgentActivity("missing")
}

func TestPublicSummariesSanitizeActivity(t *testing.T) {
	since := time.Now().UTC()
	dirty := "Bash\x1b[31m" + strings.Repeat("x", 300)
	in := []agentterminal.Summary{
		{ID: "run", State: "running", Activity: &agentactivity.Activity{State: agentactivity.Working, Detail: dirty, Since: since}},
		{ID: "failed", State: "failed", Activity: &agentactivity.Activity{State: agentactivity.Working}},
		{ID: "stopping", State: "stopping", Activity: &agentactivity.Activity{State: agentactivity.Idle}},
		{ID: "none", State: "running"},
	}
	out := publicSummaries(in)
	if d := out[0].Activity.Detail; d != agentactivity.SanitizeDetail(dirty) || strings.ContainsRune(d, 0x1b) || len([]rune(d)) > agentactivity.MaxDetailRunes {
		t.Fatalf("detail %q", d)
	}
	if out[1].Activity != nil || out[2].Activity != nil || out[3].Activity != nil {
		t.Fatalf("activity on non-running: %+v", out)
	}
	if in[0].Activity.Detail != dirty || in[1].Activity == nil {
		t.Fatal("input mutated")
	}
	if publicSummaries(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	if withoutActivity(in[0]).Activity != nil {
		t.Fatal("journal copy keeps activity")
	}
}

func TestFailedJournalSummariesCarryNoActivity(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	project := s.registry.Default()
	activity := &agentactivity.Activity{State: agentactivity.Working, Since: time.Now().UTC()}
	// An interrupted session: the journal says active, no holder exists.
	record := agentMutation{Fingerprint: "f", Summary: agentterminal.Summary{ID: "sess_lost", ProjectID: "repo", State: "running", Activity: activity, CreatedAt: time.Now().UTC()}}
	if err := project.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "api_agent_mutations", "k", record)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverAgents(); err != nil {
		t.Fatal(err)
	}
	if err := project.state.View(func(data gitstore.Data) error {
		got, _ := gitstore.Get[agentMutation](data, "api_agent_mutations", "k")
		if got.Summary.State != "failed" || got.Summary.Activity != nil {
			t.Errorf("journal %+v", got.Summary)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if listed, ok := s.agents.manager.Get("repo", "sess_lost"); !ok || listed.State != "failed" || listed.Activity != nil {
		t.Fatalf("manager %+v", listed)
	}
}

type activityProvider struct{ terminalTestProvider }

func (p *activityProvider) ID() agents.ProviderID { return "claude" }
func (p *activityProvider) ActivityProfile() agents.ActivityProfile {
	return agents.ActivityProfile{Native: []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle}, Inferred: []agentactivity.State{agentactivity.Working}}
}
func (p *activityProvider) TranslateHook(string, []byte) []agentactivity.Signal { return nil }
func (p *activityProvider) HookOutput(string) []byte                            { return nil }

func TestAgentProvidersReportActivityProfile(t *testing.T) {
	s, _, _ := terminalTestServer(t)
	if err := s.agents.runtime.Registry.Register(&activityProvider{}); err != nil {
		t.Fatal(err)
	}
	w := agentAPIRequest(t, s, "GET", "/api/agents/providers", "", "")
	var providers []struct {
		ID       string `json:"id"`
		Activity *struct {
			Native   []string `json:"native"`
			Inferred []string `json:"inferred"`
		} `json:"activity"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &providers); err != nil {
		t.Fatal(w.Body.String())
	}
	got := map[string][2][]string{}
	for _, p := range providers {
		if p.Activity == nil || p.Activity.Native == nil || p.Activity.Inferred == nil {
			t.Fatalf("%s: activity missing or null: %s", p.ID, w.Body.String())
		}
		got[p.ID] = [2][]string{p.Activity.Native, p.Activity.Inferred}
	}
	if c := got["claude"]; strings.Join(c[0], ",") != "working,waiting,idle" || strings.Join(c[1], ",") != "working" {
		t.Fatalf("claude = %v", c)
	}
	// Antigravity (no reporter) and codex (not registered) report empty lists.
	for _, id := range []string{"antigravity", "codex"} {
		if c := got[id]; len(c[0]) != 0 || len(c[1]) != 0 {
			t.Fatalf("%s = %v", id, c)
		}
	}
}
