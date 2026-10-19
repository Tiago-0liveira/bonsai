// Package sync builds a bounded, read-only Garden snapshot over observation ports.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Source describes whether a snapshot contains a live read or retained last-good data.
type Source string

const (
	// SourceLive marks a complete successful read.
	SourceLive Source = "live"
	// SourceStale marks retained last-good data after a failed or partial read.
	SourceStale Source = "stale"
)

// FindingCode identifies a detected synchronization condition.
type FindingCode string

const (
	// FindingStageConflict exposes ambiguous lifecycle labels.
	FindingStageConflict FindingCode = "stage-conflict"
	// FindingProjectionDrift exposes Project values that disagree with canonical Issue state.
	FindingProjectionDrift FindingCode = "projection-drift"
	// FindingStaleApproval exposes revoked or superseded approval records.
	FindingStaleApproval FindingCode = "stale-approval"
	// FindingDeleted retains a just-removed Issue for one accepted cycle.
	FindingDeleted FindingCode = "deleted"
	// FindingAccessRevoked keeps callers aware that last-good data lacks current authorization.
	FindingAccessRevoked FindingCode = "access-revoked"
	// FindingOffline distinguishes offline reads from other incomplete results.
	FindingOffline FindingCode = "offline"
	// FindingMalformed keeps invalid item data visible without poisoning the collection.
	FindingMalformed FindingCode = "malformed-item"
	// FindingProjectionUnavailable explains why no Project drift conclusion was possible.
	FindingProjectionUnavailable FindingCode = "projection-unavailable"
)

// Severity lets clients order findings without interpreting their messages.
type Severity string

const (
	// SeverityInfo describes an informational condition.
	SeverityInfo Severity = "info"
	// SeverityWarning describes a condition needing user attention.
	SeverityWarning Severity = "warning"
	// SeverityError describes an authorization or integrity failure.
	SeverityError Severity = "error"
)

// Finding is a safe, user-facing detection result without adapter details or credentials.
type Finding struct {
	// Code gives clients a stable machine-readable category.
	Code FindingCode
	// Severity lets clients order the finding.
	Severity Severity
	// IssueID is zero for Garden-wide findings.
	IssueID garden.IssueID
	// Message is safe to show without exposing adapter details.
	Message string
}

// Problem records an item-local decode or verification issue.
type Problem struct {
	// Code identifies the stable local problem category.
	Code string
	// Message is a safe explanation for the user.
	Message string
}

// Item is the verified read projection for one GitHub Issue.
type Item struct {
	// Issue is the observed canonical Issue record.
	Issue garden.Issue
	// Classification is derived from managed labels.
	Classification garden.Classification
	// Lane is the user-facing placement derived from verified state.
	Lane garden.LaneResult
	// Verification contains revision and approval integrity results.
	Verification garden.Verification
	// Problems keeps item-local decode and verification failures visible.
	Problems []Problem
	// Revision fingerprints the observed Issue projection.
	Revision string
	// Deleted marks an Issue retained for one accepted cycle after disappearance.
	Deleted bool
}

// Snapshot is one generation of a Garden read, including explicit partiality.
type Snapshot struct {
	// GardenID identifies the remote repository shared by clones.
	GardenID garden.GardenID
	// LocalRepoID identifies the local association receiving this snapshot.
	LocalRepoID garden.LocalRepoID
	// Generation orders requests within the association epoch.
	Generation uint64
	// Epoch invalidates reads started before reassociation.
	Epoch uint64
	// FetchedAt records when this read completed or failed.
	FetchedAt time.Time
	// Complete distinguishes the whole collection from a partial read.
	Complete bool
	// Source marks live results or retained stale data.
	Source Source
	// Items contains managed and explicitly imported Issues.
	Items []Item
	// Problems contains collection-level read failures.
	Problems []Problem
	// Findings contains typed detection and projection results.
	Findings []Finding
	// StaleReason explains why last-good data is being retained.
	StaleReason string
	// StaleSince records when the current stale period began.
	StaleSince time.Time
	// BackoffHint advises a caller without starting a retry timer here.
	BackoffHint time.Duration
	// AccessRevoked indicates that an authorization failure retained old data.
	AccessRevoked bool
}

// Limits bound every remote collection read and each adapter call.
type Limits struct {
	// PageCap bounds remote pagination calls.
	PageCap int
	// ItemCap bounds the returned Issue collection.
	ItemCap int
	// CallTimeout bounds each reader call.
	CallTimeout time.Duration
}

