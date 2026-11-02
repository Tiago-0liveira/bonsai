package garden

import (
	"errors"
	"strings"
	"testing"
)

func TestCaptureBodyRoundTrip(t *testing.T) {
	nonce := strings.Repeat("ab", 16)
	body, err := CaptureBody("1001", nonce, "line one\nline two")
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeMetadata(BodyMetadata(body))
	if err != nil || m.Accepted || m.Owner != "1001" || m.Provenance != ProvenanceCapture {
		t.Fatalf("metadata = %+v, %v", m, err)
	}
	if got := CaptureNonce(body); got != nonce {
		t.Fatalf("nonce = %q", got)
	}
	if got := DisplayBody(body); got != "line one\nline two" {
		t.Fatalf("display = %q", got)
	}
	if got := CaptureNonce(strings.ReplaceAll(body, "\n", "\r\n")); got != nonce {
		t.Fatalf("CRLF nonce = %q", got)
	}
}

func TestCaptureBodyRejectsBadNonceAndForgedMarkers(t *testing.T) {
	for _, n := range []string{"", "short", strings.Repeat("A", 32), strings.Repeat("g", 32), strings.Repeat("a", 33)} {
		if _, err := CaptureBody("1", n, ""); err == nil {
			t.Errorf("nonce %q accepted", n)
		}
	}
	good := strings.Repeat("c", 32)
	for name, body := range map[string]string{
		"first line":      CapturePrefix + good + markerSuffix + "\nrest",
		"third line":      "meta\nother\n" + CapturePrefix + good + markerSuffix,
		"inline":          "meta\nx " + CapturePrefix + good + markerSuffix,
		"bad nonce chars": "meta\n" + CapturePrefix + strings.Repeat("z", 32) + markerSuffix,
		"empty":           "",
	} {
		if got := CaptureNonce(body); got != "" {
			t.Errorf("%s: nonce %q", name, got)
		}
	}
}

func TestDisplayBodyKeepsUserText(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                         "plain",
		MetadataPrefix + "{} -->\nuser": "user",
		MetadataPrefix + "{} -->\n" + CapturePrefix + strings.Repeat("a", 32) + " -->\nuser\nmore": "user\nmore",
		CapturePrefix + strings.Repeat("a", 32) + " -->\nuser":                                     CapturePrefix + strings.Repeat("a", 32) + " -->\nuser",
	} {
		if got := DisplayBody(in); got != want {
			t.Errorf("DisplayBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBodyMetadataOnlyReadsTheFirstLine(t *testing.T) {
	if BodyMetadata("text\n"+MetadataPrefix+`{"a":1} -->`) != nil {
		t.Fatal("read metadata from a later line")
	}
	if BodyMetadata(MetadataPrefix+"{not json -->") != nil {
		t.Fatal("accepted invalid JSON")
	}
	if got := BodyMetadata(MetadataPrefix + `{"a":1} -->` + "\r\nrest"); string(got) != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceBodyMetadataKeepsEverythingElse(t *testing.T) {
	body, err := CaptureBody("1001", strings.Repeat("b", 32), "line one\r\nline two")
	if err != nil {
		t.Fatal(err)
	}
	next, err := EncodeMetadata(Metadata{Provenance: ProvenanceCapture, Owner: "1001", Accepted: true, AcceptedSpecHash: strings.Repeat("a", 64), SpecFingerprint: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReplaceBodyMetadata(body, next)
	if err != nil {
		t.Fatal(err)
	}
	if string(BodyMetadata(got)) != string(next) || DisplayBody(got) != DisplayBody(body) || CaptureNonce(got) != CaptureNonce(body) {
		t.Fatalf("body = %q", got)
	}
	crlf := strings.Replace(body, "\n", "\r\n", 1)
	if got, err := ReplaceBodyMetadata(crlf, next); err != nil || !strings.Contains(got, markerSuffix+"\r\n") {
		t.Fatalf("CRLF lost: %q %v", got, err)
	}
	for name, in := range map[string]string{"no metadata line": "just text", "empty": ""} {
		if _, err := ReplaceBodyMetadata(in, next); !errors.Is(err, ErrInvalidMetadata) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := ReplaceBodyMetadata(body, []byte("not json")); !errors.Is(err, ErrInvalidMetadata) {
		t.Errorf("bad replacement: %v", err)
	}
	only := MetadataPrefix + `{"schema_version":1}` + markerSuffix
	if got, err := ReplaceBodyMetadata(only, next); err != nil || strings.Contains(got, "\n") {
		t.Errorf("single line body gained a newline: %q %v", got, err)
	}
}

func TestNewApprovalBindsRevisionAndRejectsOtherKinds(t *testing.T) {
	id := IssueIdentity{Repo: 9, Issue: 7}
	rev := Revision{Kind: RevisionSpec, Hash: strings.Repeat("c", 64), SourceSpecFingerprint: strings.Repeat("c", 64), CommentID: 11}
	body, err := NewApproval(id, rev, "Approved.")
	if err != nil {
		t.Fatal(err)
	}
	e, text, err := decodeEnvelope(body)
	if err != nil || e.Kind != CommentApproval || e.RevisionHash != rev.Hash || e.RevisionCommentID != 11 || e.RecordHash != ContentHash(text) {
		t.Fatalf("envelope = %+v %v", e, err)
	}
	rev.Kind = RevisionPlan
	if _, err := NewApproval(id, rev, "x"); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("plan approval: %v", err)
	}
	if _, err := NewApproval(IssueIdentity{}, Revision{Kind: RevisionSpec}, "x"); !errors.Is(err, ErrInvalidApproval) {
		t.Fatalf("empty target: %v", err)
	}
}
