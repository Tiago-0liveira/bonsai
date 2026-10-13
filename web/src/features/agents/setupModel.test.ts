import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AgentAccount, SetupSpec } from '../../api/agents'
import {
  accountAddCommandFor, accountLoginCommand, defaultAuthMode, forgetSetupJob, profileNameError, recallSetupJob,
  rememberSetupJob, secretError, setupFingerprint,
} from './setupModel'

const accounts: AgentAccount[] = [
  { id: 'codex-work', provider: 'codex', name: 'work' },
  { id: 'claude-home', provider: 'claude', name: 'home' },
]
const spec: SetupSpec = {
  auth_modes: [
    { value: 'chatgpt', label: 'Browser sign-in' },
    { value: 'device', label: 'Device code', recommended: true },
    { value: 'api-key', label: 'API key', secret: { label: 'OpenAI API key', placeholder: 'sk-…' } },
  ],
  seed: { source: '~/.codex' },
}

afterEach(() => { vi.restoreAllMocks(); sessionStorage.clear() })

describe('profile name', () => {
  it('mirrors the API rule and is unique per provider', () => {
    expect(profileNameError('', 'codex', accounts)).toBe('Enter a name for the profile.')
    for (const bad of ['-work', '.x', 'two words', 'a/b', 'x'.repeat(65)]) expect(profileNameError(bad, 'codex', accounts), bad).toMatch(/letter or digit/)
    for (const good of ['w', '9lives', 'team.ci_2-b', 'x'.repeat(64)]) expect(profileNameError(good, 'codex', accounts), good).toBe('')
    expect(profileNameError('work', 'codex', accounts)).toMatch(/already has this name/)
    // Another provider's profile does not take the name.
    expect(profileNameError('home', 'codex', accounts)).toBe('')
    expect(profileNameError('Work', 'codex', accounts)).toBe('')
  })
})

describe('sign-in method', () => {
  it('starts on the current method, else the recommended one, else the first', () => {
    expect(defaultAuthMode(spec)).toBe('device')
    expect(defaultAuthMode(spec, 'api-key')).toBe('api-key')
    expect(defaultAuthMode(spec, 'gone')).toBe('device')
    expect(defaultAuthMode({ auth_modes: [{ value: 'login', label: 'Browser' }, { value: 'token', label: 'Token' }] })).toBe('login')
    expect(defaultAuthMode({})).toBe('')
    expect(defaultAuthMode(undefined)).toBe('')
  })

  it('requires a secret only for methods that need one', () => {
    expect(secretError(spec.auth_modes![0], '')).toBe('')
    expect(secretError(spec.auth_modes![2], '  ')).toBe('Paste the OpenAI API key.')
    expect(secretError(spec.auth_modes![2], 'sk-1')).toBe('')
    expect(secretError({ value: 'token', label: 'Setup token', secret: { label: 'Token', optional: true } }, '')).toBe('')
  })
})

describe('request fingerprint', () => {
  it('leaves the secret out, so a retry keeps its key whatever was pasted', () => {
    const base = { provider: 'codex', name: 'work', auth_mode: 'api-key', seed: true }
    const withSecret = setupFingerprint('create', { ...base, secret: 'sk-secret-1' })
    expect(withSecret).toBe(setupFingerprint('create', { ...base, secret: 'sk-other' }))
    expect(withSecret).toBe(setupFingerprint('create', base))
    expect(withSecret).not.toContain('sk-secret-1')
    expect(setupFingerprint('create', { ...base, seed: false })).not.toBe(withSecret)
    expect(setupFingerprint('login:a', { auth_mode: undefined })).toBe(setupFingerprint('login:a', {}))
    expect(setupFingerprint('login:a', {})).not.toBe(setupFingerprint('login:b', {}))
  })
})

describe('CLI fallback commands', () => {
  it('names the profile and the chosen method', () => {
    expect(accountAddCommandFor('codex', 'work', 'device')).toBe('bonsai agent account add codex work --auth device')
    expect(accountAddCommandFor('antigravity', '', '')).toBe('bonsai agent account add antigravity <name>')
    expect(accountLoginCommand({ provider: 'claude', name: 'home' })).toBe('bonsai agent account login claude/home')
    expect(accountLoginCommand({ provider: 'claude', name: 'home' }, 'token')).toBe('bonsai agent account login claude/home --auth token')
  })
})

describe('reattach memory', () => {
  it('remembers, recalls and forgets the running job', () => {
    expect(recallSetupJob()).toBeUndefined()
    rememberSetupJob({ id: 'job-1', kind: 'login', accountId: 'codex-work' })
    expect(recallSetupJob()).toEqual({ id: 'job-1', kind: 'login', accountId: 'codex-work' })
    forgetSetupJob('job-2')
    expect(recallSetupJob()?.id).toBe('job-1')
    forgetSetupJob('job-1')
    expect(recallSetupJob()).toBeUndefined()
    sessionStorage.setItem('bonsai.agents.setupJob', '{"id":3}')
    expect(recallSetupJob()).toBeUndefined()
    sessionStorage.setItem('bonsai.agents.setupJob', 'not json')
    expect(recallSetupJob()).toBeUndefined()
  })

  it('works without storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked') })
    expect(() => rememberSetupJob({ id: 'job-1', kind: 'create' })).not.toThrow()
    expect(recallSetupJob()).toBeUndefined()
    expect(() => forgetSetupJob()).not.toThrow()
  })
})