// Request contains only caller policy and read capabilities; it cannot mutate GitHub.
type Request struct {
	// GardenID selects the remote repository to read.
	GardenID garden.GardenID
	// LocalRepoID scopes this result to one clone or worktree.
	LocalRepoID garden.LocalRepoID
	// Generation orders this request against earlier requests.
	Generation uint64
	// Epoch binds the result to the current local association.
	Epoch uint64
	// Issues is the read-only Issue capability used for collection and verification reads.
	Issues garden.IssueReader
	// Projects is an optional read-only source for Project field projections.
	Projects garden.ProjectReader
	// ProjectID selects the associated Project whose item values are checked.
	ProjectID string
	// StageField names the Project field that projects Issue lifecycle stage.
	StageField string
	// TypeField names the Project field that projects Issue type.
	TypeField string
	// ExplicitImports opts individual unmanaged Issue IDs into this Garden.
	ExplicitImports map[garden.IssueID]struct{}
	// ApprovalPolicy supplies the stable actors trusted by verification.
	ApprovalPolicy garden.ApprovalPolicy
	// Limits bounds pagination, items, and each remote call.
	Limits Limits
	// Now injects a clock for deterministic snapshots; nil uses the system clock.
	Now func() time.Time
}

// Load reads a bounded collection and returns partial data explicitly marked incomplete.
func Load(ctx context.Context, r Request) Snapshot {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	s := Snapshot{GardenID: r.GardenID, LocalRepoID: r.LocalRepoID, Generation: r.Generation, Epoch: r.Epoch, FetchedAt: now().UTC(), Source: SourceLive}
	if r.GardenID.Repo == 0 || r.Issues == nil || r.Limits.PageCap <= 0 || r.Limits.ItemCap <= 0 || r.Limits.CallTimeout <= 0 {
		s.Problems = append(s.Problems, Problem{Code: "invalid-request", Message: "Garden read limits or identity are invalid"})
		return s
	}
	seen := map[garden.IssueID]bool{}
	items := []garden.Issue{}
	cursor := ""
	pages := 0
	restart := false
	for {
		if err := ctx.Err(); err != nil {
			s.Problems = append(s.Problems, Problem{Code: "cancelled", Message: "Garden read was cancelled"})
			break
		}
		if pages >= r.Limits.PageCap {
			s.Problems = append(s.Problems, Problem{Code: "page-cap", Message: "Garden page limit reached"})
			break
		}
		call, cancel := context.WithTimeout(ctx, r.Limits.CallTimeout)
		page, err := r.Issues.ListManagedIssues(call, r.GardenID.Repo, cursor)
		cancel()
		if errors.Is(err, garden.ErrInvalidCursor) && !restart {
			restart = true
			cursor = ""
			pages = 0
			items = nil
			seen = map[garden.IssueID]bool{}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				s.Problems = append(s.Problems, Problem{Code: "cancelled", Message: "Garden read was cancelled"})
				break
			}
			s.Problems = append(s.Problems, Problem{Code: safeCode(err), Message: "Garden issues could not be read"})
			break
		}
		pages++
		for _, it := range page.Items {
			if !seen[it.ID] {
				seen[it.ID] = true
				items = append(items, it)
				if len(items) >= r.Limits.ItemCap {
					break
				}
			}
		}
		if len(items) >= r.Limits.ItemCap && page.NextCursor != "" {
			s.Problems = append(s.Problems, Problem{Code: "item-cap", Message: "Garden item limit reached"})
			break
		}
		if page.Complete || page.NextCursor == "" {
			s.Complete = true
			break
		}
		cursor = page.NextCursor
	}
	if !s.Complete {
		s.Source = SourceStale
	}
	for id := range r.ExplicitImports {
		if seen[id] || len(items) >= r.Limits.ItemCap {
			continue
		}
		call, cancel := context.WithTimeout(ctx, r.Limits.CallTimeout)
		it, err := r.Issues.GetIssue(call, r.GardenID.Repo, id)
		cancel()
		if err != nil {
			s.Complete = false
			s.Source = SourceStale
			s.Problems = append(s.Problems, Problem{Code: safeCode(err), Message: "An explicitly imported issue could not be read"})
			continue
		}
		seen[id] = true
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, it := range items {
		item := buildItem(ctx, r, it)
		if !item.Verification.Complete {
			s.Complete = false
			s.Source = SourceStale
		}
		for _, problem := range item.Problems {
			if problem.Code == "permission-denied" || problem.Code == "offline" || problem.Code == "read-failed" || problem.Code == "cancelled" {
				s.Problems = append(s.Problems, problem)
			}
		}
		s.Items = append(s.Items, item)
		s.Findings = append(s.Findings, itemFindings(item)...)
	}
	if r.ProjectID != "" {
		loadProjectProjection(ctx, &s, r)
	}
	if hasProblem(s.Problems, "offline") {
		s.Findings = append(s.Findings, Finding{Code: FindingOffline, Severity: SeverityWarning, Message: "Garden data may be out of date"})
	}
	return s
}

