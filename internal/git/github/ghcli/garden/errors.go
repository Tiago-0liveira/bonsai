package garden

import "errors"

var (
	// ErrNilHTTPClient reports a missing transport; the adapter never falls back to http.DefaultClient.
	ErrNilHTTPClient = errors.New("garden github adapter requires an http client")
	// ErrUnsupportedBase reports an API base other than a bare https origin (for example GitHub Enterprise
	// /api/v3), which this phase does not support.
	ErrUnsupportedBase = errors.New("garden github api base is not supported")
	// ErrTruncated reports a bounded listing that still had more results; it is never read as complete.
	ErrTruncated = errors.New("garden github result truncated")
)
