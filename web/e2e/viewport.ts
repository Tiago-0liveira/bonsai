import { expect, type Page } from '@playwright/test'

/**
 * Waits until the React Flow viewport transform stops changing. Fit and wheel
 * animations are time based, so a fixed sleep is too short on a loaded CI runner
 * and a baseline captured mid-animation never matches the settled value.
 */
export async function waitForViewportToSettle(page: Page, quietMs = 500) {
  let previous: string | null = null
  let since = Date.now()
  await expect.poll(async () => {
    const style = await page.locator('.react-flow__viewport').getAttribute('style')
    if (style !== previous) { previous = style; since = Date.now() }
    return Date.now() - since >= quietMs
  }, { timeout: 15_000, intervals: [50] }).toBe(true)
}
