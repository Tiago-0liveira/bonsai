package garden

import (
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
