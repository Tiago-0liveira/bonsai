import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { StepSlider, heatLevel, type Stop } from './StepSlider'

// jsdom has no PointerEvent; Testing Library falls back to a bare Event that drops clientX and button.
if (!('PointerEvent' in window)) {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number
    constructor(type: string, init: PointerEventInit = {}) { super(type, init); this.pointerId = init.pointerId ?? 0 }
  }
  Object.defineProperty(window, 'PointerEvent', { value: PointerEventPolyfill, configurable: true })
}

const STOPS: Stop[] = ['low', 'medium', 'high', 'xhigh', 'max'].map(value => ({ value, label: value.toUpperCase() }))

function Harness({ initial = '', inherited, onChange = () => {}, disabled = false }: { initial?: string; inherited?: string; onChange?: (value: string) => void; disabled?: boolean }) {
  const [value, setValue] = useState(initial)
  return <StepSlider ariaLabel="Effort" stops={STOPS} value={value} inherited={inherited} disabled={disabled} onChange={next => { setValue(next); onChange(next) }} />
}
const slider = () => screen.getByRole('slider', { name: 'Effort' })
afterEach(() => { cleanup(); vi.restoreAllMocks() })

describe('StepSlider', () => {
  it('reads the profile default until a level is chosen, then the level', () => {
    render(<Harness inherited="high" />)
    expect(slider()).not.toHaveAttribute('aria-valuenow')
    expect(slider()).toHaveAttribute('aria-valuetext', 'Profile default: HIGH')
    expect(slider().querySelector('.step-thumb')).toHaveAttribute('data-ghost', 'true')
    fireEvent.keyDown(slider(), { key: 'ArrowRight' })
    expect(slider()).toHaveAttribute('aria-valuenow', '2')
    expect(slider()).toHaveAttribute('aria-valuetext', 'HIGH')
    expect(slider().querySelector('.step-thumb')).not.toHaveAttribute('data-ghost')
  })

  it('hides the thumb while the default is unknown and starts from the nearest end', () => {
    const onChange = vi.fn()
    const { unmount } = render(<Harness onChange={onChange} />)
    expect(slider().querySelector('.step-thumb')).toHaveAttribute('data-hidden', 'true')
    fireEvent.keyDown(slider(), { key: 'ArrowLeft' })
    expect(onChange).toHaveBeenLastCalledWith('max')
    unmount()
    render(<Harness onChange={onChange} />)
    fireEvent.keyDown(slider(), { key: 'ArrowRight' })
    expect(onChange).toHaveBeenLastCalledWith('low')
  })

  it('moves with the arrow keys, Home and End and stops at the ends', () => {
    const onChange = vi.fn()
    render(<Harness initial="medium" onChange={onChange} />)
    fireEvent.keyDown(slider(), { key: 'ArrowRight' })
    expect(onChange).toHaveBeenLastCalledWith('high')
    fireEvent.keyDown(slider(), { key: 'End' })
    expect(onChange).toHaveBeenLastCalledWith('max')
    onChange.mockClear()
    fireEvent.keyDown(slider(), { key: 'ArrowUp' })
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.keyDown(slider(), { key: 'Home' })
    expect(onChange).toHaveBeenLastCalledWith('low')
    onChange.mockClear()
    fireEvent.keyDown(slider(), { key: 'ArrowLeft' })
    expect(onChange).not.toHaveBeenCalled()
  })

  it('snaps to the stop nearest the pointer, dragging included', () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({ left: 0, width: 100, right: 100, top: 0, bottom: 10, height: 10, x: 0, y: 0, toJSON: () => ({}) })
    fireEvent.pointerDown(slider(), { clientX: 76, button: 0, pointerId: 1 })
    expect(onChange).toHaveBeenLastCalledWith('xhigh')
    expect(slider()).toHaveAttribute('data-dragging', 'true')
    fireEvent.pointerMove(slider(), { clientX: 26, pointerId: 1 })
    expect(onChange).toHaveBeenLastCalledWith('medium')
    fireEvent.pointerUp(slider(), { pointerId: 1 })
    expect(slider()).not.toHaveAttribute('data-dragging')
    fireEvent.pointerMove(slider(), { clientX: 0, pointerId: 1 })
    expect(onChange).toHaveBeenLastCalledWith('medium')
  })

  it('picks a level from its tick', () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    fireEvent.click(slider().querySelector('[data-stop="xhigh"]') as HTMLElement)
    expect(onChange).toHaveBeenCalledWith('xhigh')
  })

  it('ignores every input while disabled', () => {
    const onChange = vi.fn()
    render(<Harness initial="low" disabled onChange={onChange} />)
    expect(slider()).toHaveAttribute('aria-disabled', 'true')
    fireEvent.keyDown(slider(), { key: 'ArrowRight' })
    fireEvent.pointerDown(slider(), { clientX: 90, button: 0, pointerId: 1 })
    fireEvent.click(slider().querySelector('[data-stop="max"]') as HTMLElement)
    expect(onChange).not.toHaveBeenCalled()
  })

  describe('heat', () => {
    const heated = (value: string) => render(<StepSlider ariaLabel="Effort" stops={STOPS} value={value} heat defaultValue="medium" onChange={() => {}} />)

    it('stays cool up to the middle and warms past it, with a flame on the thumb', () => {
      // Five stops: "high" is the middle, so the heat starts after it.
      const { unmount } = heated('high')
      expect(slider()).toHaveAttribute('data-heat', '0')
      expect(slider().querySelector('.step-flame')).toBeNull()
      unmount()
      for (const [value, heat] of [['xhigh', '2'], ['max', '3']] as const) {
        const view = heated(value)
        expect(slider()).toHaveAttribute('data-heat', heat)
        expect(slider().querySelector('.step-flame')).not.toBeNull()
        view.unmount()
      }
    })

    it('does not heat without the heat prop, and marks the default stop', () => {
      render(<StepSlider ariaLabel="Effort" stops={STOPS} value="max" defaultValue="medium" onChange={() => {}} />)
      expect(slider()).toHaveAttribute('data-heat', '0')
      expect(slider().querySelector('[data-stop="medium"]')).toHaveAttribute('data-default', 'true')
      expect(slider().querySelector('[data-stop="medium"]')).toHaveAttribute('title', 'MEDIUM (default)')
    })
  })
})

describe('heatLevel', () => {
  it('is cool up to the middle, warm after it, hot near the top and 3 at the top', () => {
    expect([0, 1, 2, 3, 4, 5].map(index => heatLevel(index, 6))).toEqual([0, 0, 0, 1, 2, 3])
    expect([0, 1, 2].map(index => heatLevel(index, 3))).toEqual([0, 0, 3])
    expect(heatLevel(-1, 5)).toBe(0)
    expect(heatLevel(0, 1)).toBe(0)
  })

  it('never reads the named top efforts below hot, so they jitter on any scale', () => {
    // xhigh sits at index 3 of 6 (warm by position), yet it is hot by name.
    expect(heatLevel(3, 6, 'xhigh')).toBe(2)
    expect(heatLevel(3, 6, 'high')).toBe(1)
    expect(heatLevel(4, 6, 'max')).toBe(2)
    expect(heatLevel(5, 6, 'ultra')).toBe(3)
  })
})
