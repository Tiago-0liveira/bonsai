package garden

import (
	"errors"
	"time"
)

var (
	ErrInvalidID         = errors.New("invalid garden identity")
	ErrUnsupportedSchema = errors.New("unsupported garden metadata schema")
	ErrInvalidMetadata   = errors.New("invalid garden metadata")
	ErrInvalidRevision   = errors.New("invalid garden revision")
	ErrInvalidApproval   = errors.New("invalid garden approval")
	ErrNotFound          = errors.New("garden item not found")
	ErrInvalidCursor     = errors.New("invalid garden cursor")
	ErrPermissionDenied  = errors.New("garden permission denied")
	// ErrOffline lets read callers distinguish a network outage from malformed data.
	ErrOffline         = errors.New("garden source unavailable offline")
	ErrTooManyComments = errors.New("too many garden comments")
	// ErrAuthRequired reports a rejected or missing credential (HTTP 401).
	ErrAuthRequired = errors.New("garden authentication required")
	// ErrRateLimited reports primary or secondary rate limiting. Use RateLimitError to carry a retry hint.
	ErrRateLimited = errors.New("garden source rate limited")
	// ErrForbidden reports a 403 that is not proven to be a rate limit. It is uncertain evidence: it is
	// never proof that repository access was revoked.
	ErrForbidden = errors.New("garden request forbidden")
	// ErrGraphQL reports an unclassified GraphQL error entry.
	ErrGraphQL = errors.New("garden graphql error")
	// ErrCommentTooLarge reports a comment body over the adapter payload limit.
	ErrCommentTooLarge = errors.New("garden comment exceeds payload limit")
)

// RateLimitError is a value error carrying the server's retry hint. It satisfies errors.Is(err, ErrRateLimited).
type RateLimitError struct{ RetryAfter time.Duration }

func (e RateLimitError) Error() string        { return ErrRateLimited.Error() }
func (e RateLimitError) Is(target error) bool { return target == ErrRateLimited }
