package garden_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Helper constructors provide valid baseline domain models for adversarial mutations.

func validMetadata() garden.Metadata {
	return garden.Metadata{
		SchemaVersion:    garden.SchemaVersion,
		Provenance:       "bonsai-agent-v1",
		Owner:            "octocat",
		Accepted:         true,
		AcceptedSpecHash: garden.ContentHash("spec v1"),
		SpecFingerprint:  garden.ContentHash("spec v1"),
		CurrentPlanHash:  garden.ContentHash("plan v1"),
	}
}

func validSpecRevision() garden.Revision {
	content := "canonical specification content"
	return garden.Revision{
		Version:   1,
		Kind:      garden.RevisionSpec,
		Content:   content,
		Hash:      garden.ContentHash(content),
		CommentID: 101,
	}
}

func validPlanRevision(specFingerprint string) garden.Revision {
	content := "canonical implementation plan"
	return garden.Revision{
		Version:               1,
		Kind:                  garden.RevisionPlan,
		Content:               content,
		Hash:                  garden.ContentHash(content),
		SourceSpecFingerprint: specFingerprint,
		CommentID:             102,
	}
}

func assertNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", name, r)
		}
	}()
	fn()
}

// 1. Identity and index adversarial tests

func TestIDsAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("ParseRepoID", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"empty", "", true},
			{"whitespace only", "   \t\n", true},
			{"zero", "0", true},
			{"negative", "-1", true},
			{"negative large", "-9223372036854775808", true},
			{"overflow uint64", "18446744073709551616", true},
			{"overflow huge", strings.Repeat("9", 1000), true},
			{"unicode fullwidth digits", "１２３", true},
			{"arabic indic digits", "١٢٣", true},
			{"embedded letters", "123a", true},
			{"floating point", "12.34", true},
			{"hex format", "0x2a", true},
			{"valid standard", "12345", false},
			{"valid with trimmed spaces", " 12345 ", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				id, err := garden.ParseRepoID(tc.input)
				if tc.wantErr {
					if err == nil {
						t.Fatalf("ParseRepoID(%q) expected error, got id %v", tc.input, id)
					}
					if !errors.Is(err, garden.ErrInvalidID) {
						t.Fatalf("ParseRepoID(%q) error %v does not wrap ErrInvalidID", tc.input, err)
					}
				} else {
					if err != nil {
						t.Fatalf("ParseRepoID(%q) unexpected error: %v", tc.input, err)
					}
					if id == 0 {
						t.Fatalf("ParseRepoID(%q) returned zero id", tc.input)
					}
				}
			})
		}
	})

	t.Run("ParseIssueID", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"empty", "", true},
			{"whitespace only", " \t ", true},
			{"zero", "0", true},
			{"negative", "-42", true},
			{"overflow uint64", "18446744073709551616", true},
			{"unicode lookalike digits", "４２", true},
			{"very long input", strings.Repeat("7", 500), true},
			{"valid", "42", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				id, err := garden.ParseIssueID(tc.input)
				if tc.wantErr {
					if err == nil {
						t.Fatalf("ParseIssueID(%q) expected error, got %v", tc.input, id)
					}
					if !errors.Is(err, garden.ErrInvalidID) {
						t.Fatalf("ParseIssueID(%q) error %v does not wrap ErrInvalidID", tc.input, err)
					}
				} else {
					if err != nil {
						t.Fatalf("ParseIssueID(%q) unexpected error: %v", tc.input, err)
					}
					if id == 0 {
						t.Fatalf("ParseIssueID(%q) returned zero id", tc.input)
					}
				}
			})
		}
	})

	t.Run("NewGardenID", func(t *testing.T) {
		t.Parallel()
		if _, err := garden.NewGardenID(0); !errors.Is(err, garden.ErrInvalidID) {
			t.Fatalf("NewGardenID(0) expected ErrInvalidID, got %v", err)
		}
		gid, err := garden.NewGardenID(42)
		if err != nil {
			t.Fatalf("NewGardenID(42) unexpected error: %v", err)
		}
		if gid.Repo != 42 {
			t.Fatalf("NewGardenID(42).Repo = %v, want 42", gid.Repo)
		}
	})

	t.Run("NewLocalRepoID", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"empty", "", true},
			{"whitespace only", "   ", true},
			{"leading space", " /path/to/repo", true},
			{"trailing space", "/path/to/repo ", true},
			{"newline", "repo\n", true},
			{"valid path", "/home/user/project", false},
			{"valid project ID", "bonsai-clone-1", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				lid, err := garden.NewLocalRepoID(tc.input)
				if tc.wantErr {
					if err == nil {
						t.Fatalf("NewLocalRepoID(%q) expected error, got %v", tc.input, lid)
					}
					if !errors.Is(err, garden.ErrInvalidID) {
						t.Fatalf("NewLocalRepoID(%q) error %v does not wrap ErrInvalidID", tc.input, err)
					}
				} else {
					if err != nil {
						t.Fatalf("NewLocalRepoID(%q) unexpected error: %v", tc.input, err)
					}
					if lid.String() != tc.input {
						t.Fatalf("NewLocalRepoID(%q).String() = %q, want %q", tc.input, lid.String(), tc.input)
					}
				}
			})
		}
	})

	t.Run("CloneIdentitySeparation", func(t *testing.T) {
		t.Parallel()
		remoteRepo, err := garden.ParseRepoID("98765")
		if err != nil {
			t.Fatal(err)
		}
		gardenID1, err := garden.NewGardenID(remoteRepo)
		if err != nil {
			t.Fatal(err)
		}
		gardenID2, err := garden.NewGardenID(remoteRepo)
		if err != nil {
			t.Fatal(err)
		}
		local1, err := garden.NewLocalRepoID("clone-alpha")
		if err != nil {
			t.Fatal(err)
		}
		local2, err := garden.NewLocalRepoID("clone-beta")
		if err != nil {
			t.Fatal(err)
		}

		if gardenID1 != gardenID2 {
			t.Fatalf("clones of same remote must share GardenID: got %v and %v", gardenID1, gardenID2)
		}
		if local1 == local2 {
			t.Fatalf("two local clones must have different LocalRepoID: got %v and %v", local1, local2)
		}
	})

	t.Run("DistinctIDStringRepresentations", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: A canonical work item is one GitHub Issue, identified by the stable numeric GitHub repository ID and that Issue's stable numeric ID (and node ID when supplied). Title, branch, local path, owner spelling, and repository name are mutable display data, never identity. GardenID derives only from the remote repository ID. LocalRepoID is the separate path-derived config.ProjectID; clones and worktrees share a GardenID while retaining independent local execution identity and locks.
		// Identity wrappers must remain distinct Go types; their display forms are not part of this contract.
		const num = 42
		repoID := garden.RepoID(num)
		gardenID, err := garden.NewGardenID(repoID)
		if err != nil {
			t.Fatal(err)
		}
		issueID := garden.IssueID(num)
		localID, err := garden.NewLocalRepoID(fmt.Sprintf("%d", num))
		if err != nil {
			t.Fatal(err)
		}

		types := []reflect.Type{reflect.TypeOf(repoID), reflect.TypeOf(gardenID), reflect.TypeOf(issueID), reflect.TypeOf(localID)}
		for i := range types {
			for j := i + 1; j < len(types); j++ {
				if types[i] == types[j] {
					t.Errorf("ID wrappers must be distinct Go types: %v", types[i])
				}
			}
		}
		gardenIDAgain, err := garden.NewGardenID(repoID)
		if err != nil {
			t.Fatal(err)
		}
		localIDAgain, err := garden.NewLocalRepoID("another-clone")
		if err != nil {
			t.Fatal(err)
		}
		if gardenID != gardenIDAgain || localID == localIDAgain {
			t.Errorf("same remote should share GardenID while local identities differ")
		}
	})
}

