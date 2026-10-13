import { describe, expect, it } from 'vitest'
import type { LaunchField } from '../../api/agents'
import { NAME_RULE, profileChanges, profileFormValues, profileNameProblem } from './profileForm'

const fields: LaunchField[] = [
  { id: 'model', label: 'Model', kind: 'model' },
  { id: 'permissions', label: 'Permissions', kind: 'select', options: [{ value: '', label: 'Provider default' }, { value: 'auto', label: 'Auto' }] },
  { id: 'full_access', label: 'Full access', kind: 'toggle', options: [{ value: 'false', label: 'Ask' }, { value: 'true', label: 'Full', tone: 'danger' }] },
]

describe('profileFormValues', () => {
  it('starts selects and the model at the saved value or empty, toggles on only for "true"', () => {
    expect(profileFormValues(fields, { model: 'gpt-6.1-sol', permissions: 'auto', full_access: 'true' })).toEqual({ model: 'gpt-6.1-sol', permissions: 'auto', full_access: true })
    expect(profileFormValues(fields, undefined)).toEqual({ model: '', permissions: '', full_access: false })
    expect(profileFormValues(fields, { full_access: 'false' }).full_access).toBe(false)
  })
})

describe('profileChanges', () => {
  const saved = { model: 'opus', permissions: 'auto' }
  it('is empty while the form equals the saved defaults', () => {
    expect(profileChanges(fields, saved, profileFormValues(fields, saved))).toEqual({})
  })
  it('sends only changed fields, with null for a field returned to the provider default', () => {
    expect(profileChanges(fields, saved, { model: 'sonnet', permissions: 'auto', full_access: false })).toEqual({ model: 'sonnet' })
    expect(profileChanges(fields, saved, { model: '', permissions: '', full_access: true })).toEqual({ model: null, permissions: null, full_access: 'true' })
  })
  it('turns a saved toggle off with null and ignores surrounding whitespace in the model', () => {
    expect(profileChanges(fields, { full_access: 'true' }, { model: '', permissions: '', full_access: false })).toEqual({ full_access: null })
    expect(profileChanges(fields, { model: 'opus' }, { model: ' opus ', permissions: '', full_access: false })).toEqual({})
    expect(profileChanges(fields, undefined, { model: '   ', permissions: '', full_access: false })).toEqual({})
  })
})

describe('profileNameProblem', () => {
  it('mirrors the API name rule', () => {
    for (const name of ['work', 'Work-2', 'a.b_c', '9lives', 'x'.repeat(64)]) expect(profileNameProblem(name, [])).toBe('')
    for (const name of ['', ' work', 'work space', '-work', '.work', 'wörk', 'x'.repeat(65)]) expect(profileNameProblem(name, [])).toBe(NAME_RULE)
  })
  it('rejects names another profile of the provider already has', () => {
    expect(profileNameProblem('Team', ['Team'])).toMatch(/already has this name/)
    expect(profileNameProblem('team', ['Team'])).toBe('')
  })
})
