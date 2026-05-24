import { generateFadeIn, generateFadeOut, generateLinearFadeOut } from './curves'
import type { BoundaryHints, CrossfadeStrategy, TransitionPlan } from './strategy'

const MIN_DURATION = 0.5
const CURVE_SAMPLES = 100

// SmartCrossfade uses per-track boundary hints (intro/outro/silence detection
// from an offline analysis pass) to align the transition with the music's
// natural shape. Falls back to time-based when no hints are available.
export class SmartCrossfade implements CrossfadeStrategy {
  constructor(private fallbackDuration: number = 3) {}

  computeTransition(outgoingDuration: number, _incomingDuration: number, hints?: BoundaryHints): TransitionPlan {
    if (!hints?.outgoing) {
      return this.timedFallback(outgoingDuration)
    }

    const { fadeStartMs, outroStartMs, silenceStartMs } = hints.outgoing
    const trackEndMs = outgoingDuration * 1000
    const endMs = Math.min(silenceStartMs, trackEndMs)

    const hasNaturalFade = fadeStartMs > 0 && fadeStartMs < silenceStartMs
    const startMs = hasNaturalFade ? fadeStartMs : outroStartMs > 0 ? outroStartMs : 0

    const minStartMs = outgoingDuration * 1000 * 0.6
    if (startMs <= 0 || startMs >= endMs || startMs < minStartMs) {
      return this.timedFallback(outgoingDuration)
    }

    const durationSeconds = Math.max(MIN_DURATION, (endMs - startMs) / 1000)
    const fadeOutCurve = hasNaturalFade ? generateLinearFadeOut(CURVE_SAMPLES) : generateFadeOut(CURVE_SAMPLES)

    return {
      startTimeSeconds: startMs / 1000,
      durationSeconds,
      fadeOutCurve,
      fadeInCurve: generateFadeIn(CURVE_SAMPLES),
    }
  }

  private timedFallback(outgoingDuration: number): TransitionPlan {
    const duration = Math.min(this.fallbackDuration, outgoingDuration * 0.5)
    const startTime = outgoingDuration - duration
    return {
      startTimeSeconds: startTime,
      durationSeconds: duration,
      fadeOutCurve: generateFadeOut(CURVE_SAMPLES),
      fadeInCurve: generateFadeIn(CURVE_SAMPLES),
    }
  }
}
