package localapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type githubAudit struct {
	Hash   string          `json:"hash"`
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (s *Server) registerGitHubRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/github/repository", s.githubRepository)
	mux.HandleFunc("GET /api/github/branches", s.githubBranches)
	mux.HandleFunc("GET /api/github/pull-requests", s.githubPullRequests)
	mux.HandleFunc("GET /api/github/pull-requests/{number}", s.githubPullRequest)
	mux.HandleFunc("POST /api/github/pull-requests", s.githubCreatePullRequest)
	mux.HandleFunc("POST /api/github/pull-requests/{number}/reviews", s.githubReviewPullRequest)
	mux.HandleFunc("POST /api/github/pull-requests/{number}/comments", s.githubComment)
	mux.HandleFunc("POST /api/github/pull-requests/{number}/{action}", s.githubPullRequestAction)
	mux.HandleFunc("GET /api/github/checks/{sha}", s.githubChecks)
	mux.HandleFunc("GET /api/github/workflows", s.githubWorkflowRuns)

	// Compatibility routes used by Bonsai Web. The project ID is intentionally
	// local-only; GitHub repository identity is discovered by local gh.
	mux.HandleFunc("GET /api/projects/{projectId}/pull-requests", s.githubPullRequests)
	mux.HandleFunc("GET /api/projects/{projectId}/pull-requests/{number}", s.githubPullRequest)
	mux.HandleFunc("POST /api/projects/{projectId}/pull-requests", s.githubCreatePullRequest)
	mux.HandleFunc("POST /api/projects/{projectId}/pull-requests/{number}/reviews", s.githubReviewPullRequest)
	mux.HandleFunc("POST /api/projects/{projectId}/pull-requests/{number}/comments", s.githubComment)
	mux.HandleFunc("POST /api/projects/{projectId}/pull-requests/{number}/{action}", s.githubPullRequestAction)
	mux.HandleFunc("GET /api/projects/{projectId}/checks/{sha}", s.githubChecks)
	mux.HandleFunc("GET /api/projects/{projectId}/workflows", s.githubWorkflowRuns)
}

func (s *Server) githubRepository(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	value, err := s.registry.Default().github.Repository(r.Context(), repository)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubBranches(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	value, err := s.registry.Default().github.Branches(r.Context(), repository)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubPullRequests(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	filter := githubdomain.PRFilter{
		State: r.URL.Query().Get("state"),
		Head:  r.URL.Query().Get("head"),
		Base:  r.URL.Query().Get("base"),
	}
	value, err := s.registry.Default().github.PullRequests(r.Context(), repository, filter)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubPullRequest(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}
	value, err := s.registry.Default().github.PullRequest(r.Context(), repository, number)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubCreatePullRequest(w http.ResponseWriter, r *http.Request) {
	var input githubdomain.CreatePullRequestRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if r.PathValue("projectId") != "" {
		repository, ok := s.repositoryForRequest(w, r)
		if !ok {
			return
		}
		input.Repository = repository
	} else if !validRepository(input.Repository) {
		writeAPIError(w, http.StatusBadRequest, "invalid", "repository must be owner/name")
		return
	}
	s.githubMutation(w, r, "pull_request.create", input.Repository, input, func() (any, error) {
		return s.registry.Default().github.CreatePullRequest(r.Context(), input)
	})
}

func (s *Server) githubReviewPullRequest(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}
	var body struct {
		Event    string `json:"event"`
		Body     string `json:"body"`
		CommitID string `json:"commit_id,omitempty"`
	}
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	request := githubdomain.ReviewRequest{
		Repository: repository,
		Number:     number,
		Event:      body.Event,
		Body:       body.Body,
		CommitID:   body.CommitID,
	}
	s.githubMutation(w, r, "pull_request.review", repository, request, func() (any, error) {
		err := s.registry.Default().github.ReviewPullRequest(r.Context(), request)
		return map[string]bool{"ok": err == nil}, err
	})
}

func (s *Server) githubComment(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	identity := struct {
		Repository string `json:"repository"`
		Number     int    `json:"number"`
		Body       string `json:"body"`
	}{repository, number, body.Body}
	s.githubMutation(w, r, "pull_request.comment", repository, identity, func() (any, error) {
		err := s.registry.Default().github.Comment(r.Context(), repository, number, body.Body)
		return map[string]bool{"ok": err == nil}, err
	})
}

func (s *Server) githubPullRequestAction(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}

	action := r.PathValue("action")
	switch action {
	case "ready", "close", "reopen":
		identity := struct {
			Repository string `json:"repository"`
			Number     int    `json:"number"`
			Action     string `json:"action"`
		}{repository, number, action}
		s.githubMutation(w, r, "pull_request."+action, repository, identity, func() (any, error) {
			var err error
			switch action {
			case "ready":
				err = s.registry.Default().github.ReadyPullRequest(r.Context(), repository, number)
			case "close":
				err = s.registry.Default().github.ClosePullRequest(r.Context(), repository, number)
			case "reopen":
				err = s.registry.Default().github.ReopenPullRequest(r.Context(), repository, number)
			}
			return map[string]bool{"ok": err == nil}, err
		})
	case "merge":
		var body struct {
			Method  string `json:"method"`
			HeadSHA string `json:"head_sha"`
		}
		if !decodeStrictJSON(w, r, &body) {
			return
		}
		request := githubdomain.MergePullRequestRequest{
			Repository: repository,
			Number:     number,
			Method:     body.Method,
			HeadSHA:    body.HeadSHA,
		}
		s.githubMutation(w, r, "pull_request.merge", repository, request, func() (any, error) {
			err := s.registry.Default().github.MergePullRequest(r.Context(), request)
			return map[string]bool{"ok": err == nil}, err
		})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) githubChecks(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	sha := strings.TrimSpace(r.PathValue("sha"))
	if sha == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid", "sha is required")
		return
	}
	value, err := s.registry.Default().github.Checks(r.Context(), repository, sha)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.repositoryForRequest(w, r)
	if !ok {
		return
	}
	value, err := s.registry.Default().github.WorkflowRuns(r.Context(), repository, r.URL.Query().Get("branch"))
	writeGitHubResult(w, value, err)
}

func (s *Server) repositoryForRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	if projectID := r.PathValue("projectId"); projectID != "" {
		if !s.requireLocalProject(w, r) {
			return "", false
		}
		discovered, err := ghcli.Discover(r.Context(), s.repoDir)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "github_unavailable", err.Error())
			return "", false
		}
		if !validRepository(discovered.FullName) {
			writeAPIError(w, http.StatusBadGateway, "github_unavailable", "gh returned an invalid repository")
			return "", false
		}
		return discovered.FullName, true
	}
	repository := strings.TrimSpace(r.URL.Query().Get("repository"))
	if !validRepository(repository) {
		writeAPIError(w, http.StatusBadRequest, "invalid", "repository must be owner/name")
		return "", false
	}
	return repository, true
}

