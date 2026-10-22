// Package playgroundapi is the contract between the Playground screens and the
// backend that reads a session's branch, changes, pull request and CI. The TUI
// codes against these plain view types only, so it never touches Git, GitHub or
// the link store, and every UI test can run on the fake package.
//
// Four reads (Changes, FileDiff, PullRequest, Checks) fail and succeed on their
// own. Each reply carries a Stamp; the TUI keeps one Gate per call kind and
// drops any reply the Gate does not accept. A reply never says more than the
// backend verified: "none" and "missing" are verified absences, "unavailable"
// means the read failed and the previous answer should be kept as stale.
package playgroundapi

import (
	"context"
	"time"
)

// Limits are the bounds the backend enforces, exported so the UI can word
// "showing N of M" without guessing.
const (
	MaxFiles       = 500
	MaxCommits     = 50
	MaxDiffBytes   = 256 << 10
	MaxDiffLines   = 5000
	MaxChecks      = 200
	MaxPathBytes   = 4096
	MaxSessionIDLn = 128
)

// Backend is everything the Playground detail pane needs. Changes, FileDiff,
// PullRequest, Checks and Links only read. LinkSession is the one call that
// writes, and it writes only the local link record: it never creates an
// approved plan, attempt, approval or launch record and never edits an Issue.
//
// An error means the call did not run to a verdict (always an *Error, except
// context cancellation). A verdict that is a fact about the repository, such as
// a missing worktree, is a successful reply.
type Backend interface {
	// Changes resolves the session to a worktree of this repository and reads its
	// branch state, chosen base, changed files and commits ahead of the base.
	Changes(ctx context.Context, req ChangesRequest) (ChangesView, error)
	// FileDiff reads one file of the ChangesView it was listed in, split into hunks.
	// It returns CodeStale when the worktree or the list moved since that view.
	FileDiff(ctx context.Context, req FileDiffRequest) (FileDiffView, error)
	// PullRequest looks up the pull request of the session's branch.
	PullRequest(ctx context.Context, req PRRequest) (PRView, error)
	// Checks reads CI for a commit, independent of the pull request lookup.
	Checks(ctx context.Context, req ChecksRequest) (ChecksView, error)
	// LinkSession records an explicit, local session to Issue link.
	LinkSession(ctx context.Context, req LinkRequest) (LinkView, error)
	// Links lists the manual links of this clone.
	Links(ctx context.Context) (LinksView, error)
}

// Stamp scopes one reply. Generation grows with every call of the same kind and
// never repeats.
type Stamp struct {
	Scope      string // the local repository (clone) the reply is for
	Generation uint64
	FetchedAt  time.Time
}

// Gate accepts only the newest reply of one call kind. The zero Gate accepts
// the first reply. Use one Gate per call kind (and per selected session if the
// UI keeps several): the Gate knows nothing about sessions.
type Gate struct{ last Stamp }

// Accept reports whether a reply is newer than every reply accepted so far and
// is for the same clone. A rejected reply must be dropped unread.
func (g *Gate) Accept(s Stamp) bool {
	if s.Scope == "" {
		return false
	}
	if g.last.Scope != "" && (s.Scope != g.last.Scope || s.Generation <= g.last.Generation) {
		return false
	}
	g.last = s
	return true
}

// Last is the stamp of the newest accepted reply (zero before the first).
func (g *Gate) Last() Stamp { return g.last }

// Reset forgets every accepted reply, for example when the selection changes.
func (g *Gate) Reset() { g.last = Stamp{} }

// ---- Changes ----

type ChangesRequest struct{ SessionID string }

// WorktreeState is a verified fact about the session's worktree and branch.
type WorktreeState string

const (
	// WorktreeOK: the worktree exists and its branch resolves.
	WorktreeOK WorktreeState = "ok"
	// WorktreeMissing: the repository was read and the worktree directory is gone.
	WorktreeMissing WorktreeState = "worktree-missing"
	// BranchMissing: the worktree exists and its branch ref is verified gone.
	BranchMissing WorktreeState = "branch-missing"
	// WorktreeDetached: HEAD is not on a branch; the base, files and commits are
	// still read, a pull request cannot be looked up.
	WorktreeDetached WorktreeState = "detached"
	// WorktreeUnavailable: reading the worktree failed; nothing is known (Err is set).
	WorktreeUnavailable WorktreeState = "unavailable"
)

// BaseSource says where the chosen base came from. Nothing is ever guessed.
type BaseSource string

const (
	BaseAttempt  BaseSource = "attempt"  // the Garden launch attempt's recorded base
	BaseUpstream BaseSource = "upstream" // the branch's tracked upstream (not its own push target)
	BaseDefault  BaseSource = "default"  // the repository default branch
	BaseNone     BaseSource = "none"     // no base could be established
)

