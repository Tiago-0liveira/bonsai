package garden

import "errors"

var (
	ErrInvalidID         = errors.New("invalid garden identity")
	ErrUnsupportedSchema = errors.New("unsupported garden metadata schema")
	ErrInvalidMetadata   = errors.New("invalid garden metadata")
	ErrInvalidRevision   = errors.New("invalid garden revision")
	ErrInvalidApproval   = errors.New("invalid garden approval")
	ErrNotFound          = errors.New("garden item not found")
	ErrInvalidCursor     = errors.New("invalid garden cursor")
	ErrPermissionDenied  = errors.New("garden permission denied")
	ErrTooManyComments   = errors.New("too many garden comments")
)
