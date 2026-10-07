export function PullRequestTabs({ value, onChange }: { value: 'open' | 'closed'; onChange: (value: 'open' | 'closed') => void }) {
  return <div className="flex gap-1 border-b border-[rgb(var(--border))] p-2" aria-label="Pull request state">
    {(['open', 'closed'] as const).map(tab => <button key={tab} type="button" aria-pressed={value === tab} onClick={() => onChange(tab)} className={'bonsai-focus rounded-md px-3 py-1 text-[10px] ' + (value === tab ? 'bg-[rgb(var(--panel-3))] text-[rgb(var(--text))]' : 'text-[rgb(var(--muted))] hover:bg-[rgb(var(--panel-2))]')}>
      {tab === 'open' ? 'Open' : 'Closed'}
    </button>)}
  </div>
}