// Base is the commit the changes are measured from.
type Base struct {
	Source BaseSource
	Ref    string // human name such as "origin/main"; empty for BaseNone
	// MergeBase is the commit changes and commits are measured from: the merge
	// base of Ref and HEAD (for an attempt, of the attempt's recorded base commit
	// and HEAD). Empty for BaseNone.
	MergeBase string
	// Note is a fixed sentence explaining a fall-back or an absent base. May be empty.
	Note string
}

// Commit is one commit of the branch.
type Commit struct {
	SHA     string
	Short   string
	Subject string
	Author  string
	When    time.Time
}

// FileStatus is the kind of change against the base.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
	FileRenamed  FileStatus = "renamed"
)

// FileChange is one changed file. Path is display-safe (control bytes removed)
// and is the key FileDiff takes back.
type FileChange struct {
	Path    string
	OldPath string // set for FileRenamed
	Status  FileStatus
	// Binary: content changed but there is no text diff.
	Binary bool
	// Additions and Deletions are valid only when Counted. Untracked files and
	// binary files are not counted.
	Additions, Deletions int
	Counted              bool
	// Uncommitted: the working tree or index differs from HEAD for this file.
	Uncommitted bool
	// Untracked: the file is new and not yet added to Git.
	Untracked bool
}

// Totals count every changed file, also when the list is truncated.
type Totals struct {
	Files, Additions, Deletions int
	Binary                      int // files with no line counts
	Uncommitted                 int // files with uncommitted changes
}

// ChangesView is the read of one session's worktree. Files, Commits and Base
// fail independently: FilesErr or CommitsErr set means that part is unknown.
type ChangesView struct {
	Stamp     Stamp
	SessionID string

	State   WorktreeState
	Err     *Error // set with WorktreeUnavailable
	Branch  string // empty when detached or unavailable
	Head    Commit // HEAD of the worktree; zero when unknown
	Base    Base
	Task    TaskLink
	LinkErr *Error // the link store could not be read; never fails the rest

	Files      []FileChange
	FilesErr   *Error
	Totals     Totals
	FilesTrunc bool // Files holds only the first MaxFiles; Totals is still complete

	Commits      []Commit // newest first, at most MaxCommits
	CommitsTotal int
	CommitsTrunc bool
	CommitsErr   *Error
}

// TaskSource says how a session relates to a Garden task.
type TaskSource string

const (
	TaskNone   TaskSource = "none" // no link: not an error and not a failed session
	TaskLaunch TaskSource = "launch"
	TaskManual TaskSource = "manual"
)

// TaskLink is the session's task, if any. IssueNumber is 0 when unknown.
type TaskLink struct {
	Source      TaskSource
	IssueNumber int64
	IssueID     uint64
	AttemptID   string // set for TaskLaunch
	LinkedAt    time.Time
}

// ---- FileDiff ----

type FileDiffRequest struct {
	SessionID string
	Path      string // FileChange.Path from the ChangesView
	// Base and Generation are the ChangesView's Base.Ref and Stamp.Generation.
	Base       string
	Generation uint64
}

type DiffKind string

const (
	DiffAdd DiffKind = "add"
	DiffDel DiffKind = "del"
	DiffCtx DiffKind = "ctx"
)

// DiffLine is one display-safe line (no escapes or control bytes, tabs expanded).
// Old and New are 1-based line numbers, 0 where the line has none.
type DiffLine struct {
	Kind     DiffKind
	Text     string
	Old, New int
}

// Hunk is one "@@" block.
type Hunk struct {
	Header string
	Lines  []DiffLine
}

type TruncReason string

const (
	TruncNone  TruncReason = ""
	TruncBytes TruncReason = "bytes"
	TruncLines TruncReason = "lines"
)

type FileDiffView struct {
	Stamp     Stamp
	SessionID string
	Path      string
	OldPath   string
	Head      Commit // HEAD the diff was read at
	Binary    bool   // no text diff exists
	// Empty: Git reports no textual change (for example only the mode changed).
	Empty       bool
	Hunks       []Hunk
	Truncated   bool
	TruncReason TruncReason
	Lines       int // lines kept across Hunks
}

// ---- Pull request ----

type PRRequest struct{ SessionID string }

type PRState string

const (
	PRFound PRState = "found"
	// PRNone: a successful lookup returned no pull request for the branch head.
	PRNone PRState = "none"
	// PRUnavailable: the lookup did not complete; Code says why.
	PRUnavailable PRState = "unavailable"
)

type PRStatus string

const (
	PROpen   PRStatus = "open"
	PRClosed PRStatus = "closed"
	PRMerged PRStatus = "merged"
)