func validRepository(repository string) bool {
	return repository != "" &&
		strings.Count(repository, "/") == 1 &&
		!strings.ContainsAny(repository, "\x00\r\n\t ") &&
		!strings.HasPrefix(repository, "/") &&
		!strings.HasSuffix(repository, "/")
}

func pullRequestNumber(w http.ResponseWriter, r *http.Request) (int, bool) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "invalid pull request number")
		return 0, false
	}
	return number, true
}

func (s *Server) githubMutation(
	w http.ResponseWriter,
	r *http.Request,
	operation, repository string,
	identity any,
	run func() (any, error),
) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is required for mutations")
		return
	}
	rawIdentity, err := json.Marshal(identity)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid", "invalid mutation identity")
		return
	}
	sum := sha256.Sum256([]byte(operation + "\x00" + repository + "\x00" + string(rawIdentity)))
	hash := hex.EncodeToString(sum[:])

	var prior githubAudit
	var exists bool
	err = s.state.Update(func(data gitstore.Data) error {
		prior, exists = gitstore.Get[githubAudit](data, "github_commands", key)
		if exists {
			if prior.Hash != hash {
				return fmt.Errorf("idempotency key was already used for a different mutation")
			}
			return nil
		}
		return gitstore.Put(data, "github_commands", key, githubAudit{Hash: hash, State: "running"})
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if exists {
		switch prior.State {
		case "done":
			if prior.Error != "" {
				writeAPIError(w, http.StatusBadGateway, "github_error", prior.Error)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(prior.Result)
			return
		default:
			writeAPIError(w, http.StatusConflict, "outcome_unknown", "GitHub mutation is already in progress or its outcome is unknown")
			return
		}
	}

	value, runErr := run()
	result, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		runErr = fmt.Errorf("encode GitHub mutation result: %w", marshalErr)
		result = nil
	}
	record := githubAudit{Hash: hash, State: "done", Result: result}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	if err := s.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "github_commands", key, record)
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "journal_failed", "failed to persist GitHub mutation result")
		return
	}
	if runErr != nil {
		writeAPIError(w, http.StatusBadGateway, "github_error", runErr.Error())
		return
	}
	s.publishProjectEvent("")
	writeJSON(w, http.StatusOK, value)
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "JSON request body is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		} else {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request")
		}
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON value")
		return false
	}
	return true
}

func writeGitHubResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "github_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}