// 2. Canonical and namespaced label adversarial tests

func TestLabelsAdversarial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		labels       []string
		wantProblems []garden.LabelProblem
		wantType     garden.ItemType
		wantStage    garden.Stage
	}{
		{
			name:         "zero stages",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature)},
			wantProblems: []garden.LabelProblem{garden.ProblemStageCount},
		},
		{
			name:         "two stages",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft), garden.FormatStageLabel(garden.StageReady)},
			wantProblems: []garden.LabelProblem{garden.ProblemStageCount},
		},
		{
			name:         "two types",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft)},
			wantProblems: []garden.LabelProblem{garden.ProblemTypeCount},
		},
		{
			name:         "missing managed marker",
			labels:       []string{garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)},
			wantProblems: []garden.LabelProblem{garden.ProblemMissingManaged},
			wantType:     garden.TypeFeature,
			wantStage:    garden.StageDraft,
		},
		{
			name:         "stage label without managed marker",
			labels:       []string{garden.FormatStageLabel(garden.StageReady)},
			wantProblems: []garden.LabelProblem{garden.ProblemMissingManaged, garden.ProblemTypeCount},
			wantStage:    garden.StageReady,
		},
		{
			name:         "unknown bonsai namespaced label",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft), "bonsai:custom-workflow"},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown},
			wantType:     garden.TypeFeature,
			wantStage:    garden.StageDraft,
		},
		{
			name:         "unknown bonsai type variant",
			labels:       []string{garden.ManagedLabel, "bonsai:type/epic", garden.FormatStageLabel(garden.StageDraft)},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown, garden.ProblemTypeCount},
			wantStage:    garden.StageDraft,
		},
		{
			name:         "unknown bonsai stage variant",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), "bonsai:stage/abandoned"},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown, garden.ProblemStageCount},
			wantType:     garden.TypeFeature,
		},
		{
			// CONTRACT: The canonical labels are bonsai:managed, exactly one bonsai:type/feature|bug|chore|research, and exactly one bonsai:stage/draft|ready|in-progress|review|blocked|done. Unknown bonsai: labels, missing managed marker, or non-singleton type/stage sets are per-item problems.
			name:         "case variant Bonsai:Stage/Ready alongside valid labels",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft), "Bonsai:Stage/Ready"},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown},
			wantType:     garden.TypeFeature,
			wantStage:    garden.StageDraft,
		},
		{
			name:         "trailing space on stage label",
			labels:       []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), "bonsai:stage/ready "},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown, garden.ProblemStageCount},
			wantType:     garden.TypeFeature,
		},
		{
			name:         "trailing space on managed marker",
			labels:       []string{"bonsai:managed ", garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)},
			wantProblems: []garden.LabelProblem{garden.ProblemUnknown, garden.ProblemMissingManaged},
			wantType:     garden.TypeFeature,
			wantStage:    garden.StageDraft,
		},
		{
			name: "unrelated user labels are ignored",
			labels: []string{
				garden.ManagedLabel,
				garden.FormatTypeLabel(garden.TypeFeature),
				garden.FormatStageLabel(garden.StageDraft),
				"bug",
				"stage/ready",
				"priority:critical",
				"area/ui",
				"needs-triage",
			},
			wantProblems: nil,
			wantType:     garden.TypeFeature,
			wantStage:    garden.StageDraft,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := garden.Classify(tc.labels)
			if len(tc.wantProblems) == 0 {
				if len(got.Problems) != 0 {
					t.Fatalf("expected no problems, got %v", got.Problems)
				}
			} else {
				for _, wp := range tc.wantProblems {
					found := false
					for _, gp := range got.Problems {
						if gp == wp {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("// CONTRACT: The canonical labels are bonsai:managed, exactly one bonsai:type/feature|bug|chore|research, and exactly one bonsai:stage/draft|ready|in-progress|review|blocked|done. Unknown bonsai: labels, missing managed marker, or non-singleton type/stage sets are per-item problems: missing expected problem %q in got %v", wp, got.Problems)
					}
				}
			}
			if tc.wantType != "" && got.Type != tc.wantType {
				t.Fatalf("got Type %q, want %q", got.Type, tc.wantType)
			}
			if tc.wantStage != "" && got.Stage != tc.wantStage {
				t.Fatalf("got Stage %q, want %q", got.Stage, tc.wantStage)
			}
		})
	}
}

