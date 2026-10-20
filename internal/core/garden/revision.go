package garden

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// RevisionKind identifies the content addressed by a revision record.
type RevisionKind string

const (
	// RevisionSpec identifies a specification revision.
	RevisionSpec RevisionKind = "spec"
	// RevisionPlan identifies an implementation plan revision.
	RevisionPlan RevisionKind = "plan"
)

const MaxComments = 2000
const MaxCommentBodyBytes = MaxMetadataBytes + (1 << 20)

// CommentKind identifies the security meaning of an envelope comment.
type CommentKind string

const (
	// CommentRevision marks an append-only content revision.
	CommentRevision CommentKind = "revision"
	// CommentApproval marks approval of a revision hash and source fingerprint.
	CommentApproval CommentKind = "approval"
	// CommentRevocation invalidates an earlier approval comment.
	CommentRevocation CommentKind = "revocation"
)

// IssueIdentity binds verification to one immutable GitHub Issue.
type IssueIdentity struct {
	Repo  RepoID
	Issue IssueID
}

// Revision is a verified content revision recovered from the comment log.
type Revision struct {
	Version               uint64
	Kind                  RevisionKind
	Content               string
	Hash                  string
	SourceSpecFingerprint string
	CommentID             int64
	Parents               []string
	// Author, AuthorID and CreatedAt come from the revision's comment: display data for "updated by".
	Author    string
	AuthorID  string
	CreatedAt time.Time
}

// Approval is a verified approval record recovered from the comment log.
type Approval struct {
	RevisionHash          string
	SourceSpecFingerprint string
	CommentID             int64
	Author                string
	AuthorID              string
	Revoked               bool
	Superseded            bool
}

// Comment is an ordered server comment snapshot used to derive verification state.
type Comment struct {
	ID        int64
	Author    string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// ApprovalPolicy supplies actors trusted at verification time.
// ApprovalPolicy contains stable GitHub user IDs; logins are mutable display labels.
type ApprovalPolicy struct {
	AllowedActorIDs []string
	// ForbidSelfApproval blocks an approver from approving their own revision when explicitly enabled.
	ForbidSelfApproval bool
}

// CommentEnvelope is the strict versioned payload embedded in the first comment-body line.
type CommentEnvelope struct {
	SchemaVersion         int          `json:"schema_version"`
	Kind                  CommentKind  `json:"kind"`
	RepoID                RepoID       `json:"repo_id"`
	IssueID               IssueID      `json:"issue_id"`
	RevisionKind          RevisionKind `json:"revision_kind,omitempty"`
	ContentHash           string       `json:"content_hash,omitempty"`
	Parents               []string     `json:"parents,omitempty"`
	SourceSpecFingerprint string       `json:"source_spec_fingerprint,omitempty"`
	RevisionHash          string       `json:"revision_hash,omitempty"`
	RevisionCommentID     int64        `json:"revision_comment_id,omitempty"`
	TargetCommentID       int64        `json:"target_comment_id,omitempty"`
	RecordHash            string       `json:"record_hash,omitempty"`
}

// VerificationProblem records a comment-log inconsistency without hiding other valid records.
type VerificationProblem struct {
	CommentID int64
	Code      string
	Err       error
}

// Verification is the complete derived approval and revision state for one Issue.
type Verification struct {
	Complete               bool
	Revisions              []Revision
	Heads                  []Revision
	ConflictingHeads       []Revision
	Approvals              []Approval
	ValidApprovals         []Approval
	RevokedApprovals       []Approval
	SupersededApprovals    []Approval
	Problems               []VerificationProblem
	AcceptedSpecHash       string
	CurrentSpecFingerprint string
}

// VerifyOptions carries the completeness and persisted revocation projection for one snapshot.
type VerifyOptions struct {
	Complete    bool
	Revocations []RevocationRecord
}

// NormalizeContent canonicalizes CRLF and CR to LF, removes only trailing ASCII spaces/tabs per line, then removes leading/trailing LF bytes. Unicode whitespace and Unicode normalization are preserved, so NFC and NFD hash differently.
func NormalizeContent(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// ContentHash returns the lowercase SHA-256 of normalized content.
func ContentHash(s string) string {
	h := sha256.Sum256([]byte(NormalizeContent(s)))
	return hex.EncodeToString(h[:])
}

// EncodeCommentEnvelope creates the canonical first line plus optional human-readable body.
func EncodeCommentEnvelope(e CommentEnvelope, human string) (string, error) {
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	if e.Kind == CommentApproval || e.Kind == CommentRevocation {
		e.RecordHash = ContentHash(human)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidRevision, err)
	}
	if len(b) > MaxMetadataBytes {
		return "", fmt.Errorf("%w: envelope too large", ErrInvalidRevision)
	}
	return "<!-- bonsai:garden/v1 " + string(b) + " -->\n" + human, nil
}

// NewSpecRevision renders the comment body that appends a specification revision on top of parents (the hashes of
// the heads it supersedes, none for the first revision). The caller posts it; nothing here writes.
func NewSpecRevision(id IssueIdentity, content string, parents []string) (string, error) {
	if id.Repo == 0 || id.Issue == 0 {
		return "", fmt.Errorf("%w: identity", ErrInvalidRevision)
	}
	text := NormalizeContent(content)
	if text == "" {
		return "", fmt.Errorf("%w: empty specification", ErrInvalidRevision)
	}
	return EncodeCommentEnvelope(CommentEnvelope{Kind: CommentRevision, RepoID: id.Repo, IssueID: id.Issue, RevisionKind: RevisionSpec,
		ContentHash: ContentHash(text), Parents: append([]string(nil), parents...)}, text)
}

func decodeEnvelope(body string) (CommentEnvelope, string, error) {
	var e CommentEnvelope
	line, rest, _ := strings.Cut(body, "\n")
	const prefix, suffix = "<!-- bonsai:garden/v1 ", " -->"
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return e, "", fmt.Errorf("malformed envelope")
	}
	raw := []byte(strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix))
	if len(raw) > MaxMetadataBytes || !utf8.Valid(raw) || rejectDuplicateKeys(raw) != nil {
		return e, "", fmt.Errorf("malformed envelope")
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return e, "", fmt.Errorf("malformed envelope")
	}
	if head.SchemaVersion != SchemaVersion {
		return e, "", fmt.Errorf("%w: %d", ErrUnsupportedSchema, head.SchemaVersion)
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, "", fmt.Errorf("malformed envelope")
	}
	if e.RepoID == 0 || e.IssueID == 0 {
		return e, "", fmt.Errorf("malformed identity")
	}
	return e, rest, nil
}