func loadProjectProjection(ctx context.Context, s *Snapshot, r Request) {
	if r.Projects == nil || r.StageField == "" || r.TypeField == "" {
		s.Findings = append(s.Findings, Finding{Code: FindingProjectionUnavailable, Severity: SeverityInfo, Message: "Project field projection is unavailable"})
		return
	}
	call, cancel := context.WithTimeout(ctx, r.Limits.CallTimeout)
	projects, err := r.Projects.ListProjects(call, r.GardenID.Repo)
	cancel()
	if err != nil {
		s.Findings = append(s.Findings, Finding{Code: FindingProjectionUnavailable, Severity: SeverityInfo, Message: "Project field projection could not be read"})
		return
	}
	var selected *garden.Project
	for i := range projects {
		if projects[i].ID == r.ProjectID {
			selected = &projects[i]
			break
		}
	}
	if selected == nil || selected.IssueFields == nil {
		s.Findings = append(s.Findings, Finding{Code: FindingProjectionUnavailable, Severity: SeverityInfo, Message: "Project item values are unavailable"})
		return
	}
	for i := range s.Items {
		item := &s.Items[i]
		values, ok := selected.IssueFields[item.Issue.ID]
		if !ok {
			s.Findings = append(s.Findings, Finding{Code: FindingProjectionDrift, Severity: SeverityWarning, IssueID: item.Issue.ID, Message: "Project projection drift: Issue is missing from the Project view"})
			continue
		}
		stage, stageOK := values[r.StageField]
		typeName, typeOK := values[r.TypeField]
		if !stageOK || !typeOK || stage != string(item.Classification.Stage) || typeName != string(item.Classification.Type) {
			f := Finding{Code: FindingProjectionDrift, Severity: SeverityWarning, IssueID: item.Issue.ID, Message: "Project projection drift: fields differ from verified Garden state"}
			s.Findings = append(s.Findings, f)
		}
	}
}

func hasProblem(problems []Problem, code string) bool {
	for _, p := range problems {
		if p.Code == code {
			return true
		}
	}
	return false
}

func buildItem(ctx context.Context, r Request, it garden.Issue) Item {
	c := garden.Classify(it.Labels)
	m, me := garden.DecodeMetadata(it.Metadata)
	comments := []garden.IssueComment{}
	var err error
	call, cancel := context.WithTimeout(ctx, r.Limits.CallTimeout)
	comments, err = r.Issues.ListComments(call, r.GardenID.Repo, it.ID)
	cancel()
	conv := make([]garden.Comment, 0, len(comments))
	for _, x := range comments {
		conv = append(conv, garden.Comment{ID: x.ID, Author: x.Author, AuthorID: x.AuthorID, Body: x.Body, CreatedAt: x.CreatedAt})
	}
	v := garden.Verify(garden.IssueIdentity{Repo: r.GardenID.Repo, Issue: it.ID}, conv, r.ApprovalPolicy, garden.VerifyOptions{Complete: err == nil, Revocations: m.Revocations})
	lane := garden.Lane(c, m, v, me)
	out := Item{Issue: it, Classification: c, Lane: lane, Verification: v, Revision: issueRevision(it)}
	for _, p := range c.Problems {
		out.Problems = append(out.Problems, Problem{Code: string(p), Message: "Issue labels do not match the Garden schema"})
	}
	if me != nil {
		out.Problems = append(out.Problems, Problem{Code: "metadata", Message: "Issue metadata is invalid or uses an unsupported schema"})
	}
	if err != nil {
		code := safeCode(err)
		if ctx.Err() != nil {
			code = "cancelled"
		}
		out.Problems = append(out.Problems, Problem{Code: code, Message: "Issue verification comments could not be read"})
	}
	for _, p := range v.Problems {
		out.Problems = append(out.Problems, Problem{Code: p.Code, Message: "Issue revision or approval needs attention"})
	}
	if len(out.Problems) > 0 {
		out.Lane = garden.LaneResult{Lane: garden.LaneAttention, Reason: "Issue has invalid Garden data"}
	}
	return out
}

