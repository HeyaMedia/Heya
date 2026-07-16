// Dominant-color sampling for artwork-adaptive UI (hero Play/Details
// buttons). Images come from /api (same-origin), so canvas readback works.

export interface ImageTone {
  /** Dominant saturated tone, clamped to a usable button range. */
  main: string
  /** Hue complement of main at the same clamp — the "opposite" color. */
  complement: string
  /** rgb triplet string of the complement, for alpha tints via rgb(x / a). */
  complementTriplet: string
  /** Readable text color on `main` (luminance-picked). */
  ink: string
}

function rgbToHsl(r: number, g: number, b: number): [number, number, number] {
  r /= 255; g /= 255; b /= 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b)
  const l = (max + min) / 2
  if (max === min) return [0, 0, l]
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h: number
  if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6
  else if (max === g) h = ((b - r) / d + 2) / 6
  else h = ((r - g) / d + 4) / 6
  return [h * 360, s, l]
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  h = ((h % 360) + 360) % 360 / 360
  if (s === 0) { const v = Math.round(l * 255); return [v, v, v] }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s
  const p = 2 * l - q
  const f = (t: number) => {
    if (t < 0) t += 1
    if (t > 1) t -= 1
    if (t < 1 / 6) return p + (q - p) * 6 * t
    if (t < 1 / 2) return q
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6
    return p
  }
  return [Math.round(f(h + 1 / 3) * 255), Math.round(f(h) * 255), Math.round(f(h - 1 / 3) * 255)]
}

/** Lift a sampled "r g b" triplet into text-grade lightness (hue kept).
 *  The sampler clamps tones into button-fill territory (l ≤ 0.58) — as a
 *  FONT color over the hero grade that's too dark, so text consumers lift
 *  lightness while keeping the hue identity. */
export function toneTextVariant(triplet: string): string {
  const parts = triplet.split(' ').map(Number)
  if (parts.length < 3 || parts.some(Number.isNaN)) return triplet
  let [h, s, l] = rgbToHsl(parts[0]!, parts[1]!, parts[2]!)
  s = Math.min(0.9, Math.max(0.45, s))
  // Floor to text-grade, then +15% — hero type reads brighter than a
  // button fill needs to be.
  l = Math.min(0.92, Math.max(0.68, l) * 1.15)
  const [r, g, b] = hslToRgb(h, s, l)
  return `${r} ${g} ${b}`
}

/** The CSS custom-property set a tone-following hero publishes on its root.
 *  One builder so every hero (movie / tv / season / episode / artist /
 *  album / person / home carousels) ships the same vocabulary — including
 *  the complement accent the hero typography wears. */
export function toneStyleVars(t: ImageTone): Record<string, string> {
  const m = t.main.match(/\d+/g)
  const style: Record<string, string> = { '--tone': t.main, '--tone-ink': t.ink }
  if (m) style['--tone-rgb'] = m.slice(0, 3).join(' ')
  if (t.complementTriplet) {
    const comp = toneTextVariant(t.complementTriplet)
    style['--tone-comp'] = `rgb(${comp})`
    style['--tone-comp-rgb'] = comp
  }
  return style
}

// A tone is deterministic per URL (it's a function of the image bytes), so
// memoize the promise: the hero, the discography grid, and the album page can
// all sample the same cover and only the first pays. Keeps per-tile tinting
// (a whole album grid) cheap. Keyed by the original URL, before the ?proxy=1
// retry rewrite.
const toneCache = new Map<string, Promise<ImageTone | null>>()

/** Resolves to null on any failure (missing image, tainted canvas, …). */
export function sampleImageTone(url: string): Promise<ImageTone | null> {
  if (!import.meta.client) return Promise.resolve(null)
  const cached = toneCache.get(url)
  if (cached) return cached
  const p = sampleOnce(url).then((tone) => {
    if (tone || !url.startsWith('/api/')) return tone
    // Same-origin endpoints can 302 to third-party CDNs (album covers still
    // pointing at provider art) — those hosts send no ACAO header, so the
    // CORS-mode <img> load fails before the canvas ever sees pixels. Retry
    // through the server-side byte proxy (?proxy=1, honored by the album
    // cover endpoint; harmless no-op where bytes are already local), using
    // fetch→blob→ImageBitmap instead of a crossorigin <img> so no-cors
    // cache entries from regular <img> renders can't clash with it.
    const sep = url.includes('?') ? '&' : '?'
    return sampleBytes(`${url}${sep}proxy=1`)
  })
  toneCache.set(url, p)
  return p
}

