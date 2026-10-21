package review

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

var (
	sha1  = strings.Repeat("a", 40)
	sha2  = strings.Repeat("b", 40)
	sha3  = strings.Repeat("c", 40)
	local = garden.LocalRepoID("repo-1")
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &FileStore{Path: filepath.Join(t.TempDir(), "garden-review.json"), Now: func() time.Time { return now }}
}

func runIn(attempt, commit, key string, auto bool) RunInput {
	return RunInput{AttemptID: attempt, Issue: 7, Commit: commit, Base: Base{Ref: "main", MergeBase: sha3}, RunSession: "sess-" + key,
		Trigger: TriggerAwaitingReview, PolicyAccount: "acc", Key: key, Auto: auto}
}

func validFile(findings string) []byte {
	return []byte(`{"schema":1,"summary":"looked at it","findings":[` + findings + `]}`)
}

const oneFinding = `{"severity":"high","title":"Nil deref","path":"a/b.go","line":12,"evidence":"x.y with x nil","recommendation":"check"}`

func TestParseReportFile(t *testing.T) {
	exp := Expect{Commit: sha1, Attempt: "att-1"}
	big := strings.Repeat("x", MaxReportBytes)
	var many []string
	for i := 0; i < MaxFindings+1; i++ {
		many = append(many, strings.Replace(oneFinding, "Nil deref", "t"+string(rune('a'+i%26))+strings.Repeat("z", i), 1))
	}
	cases := []struct {
		name string
		data []byte
		ok   bool
	}{
		{"valid", validFile(oneFinding), true},
		{"no findings", validFile(""), true},
		{"oversize", []byte(`{"schema":1,"summary":"` + big + `","findings":[]}`), false},
		{"empty", nil, false},
		{"unknown top field", []byte(`{"schema":1,"summary":"s","findings":[],"garden_review":{"enabled":true}}`), false},
		{"unknown finding field", validFile(strings.Replace(oneFinding, `"line"`, `"x":1,"line"`, 1)), false},
		{"duplicate key", []byte(`{"schema":1,"schema":1,"summary":"s","findings":[]}`), false},
		{"wrong schema", []byte(`{"schema":2,"summary":"s","findings":[]}`), false},
		{"missing summary", []byte(`{"schema":1,"summary":"","findings":[]}`), false},
		{"trailing", append(validFile(""), []byte(` {}`)...), false},
		{"bad severity", validFile(strings.Replace(oneFinding, "high", "critical", 1)), false},
		{"abs path", validFile(strings.Replace(oneFinding, "a/b.go", "/etc/passwd", 1)), false},
		{"dotdot", validFile(strings.Replace(oneFinding, "a/b.go", "a/../../b.go", 1)), false},
		{"leading dotdot", validFile(strings.Replace(oneFinding, "a/b.go", "../b.go", 1)), false},
		{"drive", validFile(strings.Replace(oneFinding, "a/b.go", `C:/x.go`, 1)), false},
		{"backslash", validFile(strings.Replace(oneFinding, "a/b.go", `a\\b.go`, 1)), false},
		{"nul path", validFile(strings.Replace(oneFinding, "a/b.go", `a\u0000b.go`, 1)), false},
		{"control in path", validFile(strings.Replace(oneFinding, "a/b.go", `a\u001bb.go`, 1)), false},
		{"unclean path", validFile(strings.Replace(oneFinding, "a/b.go", "a//b.go", 1)), false},
		{"empty path", validFile(strings.Replace(oneFinding, "a/b.go", "", 1)), false},
		{"negative line", validFile(strings.Replace(oneFinding, `"line":12`, `"line":-1`, 1)), false},
		{"no evidence", validFile(strings.Replace(oneFinding, "x.y with x nil", "", 1)), false},
		{"long title", validFile(strings.Replace(oneFinding, "Nil deref", strings.Repeat("t", MaxTitleRunes+1), 1)), false},
		{"too many findings", validFile(strings.Join(many, ",")), false},
	}
	for _, c := range cases {
		_, err := ParseReportFile(c.data, exp)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidReport)) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	if _, err := ParseReportFile(validFile(""), Expect{Commit: "abc", Attempt: "a"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad expectation = %v", err)
	}
}