func issueRevision(i garden.Issue) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:%s:%s:%x", i.ID, i.NodeID, i.Body, i.Metadata)
	labels := append([]string(nil), i.Labels...)
	sort.Strings(labels)
	for _, l := range labels {
		fmt.Fprintf(h, "\x00%s", l)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func safeCode(err error) string {
	if errors.Is(err, garden.ErrPermissionDenied) {
		return "permission-denied"
	}
	if errors.Is(err, garden.ErrOffline) {
		return "offline"
	}
	return "read-failed"
}
func itemFindings(i Item) []Finding {
	out := []Finding{}
	add := func(code FindingCode, msg string) {
		out = append(out, Finding{Code: code, Severity: SeverityWarning, IssueID: i.Issue.ID, Message: msg})
	}
	if len(i.Classification.Problems) > 0 {
		add(FindingStageConflict, "Issue labels contain conflicting Garden stages or types")
	}
	if i.Lane.Lane == garden.LaneAttention {
		add(FindingMalformed, i.Lane.Reason)
	}
	if i.Lane.Reason == "accepted specification metadata drift" || i.Lane.Reason == "accepted specification head changed" || i.Lane.Reason == "accepted specification has no valid head" {
		add(FindingProjectionDrift, "Issue labels or metadata do not match verified Garden state")
	}
	if len(i.Verification.RevokedApprovals) > 0 || len(i.Verification.SupersededApprovals) > 0 {
		add(FindingStaleApproval, "An approval is revoked or no longer matches the current specification")
	}
	return out
}

// Store orders results for one local association and keeps the last complete read on failure.
type Store struct {
	mu      sync.RWMutex
	garden  garden.GardenID
	local   garden.LocalRepoID
	epoch   uint64
	current Snapshot
	dropped uint64
}

// NewStore creates an isolated read model for one local repository association.
func NewStore(id garden.GardenID, local garden.LocalRepoID) *Store {
	return &Store{garden: id, local: local, epoch: 1}
}

// Apply accepts only newer results from the current Garden association.
func (s *Store) Apply(result Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.GardenID != s.garden {
		return ErrForeignGarden
	}
	if result.Epoch != s.epoch {
		s.dropped++
		return nil
	}
	if result.Generation <= s.current.Generation {
		s.dropped++
		return nil
	}
	if result.Complete && result.Source == SourceLive {
		next := clone(result)
		next.Items = markDeleted(s.current.Items, next.Items, true)
		for _, item := range next.Items {
			if item.Deleted {
				next.Findings = append(next.Findings, Finding{Code: FindingDeleted, Severity: SeverityInfo, IssueID: item.Issue.ID, Message: "Issue was deleted or removed from the Garden"})
			}
		}
		s.current = next
		s.current.LocalRepoID = s.local
		return nil
	}
	last := s.current
	last.GardenID = s.garden
	last.LocalRepoID = s.local
	last.Generation = result.Generation
	last.FetchedAt = result.FetchedAt
	last.Complete = false
	last.Source = SourceStale
	last.StaleReason = "Garden read was incomplete"
	if last.StaleSince.IsZero() {
		last.StaleSince = result.FetchedAt
	}
	last.Problems = append([]Problem(nil), result.Problems...)
	last.Findings = append([]Finding(nil), result.Findings...)
	last.AccessRevoked = false
	for _, p := range result.Problems {
		if p.Code == "permission-denied" {
			last.AccessRevoked = true
			last.StaleReason = "Garden access was revoked"
			last.Findings = append(last.Findings, Finding{Code: FindingAccessRevoked, Severity: SeverityError, Message: "Access to the Garden was revoked"})
		}
		if p.Code == "read-failed" || p.Code == "offline" {
			last.BackoffHint = 30 * time.Second
		}
	}
	last.Items = markDeleted(last.Items, result.Items, result.Complete)
	for _, item := range last.Items {
		if item.Deleted {
			last.Findings = append(last.Findings, Finding{Code: FindingDeleted, Severity: SeverityInfo, IssueID: item.Issue.ID, Message: "Issue was deleted or removed from the Garden"})
		}
	}
	s.current = last
	return nil
}

// Read returns a defensive copy of the accepted snapshot.
func (s *Store) Read() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return clone(s.current) }

// Reassociate bumps the epoch and discards data when the remote Garden changes.
func (s *Store) Reassociate(id garden.GardenID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	if id != s.garden {
		s.garden = id
		s.current = Snapshot{}
		s.current.GardenID = id
		s.current.LocalRepoID = s.local
	}
}

// Epoch reports the association epoch used to invalidate in-flight generations.
func (s *Store) Epoch() uint64 { s.mu.RLock(); defer s.mu.RUnlock(); return s.epoch }

// Dropped reports the number of out-of-order results ignored by this store.
func (s *Store) Dropped() uint64 { s.mu.RLock(); defer s.mu.RUnlock(); return s.dropped }

