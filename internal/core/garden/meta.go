package garden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const SchemaVersion = 1
const MaxMetadataBytes = 64 << 10

// Metadata is the versioned, explicitly owned Garden envelope stored on an Issue.
type Metadata struct {
	SchemaVersion    int                `json:"schema_version"`
	Provenance       string             `json:"provenance"`
	Owner            string             `json:"owner"`
	Accepted         bool               `json:"accepted"`
	AcceptedSpecHash string             `json:"accepted_spec_hash,omitempty"`
	SpecFingerprint  string             `json:"spec_fingerprint,omitempty"`
	CurrentPlanHash  string             `json:"current_plan_hash,omitempty"`
	Revocations      []RevocationRecord `json:"revocations,omitempty"`
}

// RevocationRecord preserves the comment IDs needed to detect deleted revocations.
type RevocationRecord struct {
	ApprovalCommentID   int64 `json:"approval_comment_id"`
	RevocationCommentID int64 `json:"revocation_comment_id"`
}

// EncodeMetadata rejects unsupported schemas so old clients cannot rewrite away future fields.
func EncodeMetadata(m Metadata) ([]byte, error) {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = SchemaVersion
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedSchema, m.SchemaVersion)
	}
	if err := validateMetadata(m); err != nil {
		return nil, err
	}
	b, e := json.Marshal(m)
	if e != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidMetadata, e)
	}
	if len(b) > MaxMetadataBytes {
		return nil, fmt.Errorf("%w: metadata too large", ErrInvalidMetadata)
	}
	return b, nil
}

func validateMetadata(m Metadata) error {
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
	if m.Provenance == "" || m.Owner == "" {
		return fmt.Errorf("%w: provenance and owner required", ErrInvalidMetadata)
	}
	if m.Accepted {
		if !validHash(m.AcceptedSpecHash) || !validHash(m.SpecFingerprint) {
			return fmt.Errorf("%w: accepted metadata requires valid accepted spec hash and fingerprint", ErrInvalidMetadata)
		}
	} else if m.AcceptedSpecHash != "" {
		return fmt.Errorf("%w: unaccepted metadata has accepted spec hash", ErrInvalidMetadata)
	}
	if !m.Accepted && m.SpecFingerprint != "" {
		return fmt.Errorf("%w: unaccepted metadata has spec fingerprint", ErrInvalidMetadata)
	}
	if len(m.Revocations) > 2000 {
		return fmt.Errorf("%w: too many revocations", ErrInvalidMetadata)
	}
	seenRev, seenApproval := map[int64]bool{}, map[int64]bool{}
	for _, r := range m.Revocations {
		if r.ApprovalCommentID <= 0 || r.RevocationCommentID <= 0 || seenRev[r.RevocationCommentID] || seenApproval[r.ApprovalCommentID] {
			return fmt.Errorf("%w: invalid revocation projection", ErrInvalidMetadata)
		}
		seenRev[r.RevocationCommentID] = true
		seenApproval[r.ApprovalCommentID] = true
	}
	if m.CurrentPlanHash != "" && !validHash(m.CurrentPlanHash) {
		return fmt.Errorf("%w: invalid current plan hash", ErrInvalidMetadata)
	}
	return nil
}

// rejectDuplicateKeys scans every object before decoding so parser disagreement cannot
// make two occurrences of a security relevant field mean different things.
func rejectDuplicateKeys(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, err := d.Token()
					if err != nil {
						return err
					}
					key, ok := k.(string)
					if !ok {
						return fmt.Errorf("invalid object key")
					}
					if seen[key] {
						return fmt.Errorf("duplicate key %q", key)
					}
					seen[key] = true
					if err := value(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			case '[':
				for d.More() {
					if err := value(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			}
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing value")
		}
		return err
	}
	return nil
}

// DecodeMetadata bounds input and rejects unknown fields and trailing JSON.
func DecodeMetadata(b []byte) (Metadata, error) {
	var m Metadata
	if len(b) > MaxMetadataBytes {
		return m, fmt.Errorf("%w: metadata too large", ErrInvalidMetadata)
	}
	if !utf8.Valid(b) {
		return m, fmt.Errorf("%w: invalid UTF-8", ErrInvalidMetadata)
	}
	if err := rejectDuplicateKeys(b); err != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return m, fmt.Errorf("%w: expected object envelope", ErrInvalidMetadata)
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if e := json.Unmarshal(b, &head); e != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidMetadata, e)
	}
	if head.SchemaVersion != SchemaVersion {
		return m, fmt.Errorf("%w: %d", ErrUnsupportedSchema, head.SchemaVersion)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&m); e != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidMetadata, e)
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return m, fmt.Errorf("%w: trailing data", ErrInvalidMetadata)
	}
	if err := validateMetadata(m); err != nil {
		return m, err
	}
	return m, nil
}