type Mergeable string

const (
	MergeUnknown     Mergeable = "unknown"
	MergeMergeable   Mergeable = "mergeable"
	MergeConflicting Mergeable = "conflicting"
)

type PRView struct {
	Stamp     Stamp
	SessionID string
	State     PRState
	Code      Code // set with PRUnavailable: a fixed code, never provider text
	Branch    string
	PR        *PullRequest // set with PRFound
	// DetailErr is set when the pull request was found but its detail (mergeable,
	// reviews) could not be read; PR then carries the list fields only.
	DetailErr *Error
	LocalHead string // the worktree's HEAD, for freshness against PR.HeadSHA
}

type PullRequest struct {
	Number    int
	Title     string
	Status    PRStatus
	Draft     bool
	URL       string
	Base      string
	Head      string // head branch
	HeadSHA   string
	Mergeable Mergeable
	// ReviewKnown is false while the detail is missing; the counts are then zero.
	ReviewKnown      bool
	Approvals        int
	ChangesRequested int
	RequestedReviews int
	// FromAttempt: the number came from the Garden attempt's linked PR (its head was still verified).
	FromAttempt bool
	// HeadMatchesLocal: HeadSHA equals the worktree's HEAD.
	HeadMatchesLocal bool
}

// ---- Checks ----

type ChecksRequest struct {
	SessionID string
	// HeadSHA is the commit to read. Empty means the worktree's HEAD.
	HeadSHA string
}

type ChecksState string

const (
	ChecksPending ChecksState = "pending"
	ChecksPassed  ChecksState = "passed"
	ChecksFailed  ChecksState = "failed"
	// ChecksNone: the read succeeded and GitHub reports no check for the commit.
	ChecksNone ChecksState = "none"
	// ChecksUnavailable: the read failed; Code says why. Never "passed".
	ChecksUnavailable ChecksState = "unavailable"
)

type Check struct {
	Name       string
	Status     string // GitHub: queued | in_progress | completed
	Conclusion string // GitHub conclusion, empty until completed
	URL        string
	Result     ChecksState // this check alone: pending | passed | failed
}

type ChecksCounts struct{ Pending, Passed, Failed int }

type ChecksView struct {
	Stamp     Stamp
	SessionID string
	State     ChecksState
	Code      Code // set with ChecksUnavailable
	// HeadSHA is the commit the checks belong to; LocalHead is the worktree's
	// HEAD, so the UI can show "checks for an older commit".
	HeadSHA   string
	LocalHead string
	IsLocal   bool // HeadSHA == LocalHead
	Checks    []Check
	Counts    ChecksCounts // over every check, also when Checks is truncated
	Truncated bool
}

// ---- Links ----

type LinkRequest struct {
	SessionID   string
	IssueNumber int64
	// Replace allows changing an existing manual link of the session to another Issue.
	Replace bool
}

// Link is one manual session to Issue link of this clone.
type Link struct {
	SessionID   string
	IssueNumber int64
	IssueID     uint64
	LinkedAt    time.Time
	Origin      string // "manual"
}

type LinkView struct {
	Stamp   Stamp
	Link    Link
	Created bool // false when the same link already existed
}

type LinksView struct {
	Stamp Stamp
	Links []Link // oldest first
}

// ---- Errors ----

// Code is a fixed, user-safe reason. No provider or Git text is ever copied.
type Code string

const (
	CodeInvalidInput      Code = "invalid-input"
	CodeSessionNotFound   Code = "session-not-found"
	CodeNotInRepository   Code = "not-in-repository"
	CodeUnavailable       Code = "unavailable"
	CodeStale             Code = "stale"
	CodeFileNotChanged    Code = "file-not-changed"
	CodeGardenUnavailable Code = "garden-unavailable"
	CodeIssueNotFound     Code = "issue-not-found"
	CodeHasTask           Code = "session-has-task"
	CodeLinkConflict      Code = "link-conflict"
	CodeStore             Code = "store-unavailable"
	CodeTimeout           Code = "timeout"
	CodeFailed            Code = "failed"

	// Codes of PRUnavailable and ChecksUnavailable.
	CodeNoGitHub       Code = "no-github"        // no GitHub service in this process
	CodeNoGitHubRemote Code = "no-github-remote" // no github.com remote to ask
	CodeNoBranch       Code = "no-branch"        // detached or missing branch
	CodeAuthRequired   Code = "auth-required"
	CodePermission     Code = "permission-denied"
	CodeRateLimited    Code = "rate-limited"
	CodeOffline        Code = "offline"
	CodeNotFound       Code = "not-found"
)

// Error is the only error the backend returns. Message is fixed text for the user.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }
