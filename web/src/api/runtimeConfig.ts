// Runtime configuration supplied by the local API or contributor harness.
export function runtimeMeta(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined
  return document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content.trim()
}

export function loopbackOrigin(value: string | undefined): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    if (url.protocol !== 'http:' || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
      || url.username || url.password || url.pathname !== '/' || url.search || url.hash) return undefined
    return url.origin
  } catch { return undefined }
}