// LocalKey scopes local execution coordination to one checkout identity.
func LocalKey(local garden.LocalRepoID) string { return "garden-local:" + local.String() }

func markDeleted(old, next []Item, complete bool) []Item {
	if !complete {
		return old
	}
	present := map[garden.IssueID]bool{}
	for _, i := range next {
		present[i.Issue.ID] = true
	}
	out := []Item{}
	for _, i := range old {
		if !present[i.Issue.ID] && !i.Deleted {
			i.Deleted = true
			i.Problems = append(i.Problems, Problem{Code: "deleted", Message: "Issue is no longer present in the Garden"})
			out = append(out, i)
		}
	}
	out = append(out, next...)
	return out
}
func clone(s Snapshot) Snapshot {
	s.Items = append([]Item(nil), s.Items...)
	for i := range s.Items {
		s.Items[i].Issue.Labels = append([]string(nil), s.Items[i].Issue.Labels...)
		s.Items[i].Issue.Metadata = append([]byte(nil), s.Items[i].Issue.Metadata...)
		s.Items[i].Problems = append([]Problem(nil), s.Items[i].Problems...)
	}
	s.Problems = append([]Problem(nil), s.Problems...)
	s.Findings = append([]Finding(nil), s.Findings...)
	return s
}

// AssociationState is the persisted setup category supplied to status projection.
type AssociationState string

const (
	// AssociationNone means no durable setup choice is recorded.
	AssociationNone AssociationState = "none"
	// AssociationSkipped records an explicit opt-out choice.
	AssociationSkipped AssociationState = "skipped"
	// AssociationReadOnly means the Garden is readable without mutation capability.
	AssociationReadOnly AssociationState = "read-only"
	// AssociationConnected means setup is complete and reads are available.
	AssociationConnected AssociationState = "connected"
	// AssociationPartialSetup means a durable setup operation remains incomplete.
	AssociationPartialSetup AssociationState = "partial-setup"
	// AssociationAwaitingManual means setup requires a user action outside the adapter.
	AssociationAwaitingManual AssociationState = "awaiting-manual"
	// AssociationOperationInProgress marks an active setup operation.
	AssociationOperationInProgress AssociationState = "operation-in-progress"
)

// AssociationView is the read-only status input copied from the association and operation records.
type AssociationView struct {
	// State is copied from the durable association and operation records.
	State AssociationState
	// OperationID identifies the active operation when one exists.
	OperationID string
	// OperationDone and OperationTotal summarize durable operation progress.
	OperationDone, OperationTotal int
}

// ConnectionStatus is a UI-ready read-only summary of setup and sync state.
type ConnectionStatus struct {
	// State is the derived connection and sync status label.
	State string
	// IssueCount counts retained snapshot items, including a deletion marker.
	IssueCount int
	// AttentionCount counts items requiring review.
	AttentionCount int
	// LastSync is the snapshot fetch time when known.
	LastSync time.Time
	// OperationID identifies active setup progress.
	OperationID string
	// OperationDone counts completed operation steps.
	OperationDone int
	// OperationTotal counts all operation steps.
	OperationTotal int
}

// Status derives connection presentation from an association view and current snapshot.
func Status(a AssociationView, snapshot Snapshot) ConnectionStatus {
	out := ConnectionStatus{IssueCount: len(snapshot.Items), LastSync: snapshot.FetchedAt, OperationID: a.OperationID, OperationDone: a.OperationDone, OperationTotal: a.OperationTotal}
	for _, i := range snapshot.Items {
		if i.Lane.Lane == garden.LaneAttention || i.Deleted {
			out.AttentionCount++
		}
	}
	switch {
	case snapshot.AccessRevoked:
		out.State = "access-revoked"
	case snapshot.Source == SourceStale && snapshot.BackoffHint > 0:
		out.State = "offline"
	case a.State == AssociationNone:
		out.State = "none"
	case a.State == AssociationSkipped:
		out.State = "skipped"
	case a.State == AssociationReadOnly:
		out.State = "read-only"
	case a.State == AssociationPartialSetup:
		out.State = "partial-setup"
	case a.State == AssociationAwaitingManual:
		out.State = "awaiting-manual"
	case a.State == AssociationOperationInProgress:
		out.State = "operation-in-progress"
	case a.State == AssociationConnected && snapshot.Source == SourceStale:
		out.State = "stale"
	case a.State == AssociationConnected && !snapshot.Complete:
		out.State = "partial"
	case a.State == AssociationConnected:
		out.State = "connected"
	default:
		out.State = "none"
	}
	return out
}
