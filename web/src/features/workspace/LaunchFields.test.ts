import { expect, it } from 'vitest'
import type { LaunchField } from '../../api/agents'
import { chosenModel, fieldOptions, initialLaunchValues, launchRequestOptions, launchSummary, resolveLaunchValues } from './LaunchFields'

const fields: LaunchField[] = [
  { id: 'permissions', label: 'Permissions', kind: 'select', default: 'auto', options: [
    { value: 'read-only', label: 'Read only', set: { sandbox: 'read-only', approval: 'on-request' } },
    { value: 'auto', label: 'Auto', set: { sandbox: 'workspace-write', approval: 'on-request' } },
  ] },
  { id: 'effort', label: 'Effort', kind: 'select', per_model: true, options: [{ value: '', label: 'Default' }, { value: 'high', label: 'high' }] },
  { id: 'mode', label: 'Mode', kind: 'select', options: [{ value: 'a', label: 'A' }, { value: 'b', label: 'B' }] },
  { id: 'full_access', label: 'Full access', kind: 'toggle', options: [{ value: 'false', label: 'Ask' }, { value: 'true', label: 'Full', tone: 'danger' }] },
]
const models = [{ id: 'm', label: 'M', source: 'api' as const, default: true, efforts: ['low', 'max'] }, { id: 'plain', label: 'Plain', source: 'alias' as const }]

it('starts selects at their default or first option and toggles at the profile value', () => {
  expect(initialLaunchValues(fields)).toEqual({ permissions: 'auto', effort: '', mode: 'a', full_access: false })
  expect(initialLaunchValues(fields, { id: 'p', name: 'P', provider: 'claude', full_access: true }).full_access).toBe(true)
  expect(initialLaunchValues(fields, { id: 'p', name: 'P', provider: 'claude', full_access: 'yes' }).full_access).toBe(false)
})

it('sends preset fields, skips empty selects and always sends toggles', () => {
  expect(launchRequestOptions(fields, { permissions: 'read-only', effort: '', mode: 'b', full_access: false })).toEqual({ sandbox: 'read-only', approval: 'on-request', mode: 'b', full_access: false })
  expect(launchRequestOptions([], { stale: 'x' })).toEqual({})
})

it('takes per-model values from the chosen or default model and drops unsupported ones', () => {
  expect(chosenModel(models, '')?.id).toBe('m')
  expect(chosenModel(models, ' plain ')?.id).toBe('plain')
  expect(chosenModel(models, 'typed')).toBeUndefined()
  expect(fieldOptions(fields[1], models[0]).map(option => option.value)).toEqual(['', 'low', 'max'])
  expect(fieldOptions(fields[1], models[1]).map(option => option.value)).toEqual(['', 'high'])
  expect(resolveLaunchValues(fields, { permissions: 'auto', effort: 'high', mode: 'gone' }, models[0])).toMatchObject({ effort: '', mode: 'a' })
  expect(resolveLaunchValues(fields, { effort: 'max' }, models[0]).effort).toBe('max')
})

it('summarises chosen values with readable labels', () => {
  expect(launchSummary(fields, { permissions: 'auto', effort: 'max', mode: 'b', full_access: true }, models[0])).toEqual(['Auto', 'max effort', 'B', 'Full'])
  expect(launchSummary(fields, { effort: '', full_access: 'no' })).toEqual([])
})
