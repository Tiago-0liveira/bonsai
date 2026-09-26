import { expect, test } from '@playwright/test'

test('Auto-layout clears expanded History and keeps repeated layouts stable', async ({ page }) => {
  await page.goto('/')
  await page.locator('.react-flow__node-stack').filter({ hasText: 'feat' })
    .getByRole('button', { name: /Expand/ }).click()
  const worktree = page.getByTestId('rf__node-wt-web')
  await expect(worktree).toBeVisible()
  await worktree.getByRole('button', { name: /History.*Show/ }).click()
  await expect(worktree.getByText('Old spacing pass', { exact: true })).toBeVisible()

  const layout = page.getByRole('button', { name: 'Auto-layout', exact: true })
  await layout.click()
  const overlaps = () => page.locator('.react-flow__node').evaluateAll((elements) => {
    const rects = elements.map((element) => ({
      id: element.getAttribute('data-id'),
      rect: element.getBoundingClientRect(),
    }))
    return rects.flatMap((a, index) => rects.slice(index + 1).flatMap((b) =>
      a.rect.left < b.rect.right && b.rect.left < a.rect.right &&
      a.rect.top < b.rect.bottom && b.rect.top < a.rect.bottom
        ? [a.id + ' overlaps ' + b.id] : []))
  })
  await expect.poll(overlaps).toEqual([])

  const placements = () => page.evaluate(() =>
    JSON.parse(localStorage.getItem('bonsai-web-workspace-v5')!).state.nodePlacements)
  const first = await placements()
  await layout.click()
  await expect.poll(placements).toEqual(first)
  await expect.poll(overlaps).toEqual([])
})

test('adding and removing a shelf agent preserves other branches and readable PR labels', async ({ page }) => {
  await page.goto('/')
  await page.locator('.react-flow__node-stack').filter({ hasText: 'feat' })
    .getByRole('button', { name: /Expand/ }).click()
  const owner = page.getByTestId('rf__node-wt-web')
  await owner.getByRole('button', { name: /History.*Show/ }).click()
  await page.getByRole('button', { name: 'Auto-layout', exact: true }).click()
  // Let the explicit Fit animation finish before checking viewport stability.
  await page.waitForTimeout(400)
  const snapshot = () => page.evaluate(() => {
    const state = JSON.parse(localStorage.getItem('bonsai-web-workspace-v5')!).state
    const ids = ['bonsai', 'wt-web', 'wt-docs', 'wt-daemon', 'wt-release', 'wt-review',
      'agent-daemon', 'agent-debug', 'agent-release']
    return {
      placements: Object.fromEntries(ids.map((id) => [id, state.nodePlacements[id]])),
      viewport: document.querySelector('.react-flow__viewport')?.getAttribute('style'),
    }
  })
  const baseline = await snapshot()
  const overlaps = () => page.locator('.react-flow__node, [data-pr-edge-label]').evaluateAll((elements) => {
    const rects = elements.map((element) => ({
      id: element.getAttribute('data-id') ?? element.getAttribute('data-pr-edge-label'),
      rect: element.getBoundingClientRect(),
    }))
    return rects.flatMap((a, index) => rects.slice(index + 1).flatMap((b) =>
      a.rect.left < b.rect.right && b.rect.left < a.rect.right &&
      a.rect.top < b.rect.bottom && b.rect.top < a.rect.bottom
        ? [a.id + ' overlaps ' + b.id] : []))
  })
  await expect.poll(overlaps).toEqual([])

  await owner.getByTitle('Restore agent to canvas').first().click()
  const restored = page.getByTestId('rf__node-agent-history-a')
  await expect(restored).toBeVisible()
  await expect.poll(snapshot).toEqual(baseline)
  await expect.poll(overlaps).toEqual([])

  // The reported hidden-label case has four agents in two rows.
  await page.getByRole('button', { name: 'Auto-layout', exact: true }).click()
  await page.waitForTimeout(400)
  await expect(page.locator('[data-pr-edge-label]')).toHaveCount(2)
  await expect.poll(overlaps).toEqual([])
  const beforeRemoval = await snapshot()
  await restored.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Archive agent' }).click()
  await expect(restored).toHaveCount(0)
  await expect.poll(snapshot).toEqual(beforeRemoval)
  await expect.poll(overlaps).toEqual([])
})
