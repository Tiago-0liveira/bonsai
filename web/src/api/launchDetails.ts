import { agentProviders, type AgentCapability, type LaunchField, type LaunchFieldOption } from './agents'

// Launch details of a session. Not part of the first screen: the inspector
// imports this module when it shows a live session.

let providersOnce: Promise<AgentCapability[]> | undefined
/** The provider list for labels only: fetched once per page, again after a failure. */
export function cachedAgentProviders(): Promise<AgentCapability[]> {
  providersOnce ??= agentProviders().catch(error => { providersOnce = undefined; throw error })
  return providersOnce
}
/** An option's label; a bare value such as "high" reads better with its field: "high effort". */
export function launchOptionLabel(field: LaunchField, option: LaunchFieldOption): string {
  return option.label === option.value ? `${option.label} ${field.label.toLowerCase()}` : option.label
}
/**
 * A session's launch choices, model first: "gpt-5.5 · high effort · Auto".
 * Values are labelled through the provider's launch fields (a preset shows the
 * option whose fields all match); values no field declares are shown as sent.
 */
export function launchDetails(fields: LaunchField[], model = '', options: Record<string, string> = {}): string[] {
  const out = model ? [model] : []
  const rest = { ...options }
  for (const field of fields) {
    const preset = field.options?.find(option => option.set && Object.entries(option.set).every(([key, value]) => rest[key] === value))
    if (preset?.set) {
      out.push(preset.label)
      for (const key of Object.keys(preset.set)) delete rest[key]
      continue
    }
    const value = rest[field.id]
    if (value === undefined) continue
    delete rest[field.id]
    out.push(launchOptionLabel(field, field.options?.find(option => option.value === value) ?? { value, label: value }))
  }
  return [...out, ...Object.values(rest)]
}
