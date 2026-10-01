import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ProjectRootsSettings } from './ProjectRootsSettings'
import { useBonsaiStore } from '../../stores/bonsai'
import { changeProjectRoot, changeProjectSelection } from '../../api/settings'

vi.mock('../../api/settings', () => ({
  loadProjectRoots: vi.fn().mockResolvedValue(undefined),
  changeProjectRoot: vi.fn().mockResolvedValue(undefined),
  changeProjectSelection: vi.fn().mockResolvedValue(undefined),
}))
describe('project folder editor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useBonsaiStore.setState({ rootSettings: { version: 1, revision: 0, selection_revision: 0, roots: [], suggestions: ['/projects'], diagnostics: [], repositories: [] }, rootsLoading: false, rootsSaving: false, rootsError: '' })
  })
  it('requires confirmation of a suggestion and allows dismissal', async () => {
    const dismiss = vi.fn()
    render(<ProjectRootsSettings onDismiss={dismiss} />)
    fireEvent.click(screen.getByRole('button', { name: '/projects' }))
    expect(changeProjectRoot).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Folder path')).toHaveValue('/projects')
    fireEvent.click(screen.getByRole('button', { name: 'Add folder' }))
    await waitFor(() => expect(changeProjectRoot).toHaveBeenCalledWith('/projects'))
    fireEvent.click(screen.getByRole('button', { name: 'Later' }))
    expect(dismiss).toHaveBeenCalled()
  })
  it('requires explicit repository selection before activating discovered repositories', async () => {
    useBonsaiStore.setState({
      rootSettings: {
        version: 1,
        revision: 1,
        selection_revision: 0,
        roots: [{ id: 'root', path: '/projects' }],
        suggestions: [],
        diagnostics: [],
        repositories: [
          { id: 'one', root_id: 'root', name: 'one', path: '/projects/one', selected: false, available: true },
          { id: 'two', root_id: 'root', name: 'two', path: '/projects/two', selected: false, available: true },
        ],
      },
    })
    render(<ProjectRootsSettings />)
    fireEvent.click(screen.getByRole('button', { name: /Review discovered repositories/i }))
    expect(screen.getByText('Repositories found')).toBeVisible()
    fireEvent.click(screen.getByRole('checkbox', { name: /one/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Use selected repositories' }))
    await waitFor(() => expect(changeProjectSelection).toHaveBeenCalledWith(['one']))
  })

  it('shows availability, scan diagnostics and inline save failures', () => {
    useBonsaiStore.setState({ rootSettings: { version: 1, revision: 2, selection_revision: 0, roots: [{ id: 'root', path: '/unmounted' }], suggestions: [], diagnostics: [{ root_id: 'root', available: false, truncated: true, messages: ['Select a deeper folder.'] }], repositories: [] }, rootsError: 'Settings changed; reload and retry.' })
    render(<ProjectRootsSettings />)
    expect(screen.getByText('Folder unavailable')).toBeVisible()
    expect(screen.getByText('Select a deeper folder.')).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('Settings changed')
    fireEvent.click(screen.getByRole('button', { name: 'Remove /unmounted' }))
    expect(changeProjectRoot).toHaveBeenCalledWith(undefined, 'root')
  })
})
