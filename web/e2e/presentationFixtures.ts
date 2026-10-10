import type { Page } from '@playwright/test'
import { agents } from '../src/mock/agents'
import type { useBonsaiStore } from '../src/stores/bonsai'

declare global {
  interface Window {
    __bonsaiTestStore: typeof useBonsaiStore
    __bonsaiMetrics: { graph: number; labels: number; commits: { phase: string; duration: number }[] }
  }
}

export async function injectAgentPresentation(page: Page) {
  // The canvas can mount before the active project's first snapshot arrives.
  // Wait for that snapshot so its empty agent inventory cannot erase fixtures.
  await page.waitForFunction(() => {
    const state = window.__bonsaiTestStore.getState()
    return state.syncFreshness[state.activeProjectId]?.local?.state === 'ready'
  })
  // Test build only: fixtures never pass through the application's hydration path.
  await page.evaluate(agents => window.__bonsaiTestStore.setState({ agents }), agents)
}