func TestParseReportFileSanitizesAndFingerprints(t *testing.T) {
	data := []byte(`{"schema":1,"summary":"ok\u001b[31m red\u001b[0m\u001b]0;title\u0007 done\u0000","findings":[` +
		`{"severity":"low","title":"A\u001b[2Jb\nc","path":"x.go","line":0,"evidence":"e\u0007v\nline2","recommendation":"r\u202e"},` +
		`{"severity":"low","title":"ab c","path":"x.go","line":0,"evidence":"EV\nline2","recommendation":"dup"}]}`)
	b, err := ParseReportFile(data, Expect{Commit: sha1, Attempt: "att"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Summary != "ok red done" {
		t.Fatalf("summary = %q", b.Summary)
	}
	if len(b.Findings) != 1 {
		t.Fatalf("duplicate fingerprint kept: %d", len(b.Findings))
	}
	f := b.Findings[0]
	if f.Title != "A b c" && f.Title != "Ab c" || strings.ContainsAny(f.Evidence+f.Recommendation+f.Title, "\x1b\x07\u202e") {
		t.Fatalf("not sanitized: %+v", f)
	}
	if !strings.Contains(f.Evidence, "\n") {
		t.Fatal("multiline evidence lost its newline")
	}
	// ID is stable across position and severity, and changes with the line.
	if FindingID("x.go", 0, "A b c", "ev") != FindingID("x.go", 0, " a  B c", "EV") || FindingID("x.go", 1, "A b c", "ev") == FindingID("x.go", 0, "A b c", "ev") {
		t.Fatal("fingerprint")
	}
}

func TestInstructionsFixed(t *testing.T) {
	s, err := Instructions("/tmp/r.json")
	if err != nil || !strings.Contains(s, "read-only") || !strings.Contains(s, "file evidence") || !strings.Contains(s, "/tmp/r.json") {
		t.Fatalf("%q %v", s, err)
	}
	if _, err := Instructions("a\nb"); err == nil {
		t.Fatal("control bytes accepted")
	}
}

func TestPolicyFromPrefs(t *testing.T) {
	if p := PolicyFromPrefs(nil); p.Enabled {
		t.Fatal("nil must be off")
	}
	if p := PolicyFromPrefs(&config.GardenReviewPrefs{Enabled: true}); p.Enabled {
		t.Fatal("invalid must be off")
	}
	p := PolicyFromPrefs(&config.GardenReviewPrefs{Enabled: true, AccountID: "a", Trigger: "new-commit", RequireCI: true})
	if !p.Enabled || p.Trigger != TriggerNewCommit || p.MaxAutoRuns != 3 || !p.RequireCI {
		t.Fatalf("%+v", p)
	}
}

func TestStoreRunLifecycleAndAppendOnly(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	r1, created, err := s.RecordRun(ctx, local, runIn("att", sha1, "k1", true))
	if err != nil || !created || r1.Revision != 1 || r1.State != StateRunning {
		t.Fatalf("%+v %v %v", r1, created, err)
	}
	// Same key: same record, nothing new.
	again, created, err := s.RecordRun(ctx, local, runIn("att", sha1, "k1", true))
	if err != nil || created || again.ID != r1.ID {
		t.Fatalf("replay = %+v %v %v", again, created, err)
	}
	// One in flight per attempt.
	if _, _, err := s.RecordRun(ctx, local, runIn("att", sha2, "k2", false)); !errors.Is(err, ErrRunInFlight) {
		t.Fatalf("in flight = %v", err)
	}
	body, _ := ParseReportFile(validFile(oneFinding), Expect{Commit: sha1, Attempt: "att"})
	if _, err := s.MarkReady(ctx, local, r1.ID, 2, body); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong revision = %v", err)
	}
	other, _ := ParseReportFile(validFile(""), Expect{Commit: sha2, Attempt: "att"})
	if _, err := s.MarkReady(ctx, local, r1.ID, 1, other); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other commit body = %v", err)
	}
	ready, err := s.MarkReady(ctx, local, r1.ID, 1, body)
	if err != nil || ready.State != StateReady || len(ready.Findings) != 1 || ready.ReadyAt.IsZero() {
		t.Fatalf("%+v %v", ready, err)
	}
	// Final reports never change.
	if _, err := s.MarkFailed(ctx, local, r1.ID, 1, FailTimeout); !errors.Is(err, ErrConflict) {
		t.Fatalf("late fail = %v", err)
	}
	if _, err := s.MarkFailed(ctx, local, "nope", 1, FailTimeout); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	r2, _, err := s.RecordRun(ctx, local, runIn("att", sha2, "k2", false))
	if err != nil || r2.Revision != 2 {
		t.Fatalf("%+v %v", r2, err)
	}
	if _, err := s.MarkFailed(ctx, local, r2.ID, 2, FailCode("raw provider text")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("free-form code = %v", err)
	}
	if _, err := s.MarkInvalid(ctx, local, r2.ID, 2); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.Load(ctx, local)
	if len(snap.Reports) != 2 || snap.Reports[0].State != StateReady || snap.Reports[0].Findings[0].Title != "Nil deref" || snap.Reports[1].State != StateInvalid {
		t.Fatalf("history changed: %+v", snap.Reports)
	}
	if got, ok := LatestReady(snap.Reports, "att"); !ok || got.Revision != 1 {
		t.Fatalf("latest ready = %+v", got)
	}
	if Superseded(snap.Reports, snap.Reports[0]) {
		t.Fatal("invalid later run must not supersede")
	}
}

