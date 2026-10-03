import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { create } from 'zustand'
import { FilesPage } from './FilesPage'
import { useBonsaiStore } from '../../stores/bonsai'
import * as filesAPI from '../../api/files'

const contentStore = create(() => ({ content: 'initial content' }))
vi.mock('../../api/files', async original => {
  const api = await original<typeof import('../../api/files')>()
  const files = api.fileTree([{ path: 'src/one.ts', status: '' }, { path: 'src/two.ts', status: '' }])
  return { ...api, useFiles: vi.fn(() => files), useFileContent: function useFileContent() { return contentStore(state => state.content) } }
})
afterEach(cleanup)
it('keeps navigation mounted without rerunning its parent file derivation when content or selection changes', () => {
  useBonsaiStore.setState({ selectedFilePath: 'src/one.ts' })
  render(<FilesPage />)
  const baseline = vi.mocked(filesAPI.useFiles).mock.calls.length
  const folder = screen.getByRole('button', { name: 'src' })
  act(() => contentStore.setState({ content: 'updated fetched content' }))
  expect(screen.getByText('updated fetched content')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'two.ts' }))
  expect(useBonsaiStore.getState().selectedFilePath).toBe('src/two.ts')
  expect(vi.mocked(filesAPI.useFiles)).toHaveBeenCalledTimes(baseline)
  expect(screen.getByRole('button', { name: 'src' })).toBe(folder)
})
