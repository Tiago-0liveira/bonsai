import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { EditorPreferenceDialog } from './EditorPreferenceDialog'
import { useBonsaiStore } from '../../stores/bonsai'

afterEach(cleanup)

const settings = (preferred: string) => ({
  environment: { id: 'linux:host', label: 'Linux' }, revision: 0, preferred, saved_preferred: preferred,
  editors: [
    { id: 'kate', name: 'Kate', command: '/usr/bin/kate', folders: false },
    { id: 'zed', name: 'Zed', command: '/usr/bin/zed', folders: true },
  ],
})

it('lists only folder-capable editors when opening a worktree folder', () => {
  useBonsaiStore.setState({ editorSettings: settings(''), editorPromptOpen: true, pendingOpenFile: { worktreeId: 'wt', path: '', folder: true } })
  render(<EditorPreferenceDialog />)
  expect(screen.getByRole('dialog', { name: 'Open worktree folder with…' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Zed' })).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Kate' })).toBeNull()
  expect(screen.getByText(/opens the folder and saves your choice/)).toBeVisible()
})

it('says the default is unchanged when the folder choice applies once', () => {
  useBonsaiStore.setState({ editorSettings: settings('kate'), editorPromptOpen: true, pendingOpenFile: { worktreeId: 'wt', path: '', folder: true } })
  render(<EditorPreferenceDialog />)
  expect(screen.getByText(/Your default editor for this environment is unchanged/)).toBeVisible()
})

it('still lists every editor when opening a file', () => {
  useBonsaiStore.setState({ editorSettings: settings(''), editorPromptOpen: true, pendingOpenFile: { worktreeId: 'wt', path: 'README.md' } })
  render(<EditorPreferenceDialog />)
  expect(screen.getByRole('button', { name: 'Kate' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Zed' })).toBeVisible()
})
