package connect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Remote is a local Git remote name and URL supplied by a caller-owned Git adapter.
type Remote struct{ Name, URL string }

// RemoteLister is the seam for reading configured Git remotes.
type RemoteLister interface {
	ListRemotes(context.Context) ([]Remote, error)
}

// RepositoryResolver resolves a GitHub URL to stable remote identity and permissions.
type RepositoryResolver interface {
	ResolveRepository(context.Context, Remote) (Repository, error)
}

// RepositoryByIDResolver supplies display metadata when a fork's parent is a selectable target.
type RepositoryByIDResolver interface {
	ResolveRepositoryID(context.Context, garden.RepoID, string) (Repository, error)
}

// Repository is the safe subset needed to identify and authorize a Garden.
type Repository struct {
	ID                garden.RepoID
	FullName          string
	Fork              bool
	ParentID          garden.RepoID
	ParentFullName    string
	Access            Access
	ProjectScope      bool
	ProjectScopeKnown bool
}

// Candidate couples a local remote with its resolved repository.
type Candidate struct {
	Remote     Remote
	Aliases    []string
	Repository Repository
	Upstream   bool
}

// AmbiguousError includes only selectable candidates, never raw auth errors.
type AmbiguousError struct{ Candidates []Candidate }

func (e *AmbiguousError) Error() string { return ErrTargetAmbiguous.Error() }
func (e *AmbiguousError) Unwrap() error { return ErrTargetAmbiguous }

// Target identifies a remote Garden by numeric repository ID.
type Target struct {
	GardenID             garden.GardenID
	RemoteName, FullName string
	RemoteAliases        []string
	Repository           Repository
}

// ResolveTarget returns the sole candidate, or validates an explicit candidate choice.
func ResolveTarget(ctx context.Context, remotes RemoteLister, resolver RepositoryResolver, chosen *Candidate) (Target, []string, error) {
	rs, err := remotes.ListRemotes(ctx)
	if err != nil {
		return Target{}, nil, safeError(err)
	}
	var candidates []Candidate
	var skipped []string
	for _, r := range rs {
		r = sanitizedRemote(r)
		c, e := resolver.ResolveRepository(ctx, r)
		if e != nil {
			if errors.Is(e, ErrNoGitHubRemote) {
				skipped = append(skipped, r.Name+": not a GitHub repository")
				continue
			}
			return Target{}, skipped, safeError(e)
		}
		if c.ID == 0 {
			continue
		}
		addCandidate := func(candidate Candidate) {
			for i := range candidates {
				if candidates[i].Repository.ID == candidate.Repository.ID {
					if candidate.Remote.Name != "" && !containsString(candidates[i].Aliases, candidate.Remote.Name) {
						candidates[i].Aliases = append(candidates[i].Aliases, candidate.Remote.Name)
					}
					return
				}
			}
			candidate.Aliases = []string{candidate.Remote.Name}
			candidates = append(candidates, candidate)
		}
		addCandidate(Candidate{Remote: r, Repository: c})
		if c.Fork && c.ParentID != 0 {
			parent := Repository{ID: c.ParentID, FullName: c.ParentFullName}
			if byID, ok := resolver.(RepositoryByIDResolver); ok {
				resolved, resolveErr := byID.ResolveRepositoryID(ctx, c.ParentID, c.ParentFullName)
				if resolveErr != nil {
					return Target{}, skipped, safeError(resolveErr)
				}
				if resolved.ID != 0 {
					parent = resolved
				}
			}
			addCandidate(Candidate{Remote: r, Repository: parent, Upstream: true})
		}
	}
	if len(candidates) == 0 {
		return Target{}, skipped, ErrNoGitHubRemote
	}
	if chosen != nil {
		found := false
		for _, c := range candidates {
			if (chosen.Repository.ID != 0 && c.Repository.ID == chosen.Repository.ID) || (chosen.Repository.FullName != "" && strings.EqualFold(c.Repository.FullName, chosen.Repository.FullName)) {
				chosen = &c
				found = true
				break
			}
		}
		if !found {
			return Target{}, skipped, fmt.Errorf("chosen target is not a candidate")
		}
		candidates = []Candidate{*chosen}
	}
	if chosen == nil && len(candidates) > 1 {
		return Target{}, skipped, &AmbiguousError{Candidates: candidates}
	}
	c := candidates[0]
	id, err := garden.NewGardenID(c.Repository.ID)
	if err != nil {
		return Target{}, skipped, err
	}
	return Target{GardenID: id, RemoteName: c.Remote.Name, FullName: c.Repository.FullName, RemoteAliases: append([]string(nil), c.Aliases...), Repository: c.Repository}, skipped, nil
}

