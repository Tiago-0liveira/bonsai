import { beforeEach, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { CanvasSettings } from './CanvasSettings'
import { useBonsaiStore } from '../../stores/bonsai'
import { DEFAULT_CANVAS_PREFERENCES } from '../../types'

beforeEach(() => useBonsaiStore.setState({ canvasPreferences: DEFAULT_CANVAS_PREFERENCES }))

it('edits the canvas focus preferences', () => {
  render(<CanvasSettings />)
  const worktrees = screen.getByRole('checkbox', { name: /Focus a worktree/ })
  const agents = screen.getByRole('checkbox', { name: /Focus an agent/ })
  expect(worktrees).toBeChecked()
  expect(agents).toBeChecked()
  fireEvent.click(worktrees)
  fireEvent.click(agents)
  fireEvent.change(screen.getByRole('combobox', { name: 'On the GitHub tab, selecting a branch' }), { target: { value: 'open-canvas' } })
  expect(useBonsaiStore.getState().canvasPreferences).toEqual({ focusOnBranchSelect: false, focusAgentsOnBranchSelect: false, githubBranchSelect: 'open-canvas' })
})
