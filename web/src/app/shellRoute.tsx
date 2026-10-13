import { createContext, useContext } from 'react'

export type ShellPage = 'canvas' | 'github' | 'other'

/**
 * The open page and the navigation the shell islands need. The root layout
 * provides it from the router, so islands stay renderable without one.
 */
export interface ShellRoute {
  page: ShellPage
  showCanvas: () => void
  showPullRequest: (id: string) => void
}

const ShellRouteContext = createContext<ShellRoute>({ page: 'other', showCanvas: () => undefined, showPullRequest: () => undefined })

export const ShellRouteProvider = ShellRouteContext.Provider
export const useShellRoute = () => useContext(ShellRouteContext)