// Verify derives revisions, heads, approvals, revocations and problems from an ordered server comment log.
func Verify(identity IssueIdentity, comments []Comment, policy ApprovalPolicy, options ...VerifyOptions) Verification {
	opt := VerifyOptions{Complete: true}
	if len(options) > 0 {
		opt = options[0]
	}
	v := Verification{Complete: opt.Complete}
	if len(comments) > MaxComments {
		v.Problems = append(v.Problems, VerificationProblem{Code: "too-many-comments", Err: ErrTooManyComments})
		return v
	}
	expectedRevocations := map[int64]int64{}
	if len(opt.Revocations) > MaxComments {
		v.Problems = append(v.Problems, VerificationProblem{Code: "too-many-revocations", Err: ErrInvalidMetadata})
		return v
	}
	for _, x := range opt.Revocations {
		expectedRevocations[x.RevocationCommentID] = x.ApprovalCommentID
	}
	problem := func(c Comment, code string, err error) {
		v.Problems = append(v.Problems, VerificationProblem{c.ID, code, err})
	}
	// Work from a private, deterministic snapshot so provider pagination order cannot affect trust.
	ordered := append([]Comment(nil), comments...)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	byID := map[int64]Comment{}
	duplicate := map[int64]bool{}
	unique := make([]Comment, 0, len(ordered))
	for _, c := range ordered {
		if len(c.Body) > MaxCommentBodyBytes {
			problem(c, "comment-too-large", ErrInvalidRevision)
			continue
		}
		if previous, ok := byID[c.ID]; ok {
			if previous != c {
				duplicate[c.ID] = true
			}
			continue
		}
		byID[c.ID] = c
		unique = append(unique, c)
	}
	duplicateIDs := make([]int64, 0, len(duplicate))
	for id := range duplicate {
		duplicateIDs = append(duplicateIDs, id)
	}
	sort.Slice(duplicateIDs, func(i, j int) bool { return duplicateIDs[i] < duplicateIDs[j] })
	for _, id := range duplicateIDs {
		v.Problems = append(v.Problems, VerificationProblem{id, "duplicate-comment-id", ErrInvalidRevision})
	}
	ordered = ordered[:0]
	for _, c := range unique {
		if !duplicate[c.ID] {
			ordered = append(ordered, c)
		}
	}
	allowed := map[string]bool{}
	for _, a := range policy.AllowedActorIDs {
		allowed[a] = true
	}
	type entry struct {
		c    Comment
		e    CommentEnvelope
		text string
	}
	entries := make([]entry, 0, len(ordered))
	revByHash := map[string]Revision{}
	revByID := map[int64]Revision{}
	revisionAuthors := map[string]string{}
	commentOrder := map[int64]int{}
	for i, c := range ordered {
		commentOrder[c.ID] = i
	}
	for _, c := range ordered {
		e, text, err := decodeEnvelope(c.Body)
		if err != nil {
			if strings.Contains(err.Error(), ErrUnsupportedSchema.Error()) {
				problem(c, "unsupported-schema", err)
			} else {
				problem(c, "malformed-envelope", err)
			}
			continue
		}
		if e.RepoID != identity.Repo || e.IssueID != identity.Issue {
			problem(c, "wrong-issue-repo", fmt.Errorf("%w: envelope identity mismatch", ErrInvalidRevision))
			continue
		}
		if !validEnvelopeFields(e) {
			cause := error(ErrInvalidRevision)
			if e.Kind == CommentApproval {
				cause = ErrInvalidApproval
			}
			problem(c, "malformed-envelope", cause)
			continue
		}
		if e.Kind != CommentRevision && (e.RecordHash == "" || e.RecordHash != ContentHash(text)) {
			problem(c, "edited-hash-mismatch", ErrInvalidApproval)
			continue
		}
		entries = append(entries, entry{c, e, text})
		if e.Kind != CommentRevision {
			continue
		}
		if e.RevisionKind != RevisionSpec && e.RevisionKind != RevisionPlan {
			problem(c, "malformed-envelope", ErrInvalidRevision)
			continue
		}
		if e.ContentHash != ContentHash(text) {
			problem(c, "edited-hash-mismatch", ErrInvalidRevision)
			continue
		}
		if e.RevisionKind == RevisionPlan && e.SourceSpecFingerprint == "" {
			problem(c, "malformed-envelope", ErrInvalidRevision)
			continue
		}
		fingerprint := e.SourceSpecFingerprint
		if e.RevisionKind == RevisionSpec && fingerprint == "" {
			fingerprint = e.ContentHash
		}
		r := Revision{Version: 1, Kind: e.RevisionKind, Content: text, Hash: e.ContentHash, SourceSpecFingerprint: fingerprint, CommentID: c.ID, Parents: append([]string(nil), e.Parents...), Author: c.Author, AuthorID: c.AuthorID, CreatedAt: c.CreatedAt}
		v.Revisions = append(v.Revisions, r)
		revByHash[r.Hash] = r
		revByID[r.CommentID] = r
		revisionAuthors[r.Hash] = c.AuthorID
	}
	children := map[string]bool{}
	for _, r := range v.Revisions {
		for _, p := range r.Parents {
			if parent, ok := revByHash[p]; ok && parent.Kind == r.Kind {
				children[p] = true
			}
		}
	}
	for _, r := range v.Revisions {
		for _, p := range r.Parents {
			_, ok := revByHash[p]
			if p == r.Hash {
				problem(byID[r.CommentID], "self-parent", ErrInvalidRevision)
			}
			if !ok {
				problem(byID[r.CommentID], "deleted-referenced-comment", ErrInvalidRevision)
			}
		}
	}
	// Parent links are hash-addressed; DFS rejects every node participating in a cycle.
	state, cyclic := map[string]uint8{}, map[string]bool{}
	var visit func(string, []string)
	visit = func(hash string, path []string) {
		if state[hash] == 2 {
			return
		}
		if state[hash] == 1 {
			for _, h := range path {
				cyclic[h] = true
			}
			cyclic[hash] = true
			return
		}
		state[hash] = 1
		r, ok := revByHash[hash]
		if ok {
			for _, p := range r.Parents {
				if _, exists := revByHash[p]; exists {
					visit(p, append(path, hash))
				}
			}
		}
		state[hash] = 2
	}
	for _, r := range v.Revisions {
		visit(r.Hash, nil)
	}
	if len(cyclic) > 0 {
		for h := range cyclic {
			r := revByHash[h]
			problem(byID[r.CommentID], "revision-cycle", ErrInvalidRevision)
		}
	}
	for _, r := range v.Revisions {
		if !children[r.Hash] && !cyclic[r.Hash] {
			v.Heads = append(v.Heads, r)
		}
	}
	headCounts := map[RevisionKind]int{}
	for _, r := range v.Heads {
		headCounts[r.Kind]++
		if r.Kind == RevisionSpec {
			v.AcceptedSpecHash = r.Hash
			v.CurrentSpecFingerprint = r.SourceSpecFingerprint
		}
	}
	for kind, count := range headCounts {
		if count > 1 {
			for _, r := range v.Heads {
				if r.Kind == kind {
					v.ConflictingHeads = append(v.ConflictingHeads, r)
				}
			}
		}
	}
	if headCounts[RevisionSpec] > 1 {
		v.AcceptedSpecHash = ""
		v.CurrentSpecFingerprint = ""
	}
	approvalByID := map[int64]int{}
	for _, x := range entries {
		switch x.e.Kind {
		case CommentRevision:
		case CommentApproval:
			if x.c.AuthorID == "" || !allowed[x.c.AuthorID] {
				problem(x.c, "wrong-author", ErrInvalidApproval)
				continue
			}
			r, ok := revByID[x.e.RevisionCommentID]
			if ok && r.Hash != x.e.RevisionHash {
				ok = false
			}
			if !ok {
				problem(x.c, "deleted-referenced-comment", ErrInvalidApproval)
				continue
			}
			if commentOrder[r.CommentID] >= commentOrder[x.c.ID] {
				problem(x.c, "approval-before-revision", ErrInvalidApproval)
				continue
			}
			if policy.ForbidSelfApproval && x.c.AuthorID == revisionAuthors[r.Hash] {
				problem(x.c, "wrong-author", ErrInvalidApproval)
				continue
			}
			if r.Kind != RevisionSpec || x.e.SourceSpecFingerprint == "" {
				problem(x.c, "invalid-approval", ErrInvalidApproval)
				continue
			}
			a := Approval{RevisionHash: x.e.RevisionHash, SourceSpecFingerprint: x.e.SourceSpecFingerprint, CommentID: x.c.ID, Author: x.c.Author, AuthorID: x.c.AuthorID}
			approvalByID[x.c.ID] = len(v.Approvals)
			v.Approvals = append(v.Approvals, a)
		case CommentRevocation:
			if x.c.AuthorID == "" || !allowed[x.c.AuthorID] {
				problem(x.c, "wrong-author", ErrInvalidApproval)
				continue
			}
			idx, ok := approvalByID[x.e.TargetCommentID]
			if !ok {
				if _, exists := byID[x.e.TargetCommentID]; !exists {
					problem(x.c, "deleted-referenced-comment", ErrInvalidApproval)
				}
				continue
			}
			v.Approvals[idx].Revoked = true
		}
	}
	for id, approvalID := range expectedRevocations {
		found := false
		for _, x := range entries {
			if x.c.ID == id && x.e.Kind == CommentRevocation && x.e.TargetCommentID == approvalID {
				found = true
				break
			}
		}
		if !found {
			problem(Comment{ID: id}, "missing-or-altered-revocation", ErrInvalidApproval)
			for i := range v.Approvals {
				if v.Approvals[i].CommentID == approvalID {
					v.Approvals[i].Revoked = true
				}
			}
		}
	}
	var headSpec string
	for _, h := range v.Heads {
		if h.Kind == RevisionSpec {
			if headSpec != "" && headSpec != h.Hash {
				headSpec = ""
				break
			}
			headSpec = h.Hash
		}
	}
	fingerprint := v.CurrentSpecFingerprint
	for i := range v.Approvals {
		a := v.Approvals[i]
		if a.RevisionHash != headSpec || a.SourceSpecFingerprint != fingerprint {
			a.Superseded = true
			v.SupersededApprovals = append(v.SupersededApprovals, a)
		} else if a.Revoked {
			v.RevokedApprovals = append(v.RevokedApprovals, a)
		} else {
			v.ValidApprovals = append(v.ValidApprovals, a)
		}
	}
	return v
}

func validEnvelopeFields(e CommentEnvelope) bool {
	validHash := func(s string) bool {
		if len(s) != 64 {
			return false
		}
		for _, c := range s {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	switch e.Kind {
	case CommentRevision:
		if !validHash(e.ContentHash) || e.RevisionHash != "" || e.TargetCommentID != 0 || e.RecordHash != "" {
			return false
		}
		if e.RevisionKind == RevisionSpec {
			return true
		}
		return e.RevisionKind == RevisionPlan && e.SourceSpecFingerprint != ""
	case CommentApproval:
		return validHash(e.RevisionHash) && e.SourceSpecFingerprint != "" && e.RevisionCommentID > 0 && e.TargetCommentID == 0 && e.RevisionKind == "" && e.ContentHash == "" && e.RecordHash != ""
	case CommentRevocation:
		return e.TargetCommentID > 0 && e.RevisionHash == "" && e.SourceSpecFingerprint == "" && e.RevisionKind == "" && e.ContentHash == "" && e.RecordHash != ""
	default:
		return false
	}
}
