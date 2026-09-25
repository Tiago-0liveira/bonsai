package pkgmgr

// Confidence communicates how certain Bonsai is about discovered metadata.
type Confidence string

const (
	ConfidenceExact   Confidence = "exact"
	ConfidenceHigh    Confidence = "high"
	ConfidencePartial Confidence = "partial"
	ConfidenceUnknown Confidence = "unknown"
)
