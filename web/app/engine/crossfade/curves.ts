// Equal-power fade curves. cos/sin pair sums to constant perceived loudness
// across the crossfade, which is what you want for music (linear fade dips
// noticeably in the middle).
export function generateFadeOut(samples: number): number[] {
  const curve = new Array<number>(samples)
  for (let i = 0; i < samples; i++) {
    curve[i] = Math.cos((i / samples) * Math.PI * 0.5)
  }
  return curve
}

export function generateFadeIn(samples: number): number[] {
  const curve = new Array<number>(samples)
  for (let i = 0; i < samples; i++) {
    curve[i] = Math.sin((i / samples) * Math.PI * 0.5)
  }
  return curve
}

// Linear fade — used by the smart strategy when the source track already has
// a natural fade-out, so we don't double-curve it.
export function generateLinearFadeOut(samples: number): number[] {
  const curve = new Array<number>(samples)
  for (let i = 0; i < samples; i++) {
    curve[i] = 1 - i / (samples - 1)
  }
  return curve
}