// sanitizedRemote removes credential and non-identity URL components before a
// remote crosses into resolver-owned state or any result/error can retain it.
func sanitizedRemote(r Remote) Remote {
	s := r.URL
	if !strings.Contains(s, "://") && strings.Contains(s, "@") && strings.Contains(s[:strings.Index(s, "@")], ":") {
		at := strings.Index(s, "@")
		if strings.Contains(s[:at], ":") {
			s = s[at+1:]
		}
		if i := strings.IndexAny(s, "?#"); i >= 0 {
			s = s[:i]
		}
	} else if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		u.User = nil
		u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
		s = u.String()
	} else {
		// SCP syntax has no URL parser representation. Drop the authority user.
		if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
			authority := s[:i]
			if at := strings.LastIndex(authority, "@"); at >= 0 {
				userinfo := authority[:at]
				if strings.Contains(userinfo, ":") {
					s = authority[at+1:] + s[i:]
				}
			}
		}
		if i := strings.IndexAny(s, "?#"); i >= 0 {
			s = s[:i]
		}
	}
	r.URL = s
	return r
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Access is the permission level observed when the connection is made.
type Access uint8

const (
	AccessNone Access = iota
	AccessRead
	AccessWrite
	AccessAdmin
)

// GitHubIdentity is a stable GitHub account identity label.
type GitHubIdentity struct {
	ID    string
	Login string
}

// AgentAccount is a coding-agent profile identity, intentionally distinct from GitHubIdentity.
type AgentAccount struct{ Provider, ProfileID string }

// Mode records the explicit setup choice.
type Mode string

const (
	Create          Mode = "create"
	ConnectExisting Mode = "connect-existing"
	Skip            Mode = "skip"
)

// ProjectRef identifies a selected existing Project.
type ProjectRef struct{ ID, Number, Owner string }

// Connection is the local authorization and setup capability for a Garden.
type Connection struct {
	Target            Target
	Access            Access
	ProjectScope      bool
	ProjectScopeKnown bool
	Identity          GitHubIdentity
	// ViewerLogin is the authenticated GitHub login used for Project ownership checks.
	ViewerLogin   string
	ProjectReader garden.ProjectReader
	ProjectWriter garden.ProjectSetupWriter
}

// Reader is available for all readable connections.
func (c Connection) Reader() (garden.ProjectReader, error) {
	if c.Access < AccessRead {
		return nil, ErrAuthRequired
	}
	return c.ProjectReader, nil
}

// Setup returns setup write capability only for sufficiently authorized connections.
func (c Connection) Setup() (garden.ProjectSetupWriter, error) {
	if c.Access < AccessWrite {
		return nil, ErrReadOnly
	}
	return c.ProjectWriter, nil
}

// Label is observed remote label metadata.
type Label struct{ Name, Color, Description string }

// Desired is the bounded set of owned resources the planner may create.
type Desired struct {
	Labels          []Label
	ProjectName     string
	Fields          []garden.ProjectField
	ImportCount     int
	ExistingProject *ProjectRef
	Owner           string
}

// Observed is a snapshot obtained through read ports before planning.
type Observed struct {
	Labels   []Label
	Projects []garden.Project
}

// StepKind names a previewable setup action.
type StepKind string

const (
	CreateLabel        StepKind = "create-label"
	ReuseLabel         StepKind = "reuse-label"
	ConflictLabel      StepKind = "conflict-label"
	ConflictProject    StepKind = "conflict-project"
	CreateProject      StepKind = "create-project"
	EnsureProjectField StepKind = "ensure-project-field"
	ManualStep         StepKind = "manual"
)

// Step describes one explicit action in setup order.
type Step struct {
	Kind         StepKind
	Label        Label
	ProjectName  string
	ProjectID    string
	Field        garden.ProjectField
	Instructions string
	RepositoryID garden.RepoID `json:"repository_id,omitempty"`
}

// Plan is a pure preview and carries no issue enrollment operation.
type Plan struct {
	Mode        Mode
	ImportCount int
	Steps       []Step
}

