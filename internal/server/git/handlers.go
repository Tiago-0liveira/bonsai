package git

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	bridge "github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/server/events"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, e error) {
	status := 500
	switch domain.Code(e) {
	case "unauthorized":
		status = 401
	case "forbidden", "protected":
		status = 403
	case "not_found":
		status = 404
	case "invalid":
		status = 400
	case "conflict", "busy", "dirty_worktree", "outcome_unknown":
		status = 409
	case "daemon_offline":
		status = 503
	case "too_large":
		status = 413
	case "rate_limited":
		status = 429
	}
	var de *domain.Error
	if !errors.As(e, &de) {
		de = &domain.Error{Code: domain.Code(e), Message: e.Error()}
	}
	writeJSON(w, status, map[string]any{"error": de})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return domain.ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return domain.ErrInvalid
	}
	return nil
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/github", s.Auth.Login)
	mux.HandleFunc("GET /auth/github/callback", s.Auth.Callback)
	mux.HandleFunc("POST /auth/logout", s.Auth.Logout)
	mux.HandleFunc("GET /api/daemon/connect", s.Bridge)
	mux.HandleFunc("POST /api/devices", s.enroll)
	mux.HandleFunc("DELETE /api/devices/{id}", s.revoke)
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("GET /api/projects/{projectId}/git", s.getSnapshot)
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		user, e := s.principal(r)
		if e != nil {
			writeError(w, e)
			return
		}
		s.Events.Stream(w, r, func(v events.Event) bool {
			current, err := s.Auth.Authenticate(r)
			return err == nil && current == user && s.authorized(user, v.ProjectID, false)
		})
	})
	localRoutes := map[string]string{
		"GET /api/projects/{projectId}/branches": "git.branches", "GET /api/projects/{projectId}/worktrees": "git.worktrees",
		"POST /api/projects/{projectId}/worktrees": "git.worktree.create", "DELETE /api/worktrees/{id}": "git.worktree.remove",
		"GET /api/worktrees/{id}/status": "git.status", "GET /api/worktrees/{id}/files": "git.files", "GET /api/worktrees/{id}/files/{path...}": "git.file.read", "GET /api/worktrees/{id}/diff": "git.diff.read",
	}
	for _, action := range []string{"fetch", "pull", "push", "commit", "rebase", "merge", "stage", "unstage"} {
		localRoutes["POST /api/worktrees/{id}/"+action] = "git." + action
	}
	for _, action := range []string{"continue", "abort"} {
		localRoutes["POST /api/worktrees/{id}/operations/"+action] = "git.operation." + action
	}
	for pattern, kind := range localRoutes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { s.local(w, r, kind) })
	}
	for _, pattern := range []string{"GET /api/projects/{projectId}/pull-requests", "GET /api/projects/{projectId}/pull-requests/{number}", "POST /api/projects/{projectId}/pull-requests", "POST /api/projects/{projectId}/pull-requests/{number}/{action}", "GET /api/projects/{projectId}/checks/{sha}", "GET /api/projects/{projectId}/workflows"} {
		mux.HandleFunc(pattern, s.remote)
	}
	mux.HandleFunc("PATCH /api/worktrees/{id}/metadata", s.metadata)
}
func (s *Service) projects(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	out := []Repository{}
	for id, repo := range s.Repositories {
		if s.authorized(user, id, false) {
			out = append(out, repo)
		}
	}
	writeJSON(w, 200, out)
}
func (s *Service) getSnapshot(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	repo := r.PathValue("projectId")
	if e != nil {
		writeError(w, e)
		return
	}
	if !s.authorized(user, repo, false) {
		writeError(w, domain.ErrForbidden)
		return
	}
	if r.URL.Query().Get("fresh") == "true" {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		result, e := s.execute(ctx, bridge.Command{UserID: user, RepositoryID: repo, Type: "git.repository.refresh"})
		if e != nil {
			writeError(w, e)
			return
		}
		if result.Error != nil {
			writeError(w, result.Error)
			return
		}
		var local domain.RepositoryState
		if json.Unmarshal(result.Payload, &local) != nil {
			writeError(w, domain.ErrInvalid)
			return
		}
		if e = s.setLocal(repo, local); e != nil {
			writeError(w, e)
			return
		}
	}
	writeJSON(w, 200, s.snapshot(repo))
}
func (s *Service) local(w http.ResponseWriter, r *http.Request, kind string) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	repo := r.PathValue("projectId")
	wt := r.PathValue("id")
	if wt != "" {
		repo, e = s.worktreeRepo(wt)
		if e != nil && kind == "git.worktree.remove" {
			s.Store.View(func(d store.Data) error {
				old, ok := store.Get[commandRecord](d, "daemon_commands", r.Header.Get("Idempotency-Key"))
				if ok && old.Command.UserID == user && old.Command.WorktreeID == wt && old.Command.Type == kind {
					repo = old.Command.RepositoryID
					e = nil
				}
				return nil
			})
		}
		if e != nil {
			writeError(w, e)
			return
		}
	}
	if !s.authorized(user, repo, !bridge.IsRead(kind)) {
		writeError(w, domain.ErrForbidden)
		return
	}
	var args json.RawMessage
	if r.Method == "GET" {
		q := map[string]string{}
		for _, key := range []string{"mode", "base"} {
			if value := r.URL.Query().Get(key); value != "" {
				q[key] = value
			}
		}
		if path := r.PathValue("path"); path != "" {
			q["path"] = path
		}
		args = bridge.MarshalArguments(q)
	} else {
		args = json.RawMessage(`{}`)
		if r.ContentLength != 0 {
			if e = decode(w, r, &args); e != nil {
				writeError(w, e)
				return
			}
		}
	}
	id := r.Header.Get("Idempotency-Key")
	if len(id) > 128 {
		writeError(w, domain.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 130*time.Second)
	defer cancel()
	result, e := s.execute(ctx, bridge.Command{ID: id, UserID: user, RepositoryID: repo, WorktreeID: wt, Type: kind, Arguments: args})
	if e != nil {
		writeError(w, e)
		return
	}
	if result.Error != nil {
		writeError(w, result.Error)
		return
	}
	if !bridge.IsRead(kind) {
		snapshotResult, err := s.execute(ctx, bridge.Command{UserID: user, RepositoryID: repo, Type: "git.repository.refresh"})
		if err == nil && snapshotResult.Error == nil {
			var snapshot domain.RepositoryState
			if json.Unmarshal(snapshotResult.Payload, &snapshot) == nil {
				_ = s.setLocal(repo, snapshot)
			}
		}
		eventType := "repository.updated"
		if kind == "git.commit" {
			eventType = "commit.created"
		}
		if kind == "git.rebase" || kind == "git.merge" || kind == "git.pull" || strings.HasPrefix(kind, "git.operation.") {
			eventType = "git.operation.updated"
		}
		_ = s.publish(repo, "daemon", eventType, wt, result.ID, result.Payload)
		writeJSON(w, 200, map[string]any{"command_id": result.ID, "result": result.Payload, "snapshot": s.snapshot(repo)})
		return
	}
	writeJSON(w, 200, result.Payload)
}
func (s *Service) enroll(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req struct {
		RepositoryIDs []string `json:"repository_ids"`
	}
	if e = decode(w, r, &req); e != nil {
		writeError(w, e)
		return
	}
	if len(req.RepositoryIDs) == 0 {
		writeError(w, domain.ErrInvalid)
		return
	}
	for _, id := range req.RepositoryIDs {
		if !s.authorized(user, id, true) {
			writeError(w, domain.ErrForbidden)
			return
		}
	}
	credential := rand.Text()
	d := Device{ID: rand.Text(), UserID: user, RepositoryIDs: req.RepositoryIDs, CreatedAt: time.Now().UTC()}
	if e = s.Store.Update(func(data store.Data) error { return store.Put(data, "devices", githubapp.Hash(credential), d) }); e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 201, map[string]any{"device": d, "credential": credential})
}
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id := r.PathValue("id")
	e = s.Store.Update(func(d store.Data) error {
		for key := range d["devices"] {
			dev, ok := store.Get[Device](d, "devices", key)
			if ok && dev.ID == id && dev.UserID == user {
				dev.Revoked = true
				return store.Put(d, "devices", key, dev)
			}
		}
		return domain.ErrNotFound
	})
	if e != nil {
		writeError(w, e)
		return
	}
	s.mu.Lock()
	for _, c := range s.devices {
		if c.device.ID == id {
			c.ws.Close()
		}
	}
	s.mu.Unlock()
	w.WriteHeader(204)
}
func (s *Service) metadata(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id := r.PathValue("id")
	repo, e := s.worktreeRepo(id)
	if e != nil {
		writeError(w, e)
		return
	}
	if !s.authorized(user, repo, true) {
		writeError(w, domain.ErrForbidden)
		return
	}
	var patch struct {
		MergeTargetBranch *string `json:"merge_target_branch"`
		Tag               *string `json:"tag"`
		StackPreference   *string `json:"stack_preference"`
	}
	if e = decode(w, r, &patch); e != nil {
		writeError(w, e)
		return
	}
	var meta Metadata
	e = s.Store.Update(func(d store.Data) error {
		meta, _ = store.Get[Metadata](d, "worktree_metadata", id)
		meta.WorktreeID = id
		meta.RepositoryID = repo
		if patch.MergeTargetBranch != nil {
			if len(*patch.MergeTargetBranch) > 255 {
				return domain.ErrInvalid
			}
			meta.MergeTargetBranch = *patch.MergeTargetBranch
		}
		if patch.Tag != nil {
			if len(*patch.Tag) > 100 {
				return domain.ErrInvalid
			}
			meta.Tag = *patch.Tag
		}
		if patch.StackPreference != nil {
			if *patch.StackPreference != "auto" && *patch.StackPreference != "never" {
				return domain.ErrInvalid
			}
			meta.StackPreference = *patch.StackPreference
		}
		return store.Put(d, "worktree_metadata", id, meta)
	})
	if e != nil {
		writeError(w, e)
		return
	}
	if e = s.publish(repo, "bonsai", "worktree.updated", id, "", meta); e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, meta)
}
func (s *Service) remote(w http.ResponseWriter, r *http.Request) {
	user, e := s.principal(r)
	if e != nil {
		writeError(w, e)
		return
	}
	repo := r.PathValue("projectId")
	mutation := r.Method != "GET"
	if !s.authorized(user, repo, mutation) {
		writeError(w, domain.ErrForbidden)
		return
	}
	if s.GitHub == nil {
		writeError(w, domain.ErrAuth)
		return
	}
	full := s.Repositories[repo].FullName
	ctx := githubapp.WithUser(r.Context(), user)
	number, _ := strconv.Atoi(r.PathValue("number"))
	var value any
	if !mutation {
		switch {
		case strings.Contains(r.URL.Path, "/checks/"):
			value, e = s.cached(ctx, r.URL.RequestURI(), func() (any, error) { return s.GitHub.Checks(ctx, full, r.PathValue("sha")) })
		case strings.HasSuffix(r.URL.Path, "/workflows"):
			value, e = s.cached(ctx, r.URL.RequestURI(), func() (any, error) { return s.GitHub.WorkflowRuns(ctx, full, r.URL.Query().Get("branch")) })
		case r.PathValue("number") != "":
			value, e = s.cached(ctx, r.URL.RequestURI(), func() (any, error) { return s.GitHub.PullRequest(ctx, full, number) })
		default:
			snap := s.snapshot(repo)
			if snap.Remote == nil {
				e = s.Reconcile(ctx, repo)
				snap = s.snapshot(repo)
			}
			if snap.Remote != nil {
				value = snap.Remote.PullRequests
			}
		}
	} else {
		var body json.RawMessage
		if e = decode(w, r, &body); e != nil {
			writeError(w, e)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 128 {
			writeError(w, domain.E("invalid", "Idempotency-Key required for GitHub mutations"))
			return
		}
		identity := githubapp.Hash(user + "\x00" + repo + "\x00" + r.URL.Path + "\x00" + string(body))
		var prior remoteAudit
		var exists bool
		e = s.Store.Update(func(d store.Data) error {
			prior, exists = store.Get[remoteAudit](d, "github_commands", key)
			if exists {
				if prior.Hash != identity {
					return domain.ErrInvalid
				}
				return nil
			}
			return store.Put(d, "github_commands", key, remoteAudit{Hash: identity, State: "running"})
		})
		if e != nil {
			writeError(w, e)
			return
		}
		if exists {
			if prior.State == "running" {
				writeError(w, domain.ErrUncertain)
				return
			}
			if prior.Error != nil {
				writeError(w, prior.Error)
				return
			}
			writeJSON(w, 200, prior.Result)
			return
		}
		switch r.PathValue("action") {
		case "":
			var req gh.CreatePullRequestRequest
			e = json.Unmarshal(body, &req)
			req.Repository = full
			if e == nil {
				value, e = s.GitHub.CreatePullRequest(ctx, req)
			}
		case "reviews":
			var req gh.ReviewRequest
			e = json.Unmarshal(body, &req)
			req.Repository = full
			req.Number = number
			if e == nil {
				e = s.GitHub.ReviewPullRequest(ctx, req)
			}
		case "comments":
			var req struct {
				Body string `json:"body"`
			}
			e = json.Unmarshal(body, &req)
			if e == nil {
				e = s.GitHub.Comment(ctx, full, number, req.Body)
			}
		case "ready":
			e = s.GitHub.ReadyPullRequest(ctx, full, number)
		case "close":
			e = s.GitHub.ClosePullRequest(ctx, full, number)
		case "reopen":
			e = s.GitHub.ReopenPullRequest(ctx, full, number)
		case "merge":
			var req gh.MergePullRequestRequest
			e = json.Unmarshal(body, &req)
			req.Repository = full
			req.Number = number
			if e == nil {
				e = s.GitHub.MergePullRequest(ctx, req)
			}
		default:
			e = domain.ErrInvalid
		}
		// Network/cancellation failures can follow a successful remote mutation.
		// Leave those journal entries uncertain instead of claiming safe retry.
		record := remoteAudit{Hash: identity, State: "done"}
		record.Result, _ = json.Marshal(value)
		if e != nil {
			var de *domain.Error
			if errors.As(e, &de) {
				record.Error = de
			} else {
				record.State = "running"
				e = domain.ErrUncertain
			}
		}
		if err := s.Store.Update(func(d store.Data) error { return store.Put(d, "github_commands", key, record) }); err != nil {
			writeError(w, domain.ErrUncertain)
			return
		}
		if e == nil {
			_ = s.Reconcile(ctx, repo)
			_ = s.publish(repo, "github", "pull_request.updated", strconv.Itoa(number), key, value)
		}
	}
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, value)
}

type remoteAudit struct {
	Hash   string          `json:"hash"`
	State  string          `json:"state"`
	Result json.RawMessage `json:"result"`
	Error  *domain.Error   `json:"error,omitempty"`
}
