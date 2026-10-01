export function connectionLabel(reason?: string, sha?: string, statusUnknown = false) {
  const labels: Record<string, string> = { no_upstream: 'No upstream', upstream_missing: 'Upstream missing', detached_head: 'Detached HEAD', unborn_branch: 'Unborn branch', branch_ref_missing: 'Branch ref missing', status_unavailable: 'Status unavailable' }
  const label = labels[reason ?? ''] ?? ''
  const detail = reason === 'detached_head' && sha ? `${label} · ${sha.slice(0, 7)}` : label
  return statusUnknown && reason !== 'status_unavailable' ? `${detail ? detail + ' · ' : ''}Status unavailable` : detail
}
