package garden

import "fmt"

// LaneKind names the user-facing lane projection.
type LaneKind string

const (
	// LaneDraft contains managed items without acceptance.
	LaneDraft LaneKind = "draft"
	// LaneWork contains currently accepted specifications.
	LaneWork LaneKind = "work"
	// LaneAttention contains conflicts or invalid projections.
	LaneAttention LaneKind = "attention"
)

// LaneResult is the lane and reason derived from verified state.
type LaneResult struct {
	Lane   LaneKind
	Reason string
}

// Lane derives placement from labels, decoded metadata and append-only verification state.
func Lane(c Classification, m Metadata, v Verification, metadataErr error) LaneResult {
	if !v.Complete {
		return LaneResult{LaneAttention, "comment log is incomplete"}
	}
	if len(v.Problems) > 0 {
		return LaneResult{LaneAttention, "revision or approval integrity problem"}
	}
	if metadataErr != nil {
		return LaneResult{LaneAttention, fmt.Sprintf("invalid managed metadata: %v", metadataErr)}
	}
	if err := validateMetadata(m); err != nil {
		return LaneResult{LaneAttention, err.Error()}
	}
	if len(c.Problems) > 0 {
		return LaneResult{LaneAttention, "invalid managed labels"}
	}
	if len(v.ConflictingHeads) > 1 {
		return LaneResult{LaneAttention, "competing revision heads"}
	}
	if v.AcceptedSpecHash == "" || len(v.ValidApprovals) == 0 {
		if m.Accepted || len(v.SupersededApprovals) > 0 || len(v.RevokedApprovals) > 0 || c.Stage == StageReady {
			return LaneResult{LaneAttention, "accepted specification lacks a valid approval"}
		}
		return LaneResult{LaneDraft, "specification has not been accepted"}
	}
	if c.Stage == StageDraft && !m.Accepted {
		return LaneResult{LaneDraft, "approval stage projection is incomplete"}
	}
	return LaneResult{LaneWork, "specification acceptance is recorded"}
}
