package garden

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MetadataPrefix opens the first body line that carries the Issue metadata envelope.
const MetadataPrefix = "<!-- bonsai:garden-metadata/v1 "

// CapturePrefix opens the second body line of an Issue Bonsai created. Its nonce
// is how an ambiguous create is found again without trusting titles.
const CapturePrefix = "<!-- bonsai:garden-capture/v1 "

const markerSuffix = " -->"

// ProvenanceCapture and ProvenanceImport name how an Issue entered the Garden.
const (
	ProvenanceCapture = "bonsai-capture"
	ProvenanceImport  = "import"
)

// MaxDescriptionBytes bounds the optional description of a captured idea, far under the adapter payload limit.
const MaxDescriptionBytes = 32 << 10

// CaptureBody renders the body of a new idea: metadata line, nonce line, then the user's text.
func CaptureBody(owner, nonce, description string) (string, error) {
	if !validNonce(nonce) {
		return "", fmt.Errorf("%w: capture nonce", ErrInvalidMetadata)
	}
	meta, err := EncodeMetadata(Metadata{SchemaVersion: SchemaVersion, Provenance: ProvenanceCapture, Owner: owner})
	if err != nil {
		return "", err
	}
	return MetadataPrefix + string(meta) + markerSuffix + "\n" + CapturePrefix + nonce + markerSuffix + "\n" + description, nil
}

// CaptureNonce returns the nonce of a captured body, or "" when the second line is not a capture marker.
func CaptureNonce(body string) string {
	_, line, _ := strings.Cut(body, "\n")
	line, _, _ = strings.Cut(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if !strings.HasPrefix(line, CapturePrefix) || !strings.HasSuffix(line, markerSuffix) {
		return ""
	}
	nonce := strings.TrimSuffix(strings.TrimPrefix(line, CapturePrefix), markerSuffix)
	if !validNonce(nonce) {
		return ""
	}
	return nonce
}

// validNonce accepts the 128-bit lowercase hex form Bonsai generates, so a body can never smuggle another marker in.
func validNonce(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// DisplayBody is the user-owned part of an Issue body: it drops the metadata line and the capture marker.
func DisplayBody(body string) string {
	if strings.HasPrefix(body, MetadataPrefix) {
		_, body, _ = strings.Cut(body, "\n")
		if strings.HasPrefix(body, CapturePrefix) {
			_, body, _ = strings.Cut(body, "\n")
		}
	}
	return body
}

// BodyMetadata reads only the bounded, first-line canonical projection; arbitrary body text is never decoded as metadata.
func BodyMetadata(body string) []byte {
	line := body
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSuffix(line[:i], "\r")
	}
	if !strings.HasPrefix(line, MetadataPrefix) || !strings.HasSuffix(line, markerSuffix) || len(line) > MaxMetadataBytes+len(MetadataPrefix)+len(markerSuffix) {
		return nil
	}
	b := []byte(strings.TrimSuffix(strings.TrimPrefix(line, MetadataPrefix), markerSuffix))
	if len(b) == 0 || len(b) > MaxMetadataBytes || !json.Valid(b) {
		return nil
	}
	return b
}
