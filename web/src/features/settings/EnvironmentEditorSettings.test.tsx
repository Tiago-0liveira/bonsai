import { beforeEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { EnvironmentEditorSettings } from './EnvironmentEditorSettings'
import { EditorPreferenceDialog } from '../files/EditorPreferenceDialog'
import { useBonsaiStore } from '../../stores/bonsai'
import { loadEditors, saveEditor, openFileInEditor } from '../../api/editors'

vi.mock('../../api/editors', () => ({ loadEditors: vi.fn(), saveEditor: vi.fn(), openFileInEditor: vi.fn() }))
const settings = {
  environment: { id: 'wsl:host:Ubuntu', label: 'WSL (Ubuntu)' }, revision: 0,
  editors: [{ id: 'zed', name: 'Zed', command: '/usr/bin/zed' }], preferred: '', saved_preferred: '',
}

beforeEach(() => {
  vi.resetAllMocks()
  useBonsaiStore.setState({ editorSettings: null, editorSettingsLoading: false, editorSettingsSaving: false, editorSettingsError: '', editorPromptOpen: false, pendingOpenFile: null })
  vi.mocked(loadEditors).mockResolvedValue(settings)
})

it('lists installed environment editors in Settings and saves the selection', async () => {
  vi.mocked(saveEditor).mockResolvedValue({ ...settings, revision: 1, preferred: 'zed', saved_preferred: 'zed' })
  render(<EnvironmentEditorSettings />)
  expect(await screen.findByRole('option', { name: 'Zed' })).toBeVisible()
  expect(screen.queryByRole('option', { name: 'Cursor' })).toBeNull()
  expect(screen.queryByRole('option', { name: 'System default' })).toBeNull()
  expect(screen.getByText(/Only editors installed inside WSL/)).toBeVisible()
  fireEvent.change(screen.getByRole('combobox', { name: 'Default editor' }), { target: { value: 'zed' } })
  await waitFor(() => expect(saveEditor).toHaveBeenCalledWith(settings, 'zed'))
  await waitFor(() => expect(screen.getByRole('combobox')).toHaveValue('zed'))
})

it('explains how to install an editor when no native editor is available', async () => {
  vi.mocked(loadEditors).mockResolvedValue({ ...settings, editors: [], saved_preferred: 'cursor' })
  render(<EnvironmentEditorSettings />)
  expect(await screen.findByText(/No supported editors were found/)).toBeVisible()
  expect(screen.getByRole('combobox')).toBeDisabled()
  expect(screen.getByText(/Your previous editor is unavailable/)).toBeVisible()
})

it('shows settings failures and supports a manual refresh', async () => {
  vi.mocked(loadEditors).mockRejectedValueOnce(new Error('API offline'))
  render(<EnvironmentEditorSettings />)
  expect(await screen.findByRole('alert')).toHaveTextContent('API offline')
  fireEvent.click(screen.getByRole('button', { name: 'Refresh editors' }))
  expect(await screen.findByRole('option', { name: 'Zed' })).toBeVisible()
  expect(screen.queryByRole('alert')).toBeNull()
})

it('launches the pending file from the dynamic chooser and saves the environment choice', async () => {
  useBonsaiStore.setState({ editorSettings: settings, editorPromptOpen: true, pendingOpenFile: { worktreeId: 'wt-one', path: 'README.md' } })
  vi.mocked(saveEditor).mockResolvedValue({ ...settings, revision: 1, preferred: 'zed', saved_preferred: 'zed' })
  vi.mocked(openFileInEditor).mockResolvedValue({ editor: 'Zed', environment: 'WSL (Ubuntu)' })
  render(<EditorPreferenceDialog />)
  expect(screen.getByRole('dialog', { name: 'Open files with…' })).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Cursor' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Zed' }))
  await waitFor(() => expect(openFileInEditor).toHaveBeenCalledWith('wt-one', 'README.md', settings.environment.id))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})
