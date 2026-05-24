import { generateFadeIn, generateFadeOut } from './curves'
import type { CrossfadeStrategy, TransitionPlan } from './strategy'

const MIN_DURATION = 1
const MAX_DURATION = 12
const CURVE_SAMPLES = 100

export class TimeBasedCrossfade implements CrossfadeStrategy {
  constructor(private durationSeconds: number = 3) {
    this.durationSeconds = Math.max(MIN_DURATION, Math.min(MAX_DURATION, durationSeconds))
  }

  setDuration(seconds: number) {
    this.durationSeconds = Math.max(MIN_DURATION, Math.min(MAX_DURATION, seconds))
  }

  computeTransition(outgoingDuration: number): TransitionPlan {
    const duration = Math.min(this.durationSeconds, outgoingDuration * 0.5)
    const startTime = outgoingDuration - duration
    return {
      startTimeSeconds: startTime,
      durationSeconds: duration,
      fadeOutCurve: generateFadeOut(CURVE_SAMPLES),
      fadeInCurve: generateFadeIn(CURVE_SAMPLES),
    }
  }
}
