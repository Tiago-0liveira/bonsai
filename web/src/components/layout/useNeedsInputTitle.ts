import { useEffect } from 'react'
import { countNeedsInput } from '../../lib/agentStatus'
import { useBonsaiStore } from '../../stores/bonsai'

/** Prefixes the tab title with "(n) " while n agents wait for input, so a background tab shows it. */
export function useNeedsInputTitle() {
  const waiting = useBonsaiStore(state => countNeedsInput(state.agents))
  useEffect(() => {
    if (waiting <= 0) return
    const title = document.title
    document.title = `(${waiting}) ${title}`
    return () => { document.title = title }
  }, [waiting])
}
