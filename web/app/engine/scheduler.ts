export interface SchedulerOptions {
  gaplessOffsetMs: number
  crossfadeDurationSeconds: number
  onTransitionPoint: () => void
}

// Watches the active deck's playback position and fires the transition
// callback at the right moment for gapless (very last instant) or crossfade
// (N seconds before the end). The fired flag prevents double-trigger.
export class Scheduler {
  private options: SchedulerOptions
  private fired = false
  private mode: 'gapless' | 'crossfade' = 'gapless'
  private smartStartTimeSeconds: number | null = null

  constructor(options: Partial<SchedulerOptions> & { onTransitionPoint: () => void }) {
    this.options = {
      gaplessOffsetMs: 100,
      crossfadeDurationSeconds: 3,
      ...options,
    }
  }

  setMode(mode: 'gapless' | 'crossfade') { this.mode = mode }
  setCrossfadeDuration(seconds: number) { this.options.crossfadeDurationSeconds = seconds }
  setSmartTransitionPoint(seconds: number | null) { this.smartStartTimeSeconds = seconds }

  onTimeUpdate(currentTime: number, duration: number) {
    if (this.fired || !duration || !Number.isFinite(duration)) return

    if (this.smartStartTimeSeconds !== null) {
      if (currentTime >= this.smartStartTimeSeconds) {
        this.fired = true
        this.options.onTransitionPoint()
      }
      return
    }

    const remainingMs = (duration - currentTime) * 1000
    const threshold = this.mode === 'gapless'
      ? this.options.gaplessOffsetMs
      : this.options.crossfadeDurationSeconds * 1000

    if (remainingMs <= threshold) {
      this.fired = true
      this.options.onTransitionPoint()
    }
  }

  reset() {
    this.fired = false
    this.smartStartTimeSeconds = null
  }
}
