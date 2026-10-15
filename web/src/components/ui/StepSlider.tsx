import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { Flame } from 'lucide-react'

export interface Stop {
  value: string
  /** Read out, and shown by the caller above the slider. */
  label: string
}

const INTENSE = new Set(['xhigh', 'max', 'ultra'])

/**
 * How hot a level reads on a scale of `count` levels: 0 up to the middle, then
 * 1 (warm), 2 (hot, jitters) and 3 at the top. The named top efforts
 * (xhigh, max, ultra) are never below 2, whatever the length of the scale.
 */
export function heatLevel(index: number, count: number, value = ''): 0 | 1 | 2 | 3 {
  if (index < 0 || count < 2) return 0
  const ratio = index / (count - 1)
  let level: 0 | 1 | 2 | 3 = ratio <= 0.5 ? 0 : index === count - 1 ? 3 : ratio >= 0.7 ? 2 : 1
  if (INTENSE.has(value.toLowerCase()) && level < 2) level = 2
  return level
}

const KEYS: Record<string, number> = { ArrowRight: 1, ArrowUp: 1, PageUp: 1, ArrowLeft: -1, ArrowDown: -1, PageDown: -1 }

/**
 * A slider over a short list of levels: the thumb snaps to a stop, the track
 * fills up to it and the chosen label lifts. An empty `value` means nothing is
 * chosen yet (a profile default): the thumb is a hollow ghost at `inherited`,
 * or hidden when the default is unknown. Pointer, arrow keys, Home and End all
 * work; the end captions say which way is faster and which is smarter. The
 * ticks carry no captions: the caller names the chosen level once, above.
 */
export function StepSlider({ stops, value, onChange, ariaLabel, inherited, defaultValue, heat = false, lowCaption = 'Faster', highCaption = 'Smarter', unsetText = 'Profile default', disabled = false }: {
  stops: Stop[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  inherited?: string
  /** The stop that is the default: its tick gets a ring. */
  defaultValue?: string
  /**
   * Past the middle the track warms up (amber, then orange, then red) and the
   * thumb turns into a flame: high effort costs more and runs longer.
   */
  heat?: boolean
  lowCaption?: string
  highCaption?: string
  /** The value text read out while nothing is chosen. */
  unsetText?: string
  disabled?: boolean
}) {
  const rail = useRef<HTMLDivElement | null>(null)
  const [dragging, setDragging] = useState(false)
  const [pings, setPings] = useState(0)
  const last = Math.max(stops.length - 1, 1)
  const chosen = stops.findIndex(stop => stop.value === value)
  const ghost = chosen < 0 ? stops.findIndex(stop => stop.value === inherited) : -1
  const shown = chosen >= 0 ? chosen : ghost
  const percent = (index: number) => (stops.length < 2 ? 50 : (index / last) * 100)
  const warmth = heat && chosen >= 0 ? heatLevel(chosen, stops.length, stops[chosen].value) : 0

  const pick = (index: number) => {
    const stop = stops[Math.min(Math.max(index, 0), stops.length - 1)]
    if (!stop || disabled) return
    if (stop.value !== value) {
      onChange(stop.value)
      setPings(count => count + 1)
    }
  }
  const nearest = (clientX: number) => {
    const box = rail.current?.getBoundingClientRect()
    if (!box || !box.width) return chosen
    return Math.round(Math.min(Math.max((clientX - box.left) / box.width, 0), 1) * last)
  }
  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (disabled || event.button !== 0) return
    event.currentTarget.setPointerCapture?.(event.pointerId)
    setDragging(true)
    pick(nearest(event.clientX))
  }
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (dragging) pick(nearest(event.clientX))
  }
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return
    if (event.key === 'Home') { event.preventDefault(); pick(0); return }
    if (event.key === 'End') { event.preventDefault(); pick(stops.length - 1); return }
    const step = KEYS[event.key]
    if (!step) return
    event.preventDefault()
    if (chosen >= 0) pick(chosen + step)
    else pick(ghost >= 0 ? ghost : step > 0 ? 0 : stops.length - 1)
  }

  return (
    <div>
      <div className="mb-0.5 flex justify-between font-mono text-[10px] text-muted-2" aria-hidden="true">
        <span>{lowCaption}</span><span>{highCaption}</span>
      </div>
      <div
        role="slider"
        tabIndex={disabled ? -1 : 0}
        aria-label={ariaLabel}
        aria-orientation="horizontal"
        aria-disabled={disabled || undefined}
        aria-valuemin={0}
        aria-valuemax={stops.length - 1}
        aria-valuenow={chosen >= 0 ? chosen : undefined}
        aria-valuetext={chosen >= 0 ? stops[chosen].label : ghost >= 0 ? `${unsetText}: ${stops[ghost].label}` : unsetText}
        data-dragging={dragging || undefined}
        data-heat={warmth}
        className="step-slider"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={() => setDragging(false)}
        onPointerCancel={() => setDragging(false)}
        onLostPointerCapture={() => setDragging(false)}
        onKeyDown={onKeyDown}
      >
        <div ref={rail} className="step-rail">
          <div className="step-fill" style={{ width: shown >= 0 ? `${percent(shown)}%` : '0%', opacity: chosen >= 0 ? 1 : 0 }} />
          {stops.map((stop, index) => (
            <span
              key={stop.value}
              data-stop={stop.value}
              data-passed={chosen >= 0 && index <= chosen ? 'true' : undefined}
              data-current={index === shown ? 'true' : undefined}
              data-default={stop.value === defaultValue ? 'true' : undefined}
              title={stop.value === defaultValue ? `${stop.label} (default)` : stop.label}
              className="step-tick"
              style={{ left: `${percent(index)}%` }}
              onClick={() => pick(index)}
            />
          ))}
          <span
            className="step-thumb"
            data-ghost={chosen < 0 && ghost >= 0 ? 'true' : undefined}
            data-hidden={shown < 0 ? 'true' : undefined}
            style={{ left: `${percent(Math.max(shown, 0))}%` }}
          >
            {warmth > 0 && chosen >= 0 && <Flame size={12} strokeWidth={2.4} aria-hidden="true" className="step-flame" />}
            {pings > 0 && chosen >= 0 && <span key={pings} className="step-ping" />}
          </span>
        </div>
      </div>
    </div>
  )
}