async function sampleBytes(url: string): Promise<ImageTone | null> {
  // Normal path rides the HTTP cache (the proxy sets max-age=3600, so
  // repeat samples of the same cover — every track of an album — are
  // free). The 'reload' retry only fires when the cached path failed:
  // it bypasses AND replaces a stale/poisoned entry (e.g. a cached 302
  // from before the endpoint learned ?proxy=1).
  return (await sampleBytesOnce(url, 'default')) ?? sampleBytesOnce(url, 'reload')
}

async function sampleBytesOnce(url: string, cache: RequestCache): Promise<ImageTone | null> {
  try {
    const res = await fetch(url, { cache, headers: withClientSurfaceHeaders(url) })
    if (!res.ok) return null
    const bitmap = await createImageBitmap(await res.blob())
    try {
      return toneFromSource(bitmap)
    } finally {
      bitmap.close()
    }
  } catch {
    return null
  }
}

function sampleOnce(url: string): Promise<ImageTone | null> {
  return new Promise((resolve) => {
    if (!import.meta.client) return resolve(null)
    const img = new Image()
    img.crossOrigin = 'anonymous'
    img.onerror = () => resolve(null)
    img.onload = () => resolve(toneFromSource(img))
    img.src = url
  })
}

/** Shared pixel path: 24×24 downsample → saturated-third average → clamp. */
function toneFromSource(src: CanvasImageSource): ImageTone | null {
  try {
    const c = document.createElement('canvas')
    c.width = c.height = 24
    const ctx = c.getContext('2d', { willReadFrequently: true })
    if (!ctx) return null
    ctx.drawImage(src, 0, 0, 24, 24)
    const d = ctx.getImageData(0, 0, 24, 24).data
    // Average the most saturated third of pixels — grabs the image's
    // color identity instead of its (usually grey) global average.
    const px: [number, number, number, number][] = []
    for (let i = 0; i < d.length; i += 4) {
      const r = d[i]!, g = d[i + 1]!, b = d[i + 2]!
      px.push([r, g, b, Math.max(r, g, b) - Math.min(r, g, b)])
    }
    px.sort((a, b) => b[3] - a[3])
    const n = Math.max(1, Math.floor(px.length / 3))
    let rs = 0, gs = 0, bs = 0
    for (let i = 0; i < n; i++) { rs += px[i]![0]; gs += px[i]![1]; bs += px[i]![2] }
    let [h, s, l] = rgbToHsl(rs / n, gs / n, bs / n)
    // Clamp into button-friendly territory: saturated enough to read as
    // a color, mid lightness so ink contrast is decidable.
    s = Math.min(0.85, Math.max(0.4, s * 1.15))
    l = Math.min(0.58, Math.max(0.38, l))
    const [r1, g1, b1] = hslToRgb(h, s, l)
    const [r2, g2, b2] = hslToRgb(h + 180, s, l)
    // Ink by RELATIVE luminance, not HSL lightness: a saturated yellow
    // at l=0.5 is perceptually bright (L≈0.7) and white ink on it is
    // unreadable. 0.2 is the crossover where near-black and white give
    // equal WCAG contrast against the fill.
    const lin = (v: number) => {
      v /= 255
      return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
    }
    const L = 0.2126 * lin(r1) + 0.7152 * lin(g1) + 0.0722 * lin(b1)
    return {
      main: `rgb(${r1} ${g1} ${b1})`,
      complement: `rgb(${r2} ${g2} ${b2})`,
      complementTriplet: `${r2} ${g2} ${b2}`,
      ink: L > 0.2 ? '#16130d' : '#ffffff',
    }
  } catch {
    return null
  }
}
