package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/gym"
)

type gymInfoMsg struct {
	info *agym.InfoData
	err  error
}

type gymAgentViewMsg struct {
	worktreePath string
	view         *gym.AgentView
	err          error
}

type gymEventsMsg struct {
	runID      string
	events     []agym.Event
	nextCursor uint64
	hasMore    bool
	err        error
}

type gymTickMsg time.Time

func tickGym(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return gymTickMsg(t)
	})
}

func loadGymView(svc *gym.Service, worktreePath string) tea.Cmd {
	return func() tea.Msg {
		if svc == nil || worktreePath == "" {
			return gymAgentViewMsg{worktreePath: worktreePath}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		view, err := svc.GetAgentView(ctx, worktreePath)
		return gymAgentViewMsg{
			worktreePath: worktreePath,
			view:         view,
			err:          err,
		}
	}
}

func pollGymEvents(svc *gym.Service, runID string, after uint64) tea.Cmd {
	return func() tea.Msg {
		if svc == nil || runID == "" {
			return gymEventsMsg{runID: runID}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		page, err := svc.Client().GetEvents(ctx, runID, after, 200)
		if err != nil {
			return gymEventsMsg{runID: runID, err: err}
		}
		if page == nil || (page.HasMore && len(page.Events) == 0) {
			return gymEventsMsg{runID: runID, err: agym.ErrInvalidResponse}
		}
		return gymEventsMsg{
			runID:      runID,
			events:     page.Events,
			nextCursor: page.NextCursor,
			hasMore:    page.HasMore,
		}
	}
}

func startAgent(svc *gym.Service, worktreePath, profile, task string) tea.Cmd {
	return func() tea.Msg {
		if svc == nil {
			return opDoneMsg{label: "start agent", err: gym.ErrGymUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := svc.Start(ctx, worktreePath, profile, task)
		return opDoneMsg{label: "start agent", err: err}
	}
}

func stopAgent(svc *gym.Service, worktreePath, runID string) tea.Cmd {
	return func() tea.Msg {
		if svc == nil {
			return opDoneMsg{label: "stop agent", err: gym.ErrGymUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := svc.Stop(ctx, worktreePath, runID)
		return opDoneMsg{label: "stop agent", err: err}
	}
}

type gymAutoRunMsg struct {
	result *gym.AutoRunResult
	err    error
}

func startAgentAutoWorktree(svc *gym.Service, profile, task string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		if svc == nil {
			return gymAutoRunMsg{err: gym.ErrGymUnavailable}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		res, err := svc.StartAutoWorktree(ctx, profile, task, "", cfg)
		return gymAutoRunMsg{result: res, err: err}
	}
}