// 3. Metadata envelope adversarial tests

func TestMetadataAdversarial(t *testing.T) {
	t.Parallel()

	t.Run("RoundTrip", func(t *testing.T) {
		t.Parallel()
		orig := validMetadata()
		encoded, err := garden.EncodeMetadata(orig)
		if err != nil {
			t.Fatalf("EncodeMetadata failed: %v", err)
		}
		decoded, err := garden.DecodeMetadata(encoded)
		if err != nil {
			t.Fatalf("DecodeMetadata failed: %v", err)
		}
		if !reflect.DeepEqual(decoded, orig) {
			t.Fatalf("round-trip mismatch: got %+v, want %+v", decoded, orig)
		}
	})

	t.Run("UnknownFieldAtKnownSchemaVersion", func(t *testing.T) {
		t.Parallel()
		raw := `{"schema_version":1,"provenance":"p","owner":"o","surprise_field":"intruder"}`
		_, err := garden.DecodeMetadata([]byte(raw))
		if !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Fatalf("expected ErrInvalidMetadata for unknown field, got %v", err)
		}
	})

	t.Run("FutureSchemaVersion", func(t *testing.T) {
		t.Parallel()
		raw := `{"schema_version":2,"provenance":"p","owner":"o","future_capability":true}`
		_, err := garden.DecodeMetadata([]byte(raw))
		if !errors.Is(err, garden.ErrUnsupportedSchema) {
			t.Fatalf("expected ErrUnsupportedSchema for schema_version 2, got %v", err)
		}
	})

	t.Run("TruncatedJSON", func(t *testing.T) {
		t.Parallel()
		raw := `{"schema_version":1,"provenance":"bonsai","owner":`
		_, err := garden.DecodeMetadata([]byte(raw))
		if !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Fatalf("expected ErrInvalidMetadata for truncated JSON, got %v", err)
		}
	})

	t.Run("TrailingGarbage", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			raw  string
		}{
			{"trailing token", `{"schema_version":1,"provenance":"p","owner":"o"} trailing_junk`},
			{"trailing second object", `{"schema_version":1,"provenance":"p","owner":"o"} {"injected": true}`},
			{"trailing bracket", `{"schema_version":1,"provenance":"p","owner":"o"} ]`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				_, err := garden.DecodeMetadata([]byte(tc.raw))
				if !errors.Is(err, garden.ErrInvalidMetadata) {
					t.Fatalf("expected ErrInvalidMetadata for trailing data, got %v", err)
				}
			})
		}
	})

	t.Run("NullInput", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: Issue metadata is a JSON envelope with schema_version: 1, provenance, owner, acceptance/spec fingerprint and current plan hash. Provenance and owner are required.
		// A literal null is not a valid JSON metadata envelope; it must return ErrInvalidMetadata, not report an unsupported schema version.
		_, err := garden.DecodeMetadata([]byte("null"))
		if err == nil {
			t.Fatal("expected error for null metadata, got nil")
		}
		if !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Errorf("// CONTRACT: Issue metadata is a JSON envelope with schema_version: 1, provenance, owner, acceptance/spec fingerprint and current plan hash. Provenance and owner are required: DecodeMetadata(null) got %v, want ErrInvalidMetadata", err)
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		t.Parallel()
		_, err := garden.DecodeMetadata([]byte(""))
		if !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Fatalf("expected ErrInvalidMetadata for empty input, got %v", err)
		}
	})

	t.Run("DeeplyNestedJSON", func(t *testing.T) {
		t.Parallel()
		nest := strings.Repeat("[", 500) + strings.Repeat("]", 500)
		assertNoPanic(t, "DeeplyNestedJSON", func() {
			_, err := garden.DecodeMetadata([]byte(nest))
			if err == nil {
				t.Fatal("expected error for deeply nested array, got nil")
			}
		})
	})

	t.Run("SizeCapBoundary", func(t *testing.T) {
		t.Parallel()
		base := `{"schema_version":1,"provenance":"`
		closing := `","owner":"o"}`
		padLen := garden.MaxMetadataBytes - len(base) - len(closing)

		// Exactly at cap
		atCap := base + strings.Repeat("a", padLen) + closing
		if len(atCap) != garden.MaxMetadataBytes {
			t.Fatalf("expected len %d, got %d", garden.MaxMetadataBytes, len(atCap))
		}
		m, err := garden.DecodeMetadata([]byte(atCap))
		if err != nil {
			t.Fatalf("input exactly at cap rejected: %v", err)
		}
		if m.Owner != "o" {
			t.Fatalf("got owner %q, want 'o'", m.Owner)
		}

		// Just over cap by one byte
		overCap := base + strings.Repeat("a", padLen+1) + closing
		if len(overCap) != garden.MaxMetadataBytes+1 {
			t.Fatalf("expected len %d, got %d", garden.MaxMetadataBytes+1, len(overCap))
		}
		_, err = garden.DecodeMetadata([]byte(overCap))
		if !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Fatalf("expected ErrInvalidMetadata for input over cap, got %v", err)
		}
	})

	t.Run("DuplicateKeys", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: Decoders reject unknown fields for known schemas; unknown future versions return an unsupported-schema problem. Malformed or oversized metadata is reported for that item and never causes the Garden to be erased.
		dupJSON := `{"schema_version":1,"provenance":"p1","owner":"alice","provenance":"p2"}`
		_, err := garden.DecodeMetadata([]byte(dupJSON))
		if err == nil || !errors.Is(err, garden.ErrInvalidMetadata) {
			t.Errorf("// CONTRACT: Decoders reject unknown fields for known schemas; unknown future versions return an unsupported-schema problem. Malformed or oversized metadata is reported for that item and never causes the Garden to be erased: duplicate keys accepted without ErrInvalidMetadata (err=%v)", err)
		}
	})

	t.Run("InvalidUTF8", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: Malformed or oversized metadata is reported for that item and never causes the Garden to be erased.
		badUTF8 := []byte("{\"schema_version\":1,\"provenance\":\"\xff\xfe\xfd\",\"owner\":\"alice\"}")
		assertNoPanic(t, "InvalidUTF8", func() {
			_, err := garden.DecodeMetadata(badUTF8)
			if err == nil || !errors.Is(err, garden.ErrInvalidMetadata) {
				t.Errorf("// CONTRACT: Malformed or oversized metadata is reported for that item and never causes the Garden to be erased: invalid UTF-8 string bytes accepted without ErrInvalidMetadata (err=%v)", err)
			}
		})
	})
}

