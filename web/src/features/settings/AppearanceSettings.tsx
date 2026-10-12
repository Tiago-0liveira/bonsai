import { Check } from 'lucide-react'
import { DEFAULT_THEME_ID } from '../../theme/themes'
import { setActiveTheme, useActiveTheme, useThemes } from '../../theme/themeStore'
import type { Theme } from '../../theme/tokens'

const descriptions: Record<string, string> = {
  bonsai: 'Warm earth tones and green accents.',
  'modern-light': 'Light neutral surfaces with blue accents.',
  'modern-dark': 'Dark neutral surfaces with blue accents.',
}

/** The preview reads its own palette; hovering never changes the active theme. */
function ThemePreview({ theme }: { theme: Theme }) {
  const c = theme.colors
  return <div aria-hidden="true" className="flex h-[88px] gap-2 rounded-lg border p-2" style={{ backgroundColor: c.bg, borderColor: c.border }}>
    <div className="flex w-7 shrink-0 flex-col gap-1.5 rounded border p-1" style={{ backgroundColor: c.panel, borderColor: c.border }}>
      <span className="h-2 rounded-sm" style={{ backgroundColor: c['accent-solid'] }} />
      <span className="h-1 rounded-sm" style={{ backgroundColor: c['muted-2'] }} />
      <span className="h-1 rounded-sm" style={{ backgroundColor: c.border }} />
    </div>
    <div className="flex min-w-0 flex-1 flex-col gap-2">
      <div className="h-3 rounded border" style={{ backgroundColor: c.panel, borderColor: c.border }} />
      <div className="flex flex-1 flex-col justify-between rounded border p-2" style={{ backgroundColor: c['panel-2'], borderColor: c.border }}>
        <span className="h-1 w-3/5 rounded-sm" style={{ backgroundColor: c.text }} />
        <span className="h-1 w-4/5 rounded-sm" style={{ backgroundColor: c['muted-2'] }} />
        <span className="h-2 w-6 rounded-sm" style={{ backgroundColor: c['accent-solid'] }} />
      </div>
    </div>
  </div>
}

export function AppearanceSettings() {
  const themes = useThemes()
  const active = useActiveTheme()
  return <section className="mx-auto w-full max-w-2xl space-y-3 border-b border-border-subtle p-6" aria-labelledby="appearance-title">
    <h2 id="appearance-title" className="text-[13px] font-semibold">Appearance</h2>
    <p className="text-sm text-muted">Choose a theme for all projects in this browser. Changes apply immediately.</p>
    <fieldset>
      <legend className="sr-only">Theme</legend>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] gap-3">
        {themes.map(theme => <label key={theme.id} className="relative cursor-pointer">
          <input type="radio" name="theme" value={theme.id} checked={active.id === theme.id}
            onChange={() => setActiveTheme(theme.id)} className="peer sr-only" aria-label={theme.name} />
          <div className="h-full space-y-2 rounded-xl border border-border bg-panel-2 p-3 transition-colors hover:border-border-strong peer-checked:border-accent peer-checked:bg-accent/5 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent">
            <ThemePreview theme={theme} />
            <div className="flex items-center gap-1.5 text-[12px] font-semibold">
              <span>{theme.name}</span>
              {theme.id === DEFAULT_THEME_ID && <span className="rounded bg-panel-4 px-1 font-mono text-[9px] font-normal text-muted">Default</span>}
              {active.id === theme.id && <Check size={13} className="ml-auto shrink-0 text-accent" aria-hidden="true" />}
            </div>
            <p className="text-[11px] leading-relaxed text-muted">{descriptions[theme.id] ?? 'Custom theme.'}</p>
          </div>
        </label>)}
      </div>
    </fieldset>
  </section>
}
