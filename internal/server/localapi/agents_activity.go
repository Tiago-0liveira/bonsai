package localapi

import (
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// activityDebounce is the window that coalesces activity transitions of one
// project into a single snapshot commit.
const activityDebounce = 250 * time.Millisecond

// activityPublisher turns the manager's activity bus into debounced "agents"
// snapshot commits. It is deliberately not publishAgents: activity must not
// rewrite the journal or publish agents_changed (which makes every page
// refetch). The commit reaches the browser as project_update, so epoch/sequence
// ordering and warm start come for free.
type activityPublisher struct {
	delay  time.Duration
	commit func(project string)
	cancel func()

	mu     sync.Mutex
	closed bool
	timers map[string]*time.Timer
	// wg counts armed or running timers so stop can wait for them.
	wg sync.WaitGroup
}

// watchAgentActivity subscribes to the current manager's activity bus. It is
// called once from New, after the manager exists.
func (s *Server) watchAgentActivity(delay time.Duration) {
	p := &activityPublisher{delay: delay, commit: s.publishAgentActivity, timers: map[string]*time.Timer{}}
	p.cancel = s.agents.manager.Activity().Subscribe("localapi-activity",
		func(e agentactivity.Event) bool { return e.Type == agentactivity.ActivityChanged },
		func(e agentactivity.Event) { p.schedule(e.Session.ProjectID) })
	s.agents.activity = p
}

// schedule arms one timer per project. Events inside the window join it, so a
// burst commits once with the latest list and the browser lags by at most delay.
func (p *activityPublisher) schedule(project string) {
	if project == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.timers[project] != nil {
		return
	}
	p.wg.Add(1)
	p.timers[project] = time.AfterFunc(p.delay, func() {
		defer p.wg.Done()
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		delete(p.timers, project)
		p.mu.Unlock()
		p.commit(project)
	})
}

// stop ends the subscription, drops pending timers and waits for a commit that
// already started.
func (p *activityPublisher) stop() {
	p.cancel()
	p.mu.Lock()
	p.closed = true
	for project, t := range p.timers {
		if t.Stop() {
			p.wg.Done()
		}
		delete(p.timers, project)
	}
	p.mu.Unlock()
	p.wg.Wait()
}

// publishAgentActivity commits the project's current sessions. It shares
// publishMu with publishAgents so an older list can never be committed after a
// newer one.
func (s *Server) publishAgentActivity(project string) {
	s.agents.publishMu.Lock()
	defer s.agents.publishMu.Unlock()
	s.commitAgents(project, s.agents.manager.List(project))
}

// commitAgents puts summaries in the project's browser snapshot.
func (s *Server) commitAgents(project string, summaries []agentterminal.Summary) {
	public := publicSummaries(summaries)
	s.stateSync.commit(project, "agents", func(snapshot *browserSnapshot) {
		snapshot.Agents = public
		now := time.Now().UTC()
		snapshot.Freshness["agents"] = browserFreshness{State: "ready", UpdatedAt: &now}
	})
}

// publicSummaries copies summaries for snapshots, which reach browsers and
// warm-start files. Activity only belongs to a running session, and its Detail
// is re-sanitized here so no upstream slip can leak a prompt or command.
func publicSummaries(in []agentterminal.Summary) []agentterminal.Summary {
	if in == nil {
		return nil
	}
	out := make([]agentterminal.Summary, len(in))
	for i, summary := range in {
		out[i] = withPublicActivity(summary)
	}
	return out
}

func withPublicActivity(summary agentterminal.Summary) agentterminal.Summary {
	if summary.Activity == nil {
		return summary
	}
	if summary.State != "running" {
		summary.Activity = nil
		return summary
	}
	activity := *summary.Activity
	activity.Detail = agentactivity.SanitizeDetail(activity.Detail)
	summary.Activity = &activity
	return summary
}
