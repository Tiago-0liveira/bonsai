// Package runtime keeps a narrow compatibility wrapper for callers that used
// the original local API entrypoint. Browser-facing implementation lives in
// internal/server/localapi.
package runtime

import "github.com/Tiago-0liveira/bonsai/internal/server/localapi"

const ProductionBrowserOrigin = localapi.ProductionBrowserOrigin

// RunAPI starts the local API. The optional value is accepted only for source
// compatibility with the pre-refactor capability-file argument; string values
// are intentionally ignored. A bool enables the explicit development-origin
// mode used by local frontend development.
func RunAPI(repoDir, address, browserOrigin string, option ...any) error {
	development := false
	for _, value := range option {
		if enabled, ok := value.(bool); ok {
			development = enabled
		}
	}
	return localapi.Run(localapi.Config{
		RepoDir:       repoDir,
		Address:       address,
		BrowserOrigin: browserOrigin,
		Development:   development,
	})
}
