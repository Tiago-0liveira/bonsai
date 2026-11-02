package ui

import "github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"

// WithPlayground injects changes, pull request and CI reads, plus optional
// ReviewBackend support. Without it the Playground still lists sessions and
// supports terminal attachment; changes and review are unavailable.
func WithPlayground(b playgroundapi.Backend) Option {
	return func(m *Model) { m.pg.api = b }
}