func TestSupersededDerived(t *testing.T) {
	rs := []Report{{AttemptID: "a", Revision: 1, State: StateReady}, {AttemptID: "a", Revision: 2, State: StateReady}, {AttemptID: "b", Revision: 1, State: StateReady}}
	if !Superseded(rs, rs[0]) || Superseded(rs, rs[1]) || Superseded(rs, rs[2]) {
		t.Fatal("superseded")
	}
}

func TestStoreOtherCloneIsolated(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	if _, _, err := s.RecordRun(ctx, local, runIn("att", sha1, "k1", true)); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, "repo-2")
	if err != nil || len(snap.Reports) != 0 {
		t.Fatalf("other clone sees %+v %v", snap, err)
	}
	// Same attempt id and key in another clone is independent.
	r, created, err := s.RecordRun(ctx, "repo-2", runIn("att", sha1, "k1", true))
	if err != nil || !created || r.Revision != 1 {
		t.Fatalf("%+v %v %v", r, created, err)
	}
	if _, err := s.Load(ctx, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty repo = %v", err)
	}
}

func TestStoreStrictDecodeAndCaps(t *testing.T) {
	ctx := context.Background()
	bad := map[string]string{
		"unknown":   `{"schema_version":1,"repos":{},"x":1}`,
		"newer":     `{"schema_version":9,"repos":{}}`,
		"dupkey":    `{"schema_version":1,"schema_version":1,"repos":{}}`,
		"trailing":  `{"schema_version":1,"repos":{}} {}`,
		"norepos":   `{"schema_version":1}`,
		"badreport": `{"schema_version":1,"repos":{"r":{"reports":[{"id":"x"}]}}}`,
		"garbage":   `nope`,
	}
	for name, body := range bad {
		s := newStore(t)
		if err := os.WriteFile(s.Path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(ctx, local); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	s := newStore(t)
	full := repoData{}
	for i := 0; i < MaxReports; i++ {
		id := fmt.Sprintf("att-%d", i)
		full.Reports = append(full.Reports, Report{ID: ReportID(id, 1), AttemptID: id, Revision: 1, Commit: sha1, RunSession: "s-" + id,
			Trigger: TriggerManual, PolicyAccount: "a", State: StateFailed, FailCode: FailTimeout, CreatedAt: s.now(), RunKey: "k-" + id})
	}
	if err := s.write(disk{Repos: map[string]repoData{string(local): full}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordRun(ctx, local, runIn("one-more", sha1, "kz", true)); !errors.Is(err, ErrFull) {
		t.Fatalf("cap = %v", err)
	}
	// The same key still resolves when full.
	if _, created, err := s.RecordRun(ctx, local, runIn("att-0", sha1, "k-att-0", true)); err != nil || created {
		t.Fatalf("replay at cap = %v %v", created, err)
	}
	for _, in := range []RunInput{{}, runIn("att", "short", "k", false), runIn("a b", sha1, "k", false)} {
		if _, _, err := newStore(t).RecordRun(ctx, local, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("input %+v = %v", in, err)
		}
	}
}

func TestStoreFindingsCap(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	mk := func(att string, n int) Body {
		b := Body{AttemptID: att, Commit: sha1, Summary: "s"}
		for i := 0; i < n; i++ {
			b.Findings = append(b.Findings, Finding{ID: FindingID("p.go", i, att, "e"), Severity: SeverityInfo, Title: "t", Path: "p.go", Line: i, Evidence: "e"})
		}
		return b
	}
	seed := repoData{}
	for i := 0; i < MaxFindingsIn/MaxFindings; i++ {
		att := fmt.Sprintf("a%d", i)
		seed.Reports = append(seed.Reports, Report{ID: ReportID(att, 1), AttemptID: att, Revision: 1, Commit: sha1, RunSession: "s-" + att,
			Trigger: TriggerManual, PolicyAccount: "a", State: StateReady, CreatedAt: s.now(), RunKey: "k-" + att, Summary: "s", Findings: mk(att, MaxFindings).Findings})
	}
	if err := s.write(disk{Repos: map[string]repoData{string(local): seed}}); err != nil {
		t.Fatal(err)
	}
	r, _, _ := s.RecordRun(ctx, local, runIn("last", sha1, "klast", false))
	if _, err := s.MarkReady(ctx, local, r.ID, 1, mk("last", 1)); !errors.Is(err, ErrFull) {
		t.Fatalf("findings cap = %v", err)
	}
	if _, err := s.MarkFailed(ctx, local, r.ID, 1, FailTooLarge); err != nil {
		t.Fatal(err)
	}
}

func TestStoreFileHasNoSecretsShape(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	if _, _, err := s.RecordRun(ctx, local, runIn("att", sha1, "k1", true)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.Path)
	for _, w := range []string{"prompt", "argv", "env", "token", "output", "command"} {
		if strings.Contains(string(b), w) {
			t.Errorf("store mentions %q", w)
		}
	}
	if fi, err := os.Stat(s.Path); err != nil || fi.Size() == 0 {
		t.Fatal("not written")
	}
}

func readyReport(t *testing.T, s *FileStore, att, commit, key string, findings string) Report {
	t.Helper()
	ctx := context.Background()
	r, _, err := s.RecordRun(ctx, local, runIn(att, commit, key, false))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseReportFile(validFile(findings), Expect{Commit: commit, Attempt: att})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.MarkReady(ctx, local, r.ID, r.Revision, b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDismissals(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	r1 := readyReport(t, s, "att", sha1, "k1", oneFinding)
	fid := r1.Findings[0].ID
	d := Dismissal{AttemptID: "att", ReportRevision: 1, FindingID: fid, Commit: sha1, Reason: "  false positive  "}
	for _, reason := range []string{"", "   ", "bad\x1b[0m", strings.Repeat("x", 501), "nul\x00"} {
		bad := d
		bad.Reason = reason
		if _, _, err := s.Dismiss(ctx, local, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("reason %q = %v", reason, err)
		}
	}
	wrongCommit := d
	wrongCommit.Commit = sha2
	if _, _, err := s.Dismiss(ctx, local, wrongCommit); !errors.Is(err, ErrConflict) {
		t.Fatalf("commit mismatch = %v", err)
	}
	unknown := d
	unknown.FindingID = "f-0000000000000000"
	if _, _, err := s.Dismiss(ctx, local, unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown finding = %v", err)
	}
	got, created, err := s.Dismiss(ctx, local, d)
	if err != nil || !created || got.Reason != "false positive" || got.At.IsZero() {
		t.Fatalf("%+v %v %v", got, created, err)
	}
	if _, created, _ := s.Dismiss(ctx, local, d); created {
		t.Fatal("dismiss not idempotent")
	}
	// A later report with the same fingerprint is not auto-dismissed but reports the prior dismissal.
	r2 := readyReport(t, s, "att", sha2, "k2", oneFinding)
	if r2.Findings[0].ID != fid {
		t.Fatal("fingerprint changed across reports")
	}
	snap, _ := s.Load(ctx, local)
	if IsDismissed(snap.Dismissals, r2, fid) || !IsDismissed(snap.Dismissals, r1, fid) {
		t.Fatal("dismissal bound to the wrong revision")
	}
	prior, ok := PriorDismissal(snap.Dismissals, "att", fid, 2)
	if !ok || prior.Label() != "previously dismissed in r1" {
		t.Fatalf("prior = %+v %v", prior, ok)
	}
	if _, ok := PriorDismissal(snap.Dismissals, "att", fid, 1); ok {
		t.Fatal("prior must be earlier")
	}
	if _, ok := PriorDismissal(snap.Dismissals, "other", fid, 2); ok {
		t.Fatal("prior crossed attempts")
	}
}

func TestAcceptPreviewRules(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rep := &Report{AttemptID: "att", Revision: 2, Commit: sha1, State: StateReady, Findings: []Finding{
		{ID: "f-1111111111111111", Severity: SeverityHigh}, {ID: "f-2222222222222222", Severity: SeverityLow}}}
	freshPass := CISnapshot{State: CIPassed, HeadSHA: sha1, FetchedAt: now.Add(-time.Minute)}
	cases := []struct {
		name   string
		in     PreviewInput
		want   []Ack
		stale  bool
		counts map[Severity]int
	}{
		{"no report always needs ack", PreviewInput{HeadSHA: sha1, CI: freshPass}, []Ack{AckNoReport}, false, nil},
		{"current report policy off", PreviewInput{Report: rep, HeadSHA: sha1, CI: freshPass}, nil, false, map[Severity]int{SeverityHigh: 1, SeverityLow: 1}},
		{"stale report", PreviewInput{Report: rep, HeadSHA: sha2, CI: freshPass}, []Ack{AckReportStale}, true, nil},
		{"open high", PreviewInput{Policy: Policy{BlockOnHigh: true}, Report: rep, HeadSHA: sha1, CI: freshPass}, []Ack{AckOpenHigh}, false, nil},
		{"high dismissed", PreviewInput{Policy: Policy{BlockOnHigh: true}, Report: rep, HeadSHA: sha1, CI: freshPass,
			Dismissals: []Dismissal{{AttemptID: "att", ReportRevision: 2, FindingID: "f-1111111111111111", Commit: sha1}}}, nil, false, map[Severity]int{SeverityLow: 1}},
		{"high dismissed for other revision", PreviewInput{Policy: Policy{BlockOnHigh: true}, Report: rep, HeadSHA: sha1, CI: freshPass,
			Dismissals: []Dismissal{{AttemptID: "att", ReportRevision: 1, FindingID: "f-1111111111111111", Commit: sha1}}}, []Ack{AckOpenHigh}, false, nil},
		{"ci failed", PreviewInput{Policy: Policy{RequireCI: true}, Report: rep, HeadSHA: sha1, CI: CISnapshot{State: CIFailed, HeadSHA: sha1, FetchedAt: now}}, []Ack{AckCINotPassed}, false, nil},
		{"ci unavailable", PreviewInput{Policy: Policy{RequireCI: true}, Report: rep, HeadSHA: sha1, CI: CISnapshot{State: CIUnavailable}}, []Ack{AckCINotPassed}, false, nil},
		{"ci none not required", PreviewInput{Report: rep, HeadSHA: sha1, CI: CISnapshot{State: CINone}}, nil, false, nil},
		{"ci passed for older head", PreviewInput{Policy: Policy{RequireCI: true}, Report: rep, HeadSHA: sha1, CI: CISnapshot{State: CIPassed, HeadSHA: sha2, FetchedAt: now}}, []Ack{AckCIStale}, false, nil},
		{"ci old fetch", PreviewInput{Policy: Policy{RequireCI: true}, Report: rep, HeadSHA: sha1, CI: CISnapshot{State: CIPassed, HeadSHA: sha1, FetchedAt: now.Add(-time.Hour)}}, []Ack{AckCIStale}, false, nil},
		{"everything", PreviewInput{Policy: Policy{RequireCI: true, BlockOnHigh: true}, HeadSHA: sha1, CI: CISnapshot{State: CIFailed, HeadSHA: sha2, FetchedAt: now}}, []Ack{AckNoReport, AckCINotPassed, AckCIStale}, false, nil},
	}
	for _, c := range cases {
		c.in.AttemptID, c.in.Now = "att", now
		p := AcceptPreview(c.in)
		if len(p.Required) != len(c.want) || p.ReportStale != c.stale || !p.AcceptAllowed {
			t.Errorf("%s: required=%v stale=%v", c.name, p.Required, p.ReportStale)
			continue
		}
		for _, a := range c.want {
			if !p.Needs(a) {
				t.Errorf("%s: missing %s in %v", c.name, a, p.Required)
			}
		}
		for sev, n := range c.counts {
			if p.Counts[sev] != n {
				t.Errorf("%s: counts %v", c.name, p.Counts)
			}
		}
	}
	if p := AcceptPreview(PreviewInput{AttemptID: "att", HeadSHA: "short"}); p.AcceptAllowed {
		t.Fatal("bad head allowed")
	}
}

func TestAcceptFlow(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	r := readyReport(t, s, "att", sha1, "k1", oneFinding)
	now := s.now()
	in := PreviewInput{Policy: Policy{BlockOnHigh: true, RequireCI: true}, AttemptID: "att", Report: &r, HeadSHA: sha1,
		CI: CISnapshot{State: CIPassed, HeadSHA: sha1, FetchedAt: now}, Now: now}
	p := AcceptPreview(in)
	if !p.Needs(AckOpenHigh) || p.Needs(AckCINotPassed) {
		t.Fatalf("%v", p.Required)
	}
	if _, _, err := s.Accept(ctx, local, p, sha2, []string{"open-high"}, true); !errors.Is(err, ErrStale) {
		t.Fatalf("moved head = %v", err)
	}
	if _, _, err := s.Accept(ctx, local, p, sha1, nil, true); !errors.Is(err, ErrNeedsAcknowledgment) {
		t.Fatalf("missing ack = %v", err)
	}
	if _, _, err := s.Accept(ctx, local, p, sha1, []string{"open-high"}, false); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("unconfirmed = %v", err)
	}
	if snap, _ := s.Load(ctx, local); len(snap.Decisions) != 0 {
		t.Fatal("refused accept recorded")
	}
	d, created, err := s.Accept(ctx, local, p, sha1, []string{"open-high", "unrelated"}, true)
	if err != nil || !created || d.Origin != "local" || d.ReviewedHead != sha1 || d.ReportRevision != 1 || len(d.Acknowledged) != 1 ||
		len(d.UnresolvedAtDecision) != 1 || d.UnresolvedCounts[SeverityHigh] != 1 || !d.PolicyFlags.BlockOnHigh {
		t.Fatalf("%+v %v %v", d, created, err)
	}
	d2, created, err := s.Accept(ctx, local, p, sha1, []string{"open-high"}, true)
	if err != nil || created || d2.ID != d.ID {
		t.Fatalf("idempotent = %+v %v %v", d2, created, err)
	}
	// New commit: the old decision stays, flagged.
	p2 := AcceptPreview(PreviewInput{AttemptID: "att", Report: &r, HeadSHA: sha2, Now: now})
	if !p2.ReportStale {
		t.Fatal("report not stale")
	}
	d3, created, err := s.Accept(ctx, local, p2, sha2, []string{"report-stale"}, true)
	if err != nil || !created || d3.ID == d.ID {
		t.Fatalf("%+v %v %v", d3, created, err)
	}
	snap, _ := s.Load(ctx, local)
	if len(snap.Decisions) != 2 || !snap.Decisions[0].ForOlderCommit(sha2) || snap.Decisions[1].ForOlderCommit(sha2) {
		t.Fatalf("decisions = %+v", snap.Decisions)
	}
	// No report: decision with revision 0.
	p3 := AcceptPreview(PreviewInput{AttemptID: "other", HeadSHA: sha1, Now: now})
	d4, _, err := s.Accept(ctx, local, p3, sha1, []string{"no-report"}, true)
	if err != nil || d4.ReportRevision != 0 || d4.ReportCommit != "" {
		t.Fatalf("%+v %v", d4, err)
	}
}

func TestPlanReconcile(t *testing.T) {
	pol := Policy{Enabled: true, AccountID: "acc", Trigger: TriggerAwaitingReview, MaxAutoRuns: 3}
	att := func(id, state, head string) AttemptFacts {
		return AttemptFacts{AttemptID: id, State: state, HeadSHA: head, WorktreeOK: true}
	}
	await := att("a", AttemptStateAwaitingReview, sha1)

	t.Run("off", func(t *testing.T) {
		for _, p := range []Policy{{}, {Enabled: true, AccountID: "x", Trigger: TriggerManual}, {Enabled: true, Trigger: TriggerNewCommit}} {
			if got := PlanReconcile(p, []AttemptFacts{await}, nil); len(got) != 0 {
				t.Errorf("%+v started %v", p, got)
			}
		}
	})
	t.Run("awaiting review starts once", func(t *testing.T) {
		got := PlanReconcile(pol, []AttemptFacts{await, att("b", AttemptStateRunning, sha1), att("c", "failed", sha1), {AttemptID: "d", State: AttemptStateAwaitingReview, HeadSHA: sha1}}, nil)
		if len(got) != 1 || got[0].AttemptID != "a" || got[0].Commit != sha1 || got[0].Key != RunKey("a", sha1, TriggerAwaitingReview, "") {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("recorded run in any state blocks, across restart", func(t *testing.T) {
		s, ctx := newStore(t), context.Background()
		req := PlanReconcile(pol, []AttemptFacts{await}, nil)[0]
		in := RunInput{AttemptID: req.AttemptID, Commit: req.Commit, Base: Base{Ref: "main"}, RunSession: "sess-1", Trigger: req.Trigger, PolicyAccount: "acc", Key: req.Key, Auto: true}
		rec, _, err := s.RecordRun(ctx, local, in)
		if err != nil {
			t.Fatal(err)
		}
		reopen := func() []Report {
			s2 := &FileStore{Path: s.Path}
			snap, err := s2.Load(ctx, local)
			if err != nil {
				t.Fatal(err)
			}
			return snap.Reports
		}
		if got := PlanReconcile(pol, []AttemptFacts{await}, reopen()); len(got) != 0 {
			t.Fatalf("running run restarted: %v", got)
		}
		if _, err := s.MarkFailed(ctx, local, rec.ID, rec.Revision, FailInterrupted); err != nil {
			t.Fatal(err)
		}
		if got := PlanReconcile(pol, []AttemptFacts{await}, reopen()); len(got) != 0 {
			t.Fatalf("failed run retried automatically: %v", got)
		}
	})
	t.Run("manual run at head blocks automatic", func(t *testing.T) {
		runs := []Report{{AttemptID: "a", Commit: sha1, State: StateReady, RunKey: "rk-m"}}
		if got := PlanReconcile(pol, []AttemptFacts{await}, runs); len(got) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("one in flight", func(t *testing.T) {
		runs := []Report{{AttemptID: "a", Commit: sha2, State: StateRunning, RunKey: "rk-x", Auto: true}}
		if got := PlanReconcile(pol, []AttemptFacts{await}, runs); len(got) != 0 {
			t.Fatal(got)
		}
	})
	t.Run("max auto runs counts automatic only", func(t *testing.T) {
		auto := func(n int, isAuto bool) []Report {
			var rs []Report
			for i := 0; i < n; i++ {
				rs = append(rs, Report{AttemptID: "a", Commit: strings.Repeat(string(rune('d'+i)), 40)[:0] + strings.Repeat("0", 39) + string(rune('0'+i)), State: StateReady, RunKey: "rk-" + string(rune('a'+i)), Auto: isAuto})
			}
			return rs
		}
		if got := PlanReconcile(pol, []AttemptFacts{await}, auto(3, true)); len(got) != 0 {
			t.Fatal("max exceeded", got)
		}
		if got := PlanReconcile(pol, []AttemptFacts{await}, auto(2, true)); len(got) != 1 {
			t.Fatal("under max", got)
		}
		if got := PlanReconcile(pol, []AttemptFacts{await}, auto(5, false)); len(got) != 1 {
			t.Fatal("manual runs counted", got)
		}
	})
	t.Run("new commit", func(t *testing.T) {
		nc := pol
		nc.Trigger = TriggerNewCommit
		prior := []Report{{AttemptID: "a", Commit: sha1, State: StateReady, RunKey: "rk-1", Auto: true}}
		moved := att("a", AttemptStateRunning, sha2)
		if got := PlanReconcile(nc, []AttemptFacts{moved}, prior); len(got) != 1 || got[0].Commit != sha2 || got[0].Trigger != TriggerNewCommit {
			t.Fatalf("%+v", got)
		}
		if got := PlanReconcile(nc, []AttemptFacts{moved}, nil); len(got) != 0 {
			t.Fatal("running attempt never reviewed must wait", got)
		}
		if got := PlanReconcile(nc, []AttemptFacts{att("a", AttemptStateAwaitingReview, sha1)}, prior); len(got) != 0 {
			t.Fatal("same head re-reviewed", got)
		}
		if got := PlanReconcile(pol, []AttemptFacts{moved}, prior); len(got) != 0 {
			t.Fatal("awaiting-review trigger reacted to a running attempt", got)
		}
		if got := PlanReconcile(nc, []AttemptFacts{{AttemptID: "a", State: AttemptStateAwaitingReview, HeadSHA: sha2, WorktreeOK: true}}, prior); len(got) != 1 {
			t.Fatal("moved awaiting attempt", got)
		}
	})
}

func TestRunKeyDiffersByKindAndNonce(t *testing.T) {
	a := RunKey("a", sha1, TriggerAwaitingReview, "")
	if a == RunKey("a", sha1, TriggerNewCommit, "") || a == RunKey("a", sha2, TriggerAwaitingReview, "") ||
		RunKey("a", sha1, TriggerManual, "r1") == RunKey("a", sha1, TriggerManual, "r2") || a != RunKey("a", sha1, TriggerAwaitingReview, "") {
		t.Fatal("run key")
	}
}

func TestStoreConcurrentRecordRun(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	done := make(chan Report, 8)
	for i := 0; i < 8; i++ {
		go func() {
			r, _, err := s.RecordRun(ctx, local, runIn("att", sha1, "same", true))
			if err != nil {
				t.Error(err)
			}
			done <- r
		}()
	}
	first := <-done
	for i := 1; i < 8; i++ {
		if r := <-done; r.ID != first.ID {
			t.Fatal("duplicate runs recorded")
		}
	}
	if snap, _ := s.Load(ctx, local); len(snap.Reports) != 1 {
		t.Fatalf("%d reports", len(snap.Reports))
	}
}

// Acceptance, merge and cleanup are distinct acts: this package may only record a human decision.
func TestExportedAPIHasNoMergeCloseDeleteCleanup(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"merge", "close", "delete", "remove", "cleanup", "prune", "push", "exec", "kill", "stop", "abandon"}
	for _, p := range pkgs {
		for _, f := range p.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				var name *ast.Ident
				switch x := n.(type) {
				case *ast.FuncDecl:
					name = x.Name
				case *ast.TypeSpec:
					name = x.Name
				case *ast.ValueSpec:
					for _, id := range x.Names {
						checkName(t, id, forbidden)
					}
				}
				if name != nil {
					checkName(t, name, forbidden)
				}
				return true
			})
		}
	}
}

func checkName(t *testing.T, id *ast.Ident, forbidden []string) {
	t.Helper()
	if !id.IsExported() {
		return
	}
	l := strings.ToLower(id.Name)
	for _, w := range forbidden {
		if strings.Contains(l, w) {
			t.Errorf("exported %s suggests %q; the review package only records decisions", id.Name, w)
		}
	}
}

// The consent rule: only the local state file can enable automatic reviews. The preference type must not appear
// in any Garden or GitHub-document code, so a remote body can never be decoded into it.
func TestReviewPolicyNeverInRemoteStructs(t *testing.T) {
	roots := []string{"..", filepath.Join("..", "..", "..", "git", "github")}
	for _, root := range roots {
		_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			if abs, _ := filepath.Abs(p); strings.Contains(abs, string(filepath.Separator)+"review"+string(filepath.Separator)) {
				return nil
			}
			b, _ := os.ReadFile(p)
			for _, w := range []string{"GardenReviewPrefs", "garden_review"} {
				if strings.Contains(string(b), w) {
					t.Errorf("%s mentions %s", p, w)
				}
			}
			return nil
		})
	}
}

func TestHostileRemoteDocumentCannotCarryPolicy(t *testing.T) {
	// A report file or store carrying a policy is rejected, not applied.
	if _, err := ParseReportFile([]byte(`{"schema":1,"summary":"s","findings":[],"garden_review":{"enabled":true,"account_id":"x"}}`), Expect{Commit: sha1, Attempt: "a"}); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("%v", err)
	}
	s := newStore(t)
	if err := os.WriteFile(s.Path, []byte(`{"schema_version":1,"repos":{},"garden_review":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background(), local); err == nil {
		t.Fatal("store accepted a policy field")
	}
}

func TestRecordRunRechecksAutomaticBudgetAfterAnotherProcess(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	// These requests could have been planned from the same old snapshot. Finish
	// the first before the second is recorded, so the in-flight guard cannot
	// substitute for enforcing the consent budget under the cross-process lock.
	in := runIn("att", sha1, "auto-one", true)
	in.MaxAutoRuns = 1
	r, _, err := s.RecordRun(ctx, local, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MarkFailed(ctx, local, r.ID, r.Revision, FailTimeout); err != nil {
		t.Fatal(err)
	}
	other := &FileStore{Path: s.Path}
	next := runIn("att", sha2, "auto-two", true)
	next.MaxAutoRuns = 1
	if _, _, err = other.RecordRun(ctx, local, next); !errors.Is(err, ErrAutoLimit) {
		t.Fatalf("stale planned request bypassed consent budget: %v", err)
	}
	if _, created, err := other.RecordRun(ctx, local, in); err != nil || created {
		t.Fatalf("replay at limit: created=%v err=%v", created, err)
	}
	manual := runIn("att", sha2, "manual-retry", false)
	manual.MaxAutoRuns = 1
	if _, created, err := other.RecordRun(ctx, local, manual); err != nil || !created {
		t.Fatalf("explicit retry: created=%v err=%v", created, err)
	}
}