// BuildPlan previews owned label and project setup changes without writing.
func BuildPlan(observed Observed, mode Mode, desired Desired) (Plan, error) {
	if desired.ImportCount != 0 {
		return Plan{}, fmt.Errorf("import count must be zero")
	}
	p := Plan{Mode: mode, ImportCount: 0}
	if mode == Skip {
		return p, nil
	}
	if mode != Create && mode != ConnectExisting {
		return p, fmt.Errorf("invalid setup mode")
	}
	byName := map[string]Label{}
	for _, l := range observed.Labels {
		byName[strings.ToLower(l.Name)] = l
	}
	for _, d := range desired.Labels {
		if old, ok := byName[strings.ToLower(d.Name)]; ok {
			// A same-name desired label is always reused; visual metadata is preserved.
			p.Steps = append(p.Steps, Step{Kind: ReuseLabel, Label: old, Instructions: reuseNote(old, d)})
		} else {
			p.Steps = append(p.Steps, Step{Kind: CreateLabel, Label: d})
		}
	}
	managed := map[string]bool{}
	for _, d := range desired.Labels {
		managed[strings.ToLower(d.Name)] = true
	}
	for _, old := range observed.Labels {
		n := strings.ToLower(old.Name)
		if strings.HasPrefix(n, "bonsai:type/") || strings.HasPrefix(n, "bonsai:stage/") {
			if !managed[n] {
				p.Steps = append(p.Steps, Step{Kind: ConflictLabel, Label: old, Instructions: "An additional managed type/stage label scheme conflicts with the canonical labels."})
			}
		}
	}
	if mode == ConnectExisting {
		if desired.ExistingProject == nil {
			return Plan{}, ErrProjectNotFound
		}
		found := false
		for _, pr := range observed.Projects {
			if (desired.ExistingProject.ID != "" && pr.ID == desired.ExistingProject.ID) || (desired.ExistingProject.Number != "" && pr.Number == desired.ExistingProject.Number) {
				expectedOwner := desired.Owner
				if desired.ExistingProject.Owner != "" {
					expectedOwner = desired.ExistingProject.Owner
				}
				if desired.Owner != "" && desired.ExistingProject.Owner != "" && !strings.EqualFold(desired.Owner, desired.ExistingProject.Owner) {
					return Plan{}, ErrProjectOwner
				}
				if !strings.EqualFold(pr.Owner, expectedOwner) {
					return Plan{}, ErrProjectOwner
				}
				found = true
				for _, f := range desired.Fields {
					if !hasField(pr.Fields, f) {
						p.Steps = append(p.Steps, Step{Kind: EnsureProjectField, ProjectName: pr.ID, ProjectID: pr.ID, Field: f})
					}
				}
			}
		}
		if !found {
			return Plan{}, ErrProjectNotFound
		}
	}
	if mode == Create && desired.ProjectName != "" {
		found := false
		for _, pr := range observed.Projects {
			if strings.EqualFold(pr.Name, desired.ProjectName) {
				found = true
				p.Steps = append(p.Steps, Step{Kind: ConflictProject, ProjectName: desired.ProjectName, Instructions: "A same-name Project exists without verified operation ownership. Select it explicitly with ConnectExisting if it is yours."})
			}
		}
		if !found {
			p.Steps = append(p.Steps, Step{Kind: CreateProject, ProjectName: desired.ProjectName})
		}
		if !found {
			for _, f := range desired.Fields {
				p.Steps = append(p.Steps, Step{Kind: EnsureProjectField, ProjectName: desired.ProjectName, Field: f})
			}
		}
	}
	return p, p.Validate()
}

// BuildConnectionPlan observes Projects through the read port before validating ConnectExisting.
func BuildConnectionPlan(ctx context.Context, reader garden.ProjectReader, repo garden.RepoID, observed Observed, mode Mode, desired Desired) (Plan, error) {
	if mode == ConnectExisting {
		if reader == nil {
			return Plan{}, ErrProjectNotFound
		}
		projects, err := reader.ListProjects(ctx, repo)
		if err != nil {
			return Plan{}, safeError(err)
		}
		observed.Projects = projects
	}
	return BuildPlan(observed, mode, desired)
}

func hasField(fields []garden.ProjectField, want garden.ProjectField) bool {
	want = garden.CanonicalSetupField(want)
	for _, f := range fields {
		if strings.EqualFold(f.Name, want.Name) {
			return true
		}
	}
	return false
}
func reuseNote(old, want Label) string {
	if old.Color != want.Color || old.Description != want.Description {
		return "Existing label retained as-is; color and description differ from Bonsai defaults."
	}
	return "Existing label is compatible and retained as-is."
}

