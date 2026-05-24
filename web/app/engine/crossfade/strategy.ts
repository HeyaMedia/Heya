export interface TransitionPlan {
  startTimeSeconds: number
  durationSeconds: number
  fadeOutCurve: number[]
  fadeInCurve: number[]
}

export interface BoundaryHints {
  outgoing?: {
    fadeStartMs: number
    outroStartMs: number
    silenceStartMs: number
  }
  incoming?: {
    introEndMs: number
  }
}

export interface CrossfadeStrategy {
  computeTransition(outgoingDuration: number, incomingDuration: number, hints?: BoundaryHints): TransitionPlan
}
