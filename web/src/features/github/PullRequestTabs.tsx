export function PullRequestTabs({ value, onChange }: { value: 'open' | 'closed'; onChange: (value: 'open' | 'closed') => void }) {
  return <div className="flex gap-1 border-b border-border-subtle p-2" aria-label="Pull request state">
    {(['open', 'closed'] as const).map(tab => <button key={tab} type="button" aria-pressed={value === tab} onClick={() => onChange(tab)} className={'bonsai-focus h-[30px] rounded-[7px] px-3 text-[12px] ' + (value === tab ? 'bg-panel-3 text-text' : 'text-muted hover:text-text')}>
      {tab === 'open' ? 'Open' : 'Closed'}
    </button>)}
  </div>
}
