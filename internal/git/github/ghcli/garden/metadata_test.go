package garden

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// metadataTransport serves one Issue and records the PATCH it receives. It is separate from fixture, whose audit
// log rightly treats any other Issue body write as a violation.
type metadataTransport struct {
	mu          sync.Mutex
	body        string
	patched     []string
	pr          bool
	afterRead   func()
	labels      []string
	afterDelete func()
	afterPost   func()
}

func (m *metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case r.URL.Path == "/repos/acme/repo/issues/3" && r.Method == http.MethodGet:
		row := map[string]any{"id": 3, "number": 3, "title": "t", "body": m.body, "user": map[string]any{"id": 1}, "labels": func() []any {
			var ls []any
			for _, l := range m.labels {
				ls = append(ls, map[string]string{"name": l})
			}
			return ls
		}()}
		if m.pr {
			row["pull_request"] = map[string]any{"url": "x"}
		}
		b, _ := json.Marshal(row)
		if m.afterRead != nil {
			f := m.afterRead
			m.afterRead = nil
			f()
		}
		return response(200, string(b), ""), nil
	case strings.Contains(r.URL.Path, "/labels/") && r.Method == http.MethodDelete:
		m.labels = slices.DeleteFunc(m.labels, func(l string) bool { return l == core.FormatStageLabel(core.StageDraft) })
		if m.afterDelete != nil {
			m.afterDelete()
		}
		return response(200, `[]`, ""), nil
	case strings.HasSuffix(r.URL.Path, "/labels") && r.Method == http.MethodPost:
		m.labels = append(m.labels, core.FormatStageLabel(core.StageReady))
		if m.afterPost != nil {
			m.afterPost()
		}
		return response(200, `[]`, ""), nil
	case r.URL.Path == "/repos/acme/repo/issues/3" && r.Method == http.MethodPatch:
		raw, _ := io.ReadAll(r.Body)
		var v map[string]string
		_ = json.Unmarshal(raw, &v)
		if len(v) != 1 {
			return response(422, `{}`, ""), nil
		}
		m.patched = append(m.patched, v["body"])
		m.body = v["body"]
		return response(200, `{}`, ""), nil
	}
	return response(404, `{}`, ""), nil
}

func metadataAdapter(t *testing.T, body string, pr bool) (*Adapter, *metadataTransport) {
	t.Helper()
	tr := &metadataTransport{body: body, pr: pr}
	a, err := New(&http.Client{Transport: tr}, "https://api.github.test")
	if err != nil {
		t.Fatal(err)
	}
	a.rememberRepository(repoJSON{ID: 9, FullName: "acme/repo", NodeID: "R_9", Owner: struct {
		Login  string `json:"login"`
		NodeID string `json:"node_id"`
	}{Login: "acme", NodeID: "O_9"}})
	a.rememberIssue(9, 3, 3)
	return a, tr
}

func TestMetadataWriterExposesOnlyTheBodyWrite(t *testing.T) {
	a, _ := metadataAdapter(t, "", false)
	if n := reflect.TypeOf(a.MetadataWriter()).NumMethod(); n != 1 {
		t.Fatalf("metadata writer exposes %d methods", n)
	}
}

func TestReplaceMetadataRefusesUnconditionalBodyWrites(t *testing.T) {
	body, _ := core.CaptureBody("1001", strings.Repeat("a", 32), "keep this text")
	a, tr := metadataAdapter(t, body, false)
	next, _ := core.EncodeMetadata(core.Metadata{Provenance: core.ProvenanceCapture, Owner: "1001", Accepted: true, AcceptedSpecHash: strings.Repeat("a", 64), SpecFingerprint: strings.Repeat("a", 64)})
	tr.afterRead = func() { tr.body += "\nConcurrent mobile edit." }
	err := a.MetadataWriter().ReplaceMetadata(context.Background(), 9, 3, core.BodyMetadata(body), next)
	if !errors.Is(err, core.ErrMetadataWriteUnsupported) || len(tr.patched) != 0 || !strings.HasSuffix(tr.body, "Concurrent mobile edit.") {
		t.Fatalf("err=%v body=%q patches=%v", err, tr.body, tr.patched)
	}
}

func TestReplaceMetadataRefusesChangedPullRequestAndOversizedBodies(t *testing.T) {
	body, _ := core.CaptureBody("1001", strings.Repeat("a", 32), "text")
	prev := core.BodyMetadata(body)
	next, _ := core.EncodeMetadata(core.Metadata{Provenance: core.ProvenanceCapture, Owner: "1001"})
	ctx := context.Background()

	a, tr := metadataAdapter(t, body, false)
	if err := a.MetadataWriter().ReplaceMetadata(ctx, 9, 3, []byte(`{"schema_version":1,"provenance":"x","owner":"y"}`), next); !errors.Is(err, core.ErrMetadataChanged) || len(tr.patched) != 0 {
		t.Fatalf("changed line: err=%v patches=%d", err, len(tr.patched))
	}
	a, tr = metadataAdapter(t, body, true)
	if err := a.MetadataWriter().ReplaceMetadata(ctx, 9, 3, prev, next); !errors.Is(err, core.ErrNotFound) || len(tr.patched) != 0 {
		t.Fatalf("pull request: err=%v patches=%d", err, len(tr.patched))
	}
	a, tr = metadataAdapter(t, "no metadata line here", false)
	if err := a.MetadataWriter().ReplaceMetadata(ctx, 9, 3, nil, next); !errors.Is(err, core.ErrMetadataWriteUnsupported) || len(tr.patched) != 0 {
		t.Fatalf("no metadata: err=%v patches=%d", err, len(tr.patched))
	}
	big, _ := core.CaptureBody("1001", strings.Repeat("a", 32), strings.Repeat("é", 65536))
	a, tr = metadataAdapter(t, big, false)
	if err := a.MetadataWriter().ReplaceMetadata(ctx, 9, 3, core.BodyMetadata(big), next); !errors.Is(err, core.ErrMetadataWriteUnsupported) || len(tr.patched) != 0 {
		t.Fatalf("oversized: err=%v patches=%d", err, len(tr.patched))
	}
}

func TestUpdateStageDetectsConcurrentManagedLabels(t *testing.T) {
	for _, when := range []string{"before", "delete", "post", "none"} {
		t.Run(when, func(t *testing.T) {
			a, tr := metadataAdapter(t, "", false)
			previous := []string{core.ManagedLabel, core.FormatTypeLabel(core.TypeBug), core.FormatStageLabel(core.StageDraft), "user-label"}
			tr.labels = slices.Clone(previous)
			change := func() { tr.labels = append(tr.labels, core.FormatStageLabel(core.StageBlocked)) }
			switch when {
			case "before":
				change()
			case "delete":
				tr.afterDelete = change
			case "post":
				tr.afterPost = change
			}
			err := a.Writer().UpdateStage(t.Context(), 9, 3, previous, core.StageDraft, core.StageReady)
			if when == "none" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, core.ErrMetadataChanged) || !slices.Contains(tr.labels, core.FormatStageLabel(core.StageBlocked)) {
				t.Fatalf("err=%v labels=%v", err, tr.labels)
			}
			if !slices.Contains(tr.labels, "user-label") || !slices.Contains(tr.labels, core.FormatTypeLabel(core.TypeBug)) {
				t.Fatalf("other labels changed: %v", tr.labels)
			}
		})
	}
}
