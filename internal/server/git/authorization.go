package git

import (
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"net/http"
	"time"
)

type Repository struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspace_id"`
	FullName           string            `json:"full_name"`
	GitHubRepositoryID int64             `json:"github_repository_id"`
	InstallationID     int64             `json:"installation_id"`
	DefaultBranch      string            `json:"default_branch"`
	Members            map[string]string `json:"-"`
}
type Device struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	RepositoryIDs []string  `json:"repository_ids"`
	CreatedAt     time.Time `json:"created_at"`
	Revoked       bool      `json:"revoked"`
}

func (s *Service) authorized(user, repo string, write bool) bool {
	r, ok := s.Repositories[repo]
	if !ok || user == "" {
		return false
	}
	role := r.Members[user]
	return role == "write" || (!write && role == "read")
}
func (s *Service) principal(r *http.Request) (string, error) {
	user, e := s.Auth.Authenticate(r)
	if e != nil {
		return "", e
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != s.Origin {
		return "", domain.ErrForbidden
	}
	return user, nil
}
func (s *Service) device(token string) (Device, error) {
	var d Device
	var ok bool
	s.Store.View(func(data store.Data) error {
		d, ok = store.Get[Device](data, "devices", githubapp.Hash(token))
		return nil
	})
	if !ok || d.Revoked {
		return Device{}, domain.ErrAuth
	}
	return d, nil
}
func (d Device) has(repo string) bool {
	for _, id := range d.RepositoryIDs {
		if repo == id {
			return true
		}
	}
	return false
}
