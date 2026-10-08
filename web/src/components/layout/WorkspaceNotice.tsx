import { useEffect } from 'react'
import { Info } from 'lucide-react'
import { useBonsaiStore } from '../../stores/bonsai'

export function WorkspaceNotice() {
  const notice = useBonsaiStore(state => state.notice)
  const setNotice = useBonsaiStore(state => state.setNotice)
  useEffect(() => {
    if (!notice) return
    const timer = window.setTimeout(() => setNotice(''), 2600)
    return () => window.clearTimeout(timer)
  }, [notice, setNotice])
  if (!notice) return null
  return <div role="status" className="pointer-events-none absolute bottom-3 right-3 z-50 flex items-center gap-2 rounded-[10px] border border-border-strong bg-panel-3 px-3 py-2 text-[12px] shadow-overlay">
    <Info size={13} className="text-muted" />{notice}
  </div>
}
