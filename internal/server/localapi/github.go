package localapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

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
}

func (s *Server) githubRepository(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	value, err := s.github.Repository(r.Context(), repository)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubBranches(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	value, err := s.github.Branches(r.Context(), repository)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubPullRequests(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	filter := githubdomain.PRFilter{
		State: r.URL.Query().Get("state"),
		Head:  r.URL.Query().Get("head"),
		Base:  r.URL.Query().Get("base"),
	}
	value, err := s.github.PullRequests(r.Context(), repository, filter)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubPullRequest(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}
	value, err := s.github.PullRequest(r.Context(), repository, number)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubCreatePullRequest(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var input githubdomain.CreatePullRequestRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validRepository(input.Repository) {
		writeAPIError(w, http.StatusBadRequest, "invalid", "repository must be owner/name")
		return
	}
	value, err := s.github.CreatePullRequest(r.Context(), input)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubReviewPullRequest(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	repository, ok := repositoryQuery(w, r)
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
	err := s.github.ReviewPullRequest(r.Context(), githubdomain.ReviewRequest{
		Repository: repository,
		Number: number,
		Event: body.Event,
		Body: body.Body,
		CommitID: body.CommitID,
	})
	writeGitHubResult(w, map[string]bool{"ok": err == nil}, err)
}

func (s *Server) githubComment(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	repository, ok := repositoryQuery(w, r)
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
	err := s.github.Comment(r.Context(), repository, number, body.Body)
	writeGitHubResult(w, map[string]bool{"ok": err == nil}, err)
}

func (s *Server) githubPullRequestAction(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	number, ok := pullRequestNumber(w, r)
	if !ok {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "ready":
		err = s.github.ReadyPullRequest(r.Context(), repository, number)
	case "close":
		err = s.github.ClosePullRequest(r.Context(), repository, number)
	case "reopen":
		err = s.github.ReopenPullRequest(r.Context(), repository, number)
	case "merge":
		var body struct {
			Method  string `json:"method"`
			HeadSHA string `json:"head_sha"`
		}
		if !decodeStrictJSON(w, r, &body) {
			return
		}
		err = s.github.MergePullRequest(r.Context(), githubdomain.MergePullRequestRequest{
			Repository: repository,
			Number: number,
			Method: body.Method,
			HeadSHA: body.HeadSHA,
		})
	default:
		http.NotFound(w, r)
		return
	}
	writeGitHubResult(w, map[string]bool{"ok": err == nil}, err)
}

func (s *Server) githubChecks(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	sha := strings.TrimSpace(r.PathValue("sha"))
	if sha == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid", "sha is required")
		return
	}
	value, err := s.github.Checks(r.Context(), repository, sha)
	writeGitHubResult(w, value, err)
}

func (s *Server) githubWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	repository, ok := repositoryQuery(w, r)
	if !ok {
		return
	}
	value, err := s.github.WorkflowRuns(r.Context(), repository, r.URL.Query().Get("branch"))
	writeGitHubResult(w, value, err)
}

func repositoryQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
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

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is required for mutations")
		return false
	}
	return true
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
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
