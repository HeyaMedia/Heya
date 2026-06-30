import { alog } from './debug'

export interface SchedulerOptions {
  crossfadeDurationSeconds: number
  onTransitionPoint: () => void
}

// Watches the active deck's position and fires the transition callback for the
// *overlap* transitions only — crossfade (N seconds before the end) and smart
// crossfade (at an absolute start time). Gapless is deliberately NOT scheduled
// here: firing early and pausing the outgoing deck would clip the end of every
// track. Gapless instead swaps on the deck's natural `ended` event (see
// usePlayer.handleEnded), so the outgoing track plays in full. The `fired` flag
// prevents a double-trigger.
export class Scheduler {
  private options: SchedulerOptions
  private fired = false
  private mode: 'gapless' | 'crossfade' = 'gapless'
  private smartStartTimeSeconds: number | null = null

  constructor(options: Partial<SchedulerOptions> & { onTransitionPoint: () => void }) {
    this.options = {
      crossfadeDurationSeconds: 3,
      ...options,
    }
  }

  setMode(mode: 'gapless' | 'crossfade') { this.mode = mode }
  setCrossfadeDuration(seconds: number) { this.options.crossfadeDurationSeconds = seconds }
  setSmartTransitionPoint(seconds: number | null) { this.smartStartTimeSeconds = seconds }

  onTimeUpdate(currentTime: number, duration: number) {
    if (this.fired || !duration || !Number.isFinite(duration)) return

    // Smart crossfade: absolute start time (from server boundary analysis).
    if (this.smartStartTimeSeconds !== null) {
      if (currentTime >= this.smartStartTimeSeconds) {
        this.fired = true
        alog('sched', `fired smart @ ${currentTime.toFixed(1)}s (start ${this.smartStartTimeSeconds.toFixed(1)}s)`)
        this.options.onTransitionPoint()
      }
      return
    }

    // Only crossfade gets an early fire. Gapless is handled on `ended`.
    if (this.mode !== 'crossfade') return

    const remainingMs = (duration - currentTime) * 1000
    const thresholdMs = this.options.crossfadeDurationSeconds * 1000
    if (remainingMs <= thresholdMs) {
      this.fired = true
      alog('sched', `fired crossfade @ ${currentTime.toFixed(1)}/${duration.toFixed(1)}s (overlap ${(thresholdMs / 1000).toFixed(2)}s)`)
      this.options.onTransitionPoint()
    }
  }

  reset() {
    this.fired = false
    this.smartStartTimeSeconds = null
  }
}
