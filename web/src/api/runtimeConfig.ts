// Runtime configuration injected into index.html by whoever serves the page:
// the local Bonsai API (`bonsai web`) or the hosted deployment. Values here
// override the build-time VITE_* environment.

const deployPlaceholder = /^__[A-Z0-9_]+__$/

// The content of a runtime-config meta tag. Undefined when the tag is absent
// or still holds its deploy-time placeholder. An empty string is a real value
// (for example, "no relay").
export function runtimeMeta(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined
  const content = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content
  if (content === undefined) return undefined
  const value = content.trim()
  return deployPlaceholder.test(value) ? undefined : value
}

export type BonsaiEntry = 'local' | 'hosted'

// Which server delivered this page: the local API itself, or a hosted origin.
export function runtimeEntry(): BonsaiEntry {
  return runtimeMeta('bonsai-entry') === 'local' ? 'local' : 'hosted'
}
