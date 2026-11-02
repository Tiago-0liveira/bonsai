package agentactivity

import "errors"

var (
	// ErrLineTooLong means a spool line would exceed MaxLineBytes, which Tail
	// would ignore.
	ErrLineTooLong = errors.New("agentactivity: spool line too long")
	// ErrShortWrite means the single spool write did not write every byte.
	ErrShortWrite = errors.New("agentactivity: short spool write")
	// ErrNotRegular means the spool path opened to something other than a
	// regular file.
	ErrNotRegular = errors.New("agentactivity: spool is not a regular file")
	// ErrSubscriberPanic is reported through Bus.OnProblem when a subscriber
	// callback panics.
	ErrSubscriberPanic = errors.New("agentactivity: subscriber panicked")
	// ErrQueueOverflow is reported through Bus.OnProblem when a slow
	// subscriber loses its oldest event.
	ErrQueueOverflow = errors.New("agentactivity: subscriber queue overflow, oldest event dropped")
)