// 4. Content addressing and approval reverification adversarial tests

func TestRevisionsAndApprovalsAdversarial(t *testing.T) {
	t.Parallel()
	id := garden.IssueIdentity{Repo: 1, Issue: 2}
	spec := "spec"
	hash := garden.ContentHash(spec)
	makeComment := func(id int64, author string, e garden.CommentEnvelope, body string) garden.Comment {
		t.Helper()
		if e.Kind == garden.CommentApproval && e.RevisionCommentID == 0 {
			e.RevisionCommentID = 1
		}
		raw, err := garden.EncodeCommentEnvelope(e, body)
		if err != nil {
			t.Fatal(err)
		}
		return garden.Comment{ID: id, Author: author, AuthorID: author + "-id", Body: raw}
	}
	rev := makeComment(1, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 1, IssueID: 2, RevisionKind: garden.RevisionSpec, ContentHash: hash}, spec)
	approval := makeComment(2, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 1, IssueID: 2, RevisionHash: hash, SourceSpecFingerprint: hash}, "approved")
	t.Run("allowed approval", func(t *testing.T) {
		v := garden.Verify(id, []garden.Comment{rev, approval}, garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}})
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("wrong author", func(t *testing.T) {
		v := garden.Verify(id, []garden.Comment{rev, approval}, garden.ApprovalPolicy{AllowedActorIDs: []string{"other-id"}})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("edited revision", func(t *testing.T) {
		edited := rev
		edited.Body += "tamper"
		v := garden.Verify(id, []garden.Comment{edited, approval}, garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}})
		if len(v.Problems) == 0 {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("issue binding", func(t *testing.T) {
		foreign := makeComment(3, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 3, IssueID: 4, RevisionHash: hash, SourceSpecFingerprint: hash}, "approved")
		v := garden.Verify(id, []garden.Comment{rev, foreign}, garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("%+v", v)
		}
	})
}

// 5. Lane classification adversarial tests
