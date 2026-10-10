package localapi

import (
	"log"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

// liveGitHubHost is the only provider host live updates support.
const liveGitHubHost = "github.com"

// handleLiveEvent turns one verified webhook delivery into provider refreshes
// for every project whose remote is the delivery's repository. It runs on the
// receiver's request goroutine, so it only queues work.
func (s *stateSync) handleLiveEvent(event webhooks.LiveEvent) {
	projects := s.liveProjects(event)
	if event.Event == "ping" {
		log.Printf("live updates: ping from %s (hook %d), %d project(s)", event.RepositoryFullName, event.HookID, len(projects))
		return
	}
	queue := s.Queue
	if s.liveQueue != nil {
		queue = s.liveQueue
	}
	force := true
	if event.ChecksEvent() {
		// Only this commit's checks changed: expire them and let a normal
		// refresh re-read just that SHA (repository data stays cached).
		s.providers.invalidateChecks(event.HeadSHA)
		force = false
	}
	for _, id := range projects {
		queue(id, refreshProvider, force)
	}
	name := event.Event
	if event.Action != "" {
		name += "/" + event.Action
	}
	log.Printf("live updates: %s for %s, %d project(s)", name, event.RepositoryFullName, len(projects))
}

// liveProjects lists the projects a delivery is about: the GitHub repository
// ID of the last provider read, or the remote name (case-insensitive) when no
// provider read has happened yet. Every remote counts, so a fork's checks
// reach the project that pushes to it.
func (s *stateSync) liveProjects(event webhooks.LiveEvent) []string {
	var out []string
	for _, info := range s.registry.List() {
		if !info.Available {
			continue
		}
		snapshot, ok := s.CachedSnapshot(info.ID)
		if !ok {
			continue
		}
		if matchesLiveRepository(snapshot, event) {
			out = append(out, info.ID)
		}
	}
	return out
}

func matchesLiveRepository(snapshot browserSnapshot, event webhooks.LiveEvent) bool {
	if event.RepositoryID != 0 && snapshot.Remote != nil && snapshot.Remote.Repository.ID == event.RepositoryID {
		return true
	}
	if event.RepositoryFullName == "" || snapshot.Local == nil {
		return false
	}
	for _, remote := range snapshot.Local.Remotes {
		if strings.EqualFold(remote.Host, liveGitHubHost) && strings.EqualFold(remote.FullName, event.RepositoryFullName) {
			return true
		}
	}
	return false
}

// liveRepositories are the GitHub repositories (owner/name) a project's
// deliveries come from: its local GitHub remotes and the last provider read.
func liveRepositories(snapshot browserSnapshot) []string {
	var out []string
	if snapshot.Local != nil {
		for _, remote := range snapshot.Local.Remotes {
			if strings.EqualFold(remote.Host, liveGitHubHost) && remote.FullName != "" {
				out = append(out, remote.FullName)
			}
		}
	}
	if snapshot.Remote != nil && snapshot.Remote.Repository.FullName != "" {
		out = append(out, snapshot.Remote.Repository.FullName)
	}
	return out
}

// liveProjectsByName lists the projects whose remote is repository.
func (s *stateSync) liveProjectsByName(repository string) []string {
	return s.liveProjects(webhooks.LiveEvent{RepositoryFullName: repository})
}