// Validate rejects any plan capable of destructive label or backlog changes.
func (p Plan) Validate() error {
	if p.ImportCount != 0 {
		return fmt.Errorf("plan imports issues")
	}
	seen := map[string]bool{}
	conflicts := []Step{}
	writes := []Step{}
	for _, s := range p.Steps {
		id := string(s.Kind) + ":" + strings.ToLower(s.Label.Name) + ":" + strings.ToLower(s.ProjectName) + ":" + strings.ToLower(s.ProjectID) + ":" + strings.ToLower(s.Field.Name)
		if seen[id] {
			return fmt.Errorf("duplicate plan step")
		}
		seen[id] = true
		if (s.Kind == CreateLabel || s.Kind == ReuseLabel || s.Kind == ConflictLabel) && s.Label.Name == "" {
			return fmt.Errorf("invalid label step")
		}
		if s.Kind == CreateProject && s.ProjectName == "" || s.Kind == EnsureProjectField && s.ProjectID == "" && s.ProjectName == "" {
			return fmt.Errorf("project step has no project")
		}
		if s.RepositoryID != 0 {
			return fmt.Errorf("plan step references another repository")
		}
		if s.Kind == ConflictProject {
			conflicts = append(conflicts, s)
		}
		if s.Kind == CreateProject || s.Kind == EnsureProjectField {
			writes = append(writes, s)
		}
		switch s.Kind {
		case CreateLabel, ReuseLabel, ConflictLabel, ConflictProject, CreateProject, EnsureProjectField, ManualStep:
		default:
			return fmt.Errorf("unsupported plan step")
		}
	}
	for _, c := range conflicts {
		if len(writes) > 0 {
			return fmt.Errorf("plan with project conflict cannot write Projects")
		}
		for _, w := range writes {
			if (c.ProjectName != "" && strings.EqualFold(c.ProjectName, w.ProjectName)) || (c.ProjectID != "" && c.ProjectID == w.ProjectID) || (c.ProjectID != "" && c.ProjectID == w.ProjectName) || (w.ProjectID != "" && w.ProjectID == c.ProjectName) {
				return fmt.Errorf("project conflict contradicts project write")
			}
		}
	}
	return nil
}

// Association is sanitized local state; it never contains credentials or agent identities.
type Association struct {
	LocalRepoID       garden.LocalRepoID `json:"local_repo_id"`
	GardenID          garden.GardenID    `json:"garden_id"`
	RemoteFullName    string             `json:"remote_full_name"`
	Project           ProjectRef         `json:"project"`
	Mode              Mode               `json:"mode"`
	Access            Access             `json:"access"`
	Identity          GitHubIdentity     `json:"github_identity"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	ActiveOperationID string             `json:"active_operation_id,omitempty"`
}

// OperationStatus reports durable progress after each setup action.
type OperationStatus string

const (
	Pending OperationStatus = "pending"
	Done    OperationStatus = "done"
	Failed  OperationStatus = "failed"
	Skipped OperationStatus = "skipped"
)

// OperationStep persists progress without retaining raw remote error text.
type OperationStep struct {
	ID        string          `json:"id"`
	Kind      StepKind        `json:"kind"`
	Status    OperationStatus `json:"status"`
	ErrorCode string          `json:"error_code,omitempty"`
	Attempted bool            `json:"attempted,omitempty"`
	ProjectID string          `json:"project_id,omitempty"`
	Nonce     string          `json:"nonce,omitempty"`
}

// Operation is the resumable record for one local setup attempt.
type Operation struct {
	ID           string             `json:"id"`
	LocalRepoID  garden.LocalRepoID `json:"local_repo_id"`
	PlanHash     string             `json:"plan_hash"`
	Plan         Plan               `json:"plan"`
	Steps        []OperationStep    `json:"steps"`
	Access       Access             `json:"access,omitempty"`
	RepositoryID garden.RepoID      `json:"repository_id,omitempty"`
}

func safeError(err error) error {
	switch {
	case errors.Is(err, ErrAuthRequired):
		return ErrAuthRequired
	case errors.Is(err, ErrRateLimited):
		return ErrRateLimited
	case errors.Is(err, ErrOffline):
		return ErrOffline
	case errors.Is(err, ErrPermissionDenied):
		return ErrPermissionDenied
	case errors.Is(err, ErrRepositoryIdentityChanged):
		return ErrRepositoryIdentityChanged
	case errors.Is(err, ErrGitHubValidation):
		return ErrGitHubValidation
	case errors.Is(err, garden.ErrGraphQL):
		return garden.ErrGraphQL
	default:
		var scope ErrScopeMissing
		if errors.As(err, &scope) {
			return scope
		}
		return fmt.Errorf("repository resolution failed")
	}
}
func planHash(p Plan) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
