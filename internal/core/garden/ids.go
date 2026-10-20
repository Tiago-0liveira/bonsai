package garden

import (
	"fmt"
	"strconv"
	"strings"
)

// RepoID is GitHub's immutable numeric repository identity.
type RepoID uint64

// GardenID is the remote Garden identity, independent of any checkout path.
type GardenID struct{ Repo RepoID }

// IssueID is GitHub's stable numeric Issue identity.
type IssueID uint64

// LocalRepoID is a config.ProjectID and identifies one local execution root.
type LocalRepoID string

func ParseRepoID(s string) (RepoID, error) {
	n, e := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if e != nil || n == 0 {
		return 0, fmt.Errorf("%w: repository ID", ErrInvalidID)
	}
	return RepoID(n), nil
}
func NewGardenID(id RepoID) (GardenID, error) {
	if id == 0 {
		return GardenID{}, fmt.Errorf("%w: repository ID", ErrInvalidID)
	}
	return GardenID{Repo: id}, nil
}
func ParseIssueID(s string) (IssueID, error) {
	n, e := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if e != nil || n == 0 {
		return 0, fmt.Errorf("%w: issue ID", ErrInvalidID)
	}
	return IssueID(n), nil
}
func NewLocalRepoID(s string) (LocalRepoID, error) {
	if strings.TrimSpace(s) == "" || strings.TrimSpace(s) != s {
		return "", fmt.Errorf("%w: local repository ID", ErrInvalidID)
	}
	return LocalRepoID(s), nil
}
func (id RepoID) String() string      { return strconv.FormatUint(uint64(id), 10) }
func (id GardenID) String() string    { return id.Repo.String() }
func (id IssueID) String() string     { return strconv.FormatUint(uint64(id), 10) }
func (id LocalRepoID) String() string { return string(id) }
