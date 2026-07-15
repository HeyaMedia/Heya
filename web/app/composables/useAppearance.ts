// Theme / accent / density / type / flair preferences.
//
// Three layers keep this flash-free and cross-device:
//   1. The inline boot script in nuxt.config.ts stamps every appearance
//      attribute (data-theme / data-accent / data-density / data-typeset /
//      data-fontscale / data-lighting / data-glass / data-radius / data-hero /
//      data-motion) on <html> — plus the cached custom-accent inline vars —
//      from the localStorage mirror before first paint.
//   2. This composable owns the reactive state, re-stamps the attributes on
//      change, and keeps the localStorage mirror fresh.
//   3. /api/me/settings (users.settings JSONB) is the cross-device source of
//      truth — hydrated once after auth (plugins/appearance.client.ts),
//      server value wins over the local mirror; edits are debounced back up.
//
// Canvas-drawn UI (visualizers, waveforms) can't consume CSS vars reactively;
// they listen for the window 'heya:theme' CustomEvent dispatched after every
// applied change and re-resolve via getComputedStyle.
//
// Custom accent: a user-picked hex is derived into the full --accent family
// (bright/deep/ink/rgb) once, in JS, and STAMPED AS INLINE STYLE VARS on
// <html> (inline style outranks the preset attribute blocks by specificity).
// The derived family is cached in the prefs blob so the boot script stamps the
// saved values verbatim — it never re-derives (stays dumb + tiny).

export type ThemeMode = 'system' | 'dark' | 'light' | 'oled'
export type AccentName =
  | 'gold' | 'ember' | 'crimson' | 'rose' | 'iris'
  | 'ocean' | 'teal' | 'moss' | 'silver'
export type Density = 'comfortable' | 'compact' | 'spacious'
export type TypeSet = 'heya' | 'editorial' | 'grotesk' | 'rounded' | 'system'
export type FontScale = 'sm' | 'md' | 'lg'
export type Lighting = 'dramatic' | 'flat'
export type Glass = 'rich' | 'minimal'
export type RadiusMode = 'soft' | 'sharp'
export type HeroMode = 'standard' | 'short'
export type MotionMode = 'system' | 'reduced' | 'full'
export type ScrollbarMode = 'overlay' | 'classic'

/** The derived --accent family cached for a custom hex (see header). */
export interface AccentDerived {
  accent: string   // clamped base hex
  rgb: string      // "R G B"
  bright: string
  deep: string
  ink: string      // WCAG-picked near-black / near-white text on the fill
}

export interface AppearancePrefs {
  theme: ThemeMode
  accent: AccentName
  /** User hex when a custom accent is active; null → a preset is in use. */
  accentCustom: string | null
  /** Cached derived family for `accentCustom` (boot stamps this verbatim). */
  accentCustomDerived: AccentDerived | null
  density: Density
  typeset: TypeSet
  fontScale: FontScale
  /** Page tint follows artwork tone. Off → pages keep the accent default. */
  toneFollow: boolean
  lighting: Lighting
  glass: Glass
  radius: RadiusMode
  hero: HeroMode
  motion: MotionMode
  /** Scrollbar style: 'overlay' (default, floating auto-hiding thumb) or
   *  'classic' (native OS bar returns). */
  scrollbar: ScrollbarMode
  ambientMode: 'on' | 'off'
  ambientIntensity: number // scrim-relative backdrop visibility, 5–60 (%)
  /** "More Like This" on detail pages: also show titles NOT in the library
   *  (they link out to the strongest public metadata provider). */
  showUnavailableRecs: boolean
}

export const ACCENTS: { name: AccentName; label: string; hex: string }[] = [
  { name: 'gold', label: 'Heya Gold', hex: '#e6b94a' },
  { name: 'ember', label: 'Ember', hex: '#e8834a' },
  { name: 'crimson', label: 'Crimson', hex: '#d9565c' },
  { name: 'rose', label: 'Rose', hex: '#de6f9f' },
  { name: 'iris', label: 'Iris', hex: '#9b82e8' },
  { name: 'ocean', label: 'Ocean', hex: '#5b9ce6' },
  { name: 'teal', label: 'Teal', hex: '#45c2b0' },
  { name: 'moss', label: 'Moss', hex: '#93c159' },
  { name: 'silver', label: 'Silver', hex: '#b9bec8' },
]

export const TYPESETS: { value: TypeSet; label: string; hint: string }[] = [
  { value: 'heya', label: 'Heya', hint: 'Archivo display · Inter body' },
  { value: 'editorial', label: 'Editorial', hint: 'Fraunces · Source Serif' },
  { value: 'grotesk', label: 'Grotesk', hint: 'Space Grotesk · Inter' },
  { value: 'rounded', label: 'Rounded', hint: 'Nunito · Inter' },
  { value: 'system', label: 'System', hint: 'Native OS fonts · no download' },
]

const STORAGE_KEY = 'heya-appearance'
export const AMBIENT_INTENSITY_DEFAULT = 30

const DEFAULTS: AppearancePrefs = {
  theme: 'dark',
  accent: 'gold',
  accentCustom: null,
  accentCustomDerived: null,
  density: 'comfortable',
  typeset: 'heya',
  fontScale: 'md',
  toneFollow: true,
  lighting: 'dramatic',
  glass: 'rich',
  radius: 'soft',
  hero: 'standard',
  motion: 'system',
  scrollbar: 'overlay',
  ambientMode: 'on',
  ambientIntensity: AMBIENT_INTENSITY_DEFAULT,
  showUnavailableRecs: false,
}

// Boot-inline-style backgrounds must match --bg-1 per theme (heya.css).
const THEME_COLOR: Record<'dark' | 'light' | 'oled', string> = {
  dark: '#0c0c10',
  light: '#f1eee7',
  oled: '#000000',
}

// ── Custom-accent derivation ─────────────────────────────────────────────
// Mirrors the HSL / relative-luminance math in useImageTone.ts: clamp the
// picked hex into a usable accent band, then spin off the bright/deep cuts and
// a WCAG-picked ink. Deterministic — computed once per pick, then cached.

function clamp(v: number, lo: number, hi: number) { return Math.min(hi, Math.max(lo, v)) }

function hexToRgb(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return null
  let h = m[1]!
  if (h.length === 3) h = h[0]! + h[0]! + h[1]! + h[1]! + h[2]! + h[2]!
  const n = parseInt(h, 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function rgbToHex(r: number, g: number, b: number): string {
  const h = (v: number) => clamp(Math.round(v), 0, 255).toString(16).padStart(2, '0')
  return `#${h(r)}${h(g)}${h(b)}`
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

function relLuminance(r: number, g: number, b: number): number {
  const lin = (v: number) => {
    v /= 255
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

/** Derive the full --accent family from a user hex (null on a bad hex). */
export function deriveAccent(hex: string): AccentDerived | null {
  const rgb = hexToRgb(hex)
  if (!rgb) return null
  let [h, s, l] = rgbToHsl(rgb[0], rgb[1], rgb[2])
  // Clamp into a band that reads as a saturated "accent" and keeps ink
  // contrast decidable — the same intent as sampleImageTone's button clamp,
  // widened a touch since presets span gold→iris.
  s = clamp(s, 0.32, 0.95)
  l = clamp(l, 0.46, 0.7)
  const base = hslToRgb(h, s, l)
  const bright = hslToRgb(h, clamp(s * 1.04, 0, 1), clamp(l + 0.11, 0, 0.82))
  const deep = hslToRgb(h, clamp(s * 1.02, 0, 1), clamp(l - 0.18, 0.2, 1))
  // Ink by relative luminance (a saturated yellow at l=0.6 is perceptually
  // bright → needs dark ink). Dark ink is hue-tinted near-black to match the
  // preset --accent-ink family; light accents fall back to warm white.
  const dark = hslToRgb(h, clamp(s * 0.7, 0, 0.6), 0.07)
  const ink = relLuminance(base[0], base[1], base[2]) > 0.28
    ? rgbToHex(dark[0], dark[1], dark[2])
    : '#f6f5f0'
  return {
    accent: rgbToHex(base[0], base[1], base[2]),
    rgb: `${base[0]} ${base[1]} ${base[2]}`,
    bright: rgbToHex(bright[0], bright[1], bright[2]),
    deep: rgbToHex(deep[0], deep[1], deep[2]),
    ink,
  }
}

function readMirror(): AppearancePrefs {
  if (!import.meta.client) return { ...DEFAULTS }
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
    return { ...DEFAULTS, ...raw }
  } catch {
    return { ...DEFAULTS }
  }
}

let mediaWatcher: MediaQueryList | null = null
let saveTimer: ReturnType<typeof setTimeout> | null = null

const ACCENT_VARS = ['--accent', '--accent-rgb', '--accent-bright', '--accent-deep', '--accent-ink']

export function useAppearance() {
  const prefs = useState<AppearancePrefs>('appearance_prefs', readMirror)

  /** The theme actually painted (system resolved to dark/light). */
  const resolvedTheme = useState<'dark' | 'light' | 'oled'>('appearance_resolved', () =>
    resolve(prefs.value.theme),
  )

  function resolve(mode: ThemeMode): 'dark' | 'light' | 'oled' {
    if (mode === 'system') {
      if (import.meta.client && window.matchMedia?.('(prefers-color-scheme: light)').matches)
        return 'light'
      return 'dark'
    }
    return mode
  }

  function apply() {
    if (!import.meta.client) return
    const t = resolve(prefs.value.theme)
    resolvedTheme.value = t
    const el = document.documentElement
    const d = el.dataset
    if (t === 'dark') delete d.theme
    else d.theme = t

    // Accent: a valid custom hex stamps its cached derived family as inline
    // vars (they outrank the preset blocks); otherwise clear them and fall
    // back to the preset attribute.
    const custom = prefs.value.accentCustom && prefs.value.accentCustomDerived
    if (custom) {
      const c = prefs.value.accentCustomDerived!
      el.style.setProperty('--accent', c.accent)
      el.style.setProperty('--accent-rgb', c.rgb)
      el.style.setProperty('--accent-bright', c.bright)
      el.style.setProperty('--accent-deep', c.deep)
      el.style.setProperty('--accent-ink', c.ink)
      delete d.accent
    } else {
      for (const v of ACCENT_VARS) el.style.removeProperty(v)
      if (prefs.value.accent === 'gold') delete d.accent
      else d.accent = prefs.value.accent
    }

    setAttr(d, 'density', prefs.value.density, 'comfortable')
    setAttr(d, 'typeset', prefs.value.typeset, 'heya')
    setAttr(d, 'fontscale', prefs.value.fontScale, 'md')
    setAttr(d, 'lighting', prefs.value.lighting, 'dramatic')
    setAttr(d, 'glass', prefs.value.glass, 'rich')
    setAttr(d, 'radius', prefs.value.radius, 'soft')
    setAttr(d, 'hero', prefs.value.hero, 'standard')
    setAttr(d, 'motion', prefs.value.motion, 'system')
    setAttr(d, 'scrollbar', prefs.value.scrollbar, 'overlay')

    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute('content', THEME_COLOR[t])

    // Cookie mirror of the *resolved* theme: the Go SPA handler reads it to
    // stamp data-theme on the served shell, so the loading screen paints in
    // the right palette before any JS runs.
    document.cookie = `heya_theme=${t}; Path=/; Max-Age=31536000; SameSite=Lax`

    // Canvas consumers re-resolve tokens on this signal.
    window.dispatchEvent(new CustomEvent('heya:theme', { detail: { theme: t } }))

    // System mode follows OS changes live; other modes drop the listener.
    if (prefs.value.theme === 'system' && !mediaWatcher) {
      mediaWatcher = window.matchMedia('(prefers-color-scheme: light)')
      mediaWatcher.addEventListener('change', apply)
    } else if (prefs.value.theme !== 'system' && mediaWatcher) {
      mediaWatcher.removeEventListener('change', apply)
      mediaWatcher = null
    }
  }

  function setAttr(d: DOMStringMap, key: string, value: string, dflt: string) {
    if (value === dflt) delete d[key]
    else d[key] = value
  }

  function mirror() {
    if (import.meta.client)
      localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs.value))
  }

  /** Debounced write-back to /api/me/settings (GET-merge-PUT of the blob). */
  function scheduleSave() {
    const { isAuthenticated } = useAuth()
    if (!isAuthenticated.value) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      saveTimer = null
      try {
        const token = localStorage.getItem('heya_token')
        const headers = withClientSurfaceHeaders('/api/me/settings', { Authorization: `Bearer ${token}` })
        const settings = await $fetch<Record<string, unknown>>('/api/me/settings', { headers })
        settings.appearance = {
          theme: prefs.value.theme,
          accent: prefs.value.accent,
          accent_custom: prefs.value.accentCustom,
          accent_custom_derived: prefs.value.accentCustomDerived,
          density: prefs.value.density,
          typeset: prefs.value.typeset,
          font_scale: prefs.value.fontScale,
          tone_follow: prefs.value.toneFollow,
          lighting: prefs.value.lighting,
          glass: prefs.value.glass,
          radius: prefs.value.radius,
          hero: prefs.value.hero,
          motion: prefs.value.motion,
          scrollbar: prefs.value.scrollbar,
          ambient_mode: prefs.value.ambientMode,
          ambient_intensity: prefs.value.ambientIntensity,
          show_unavailable_recs: prefs.value.showUnavailableRecs,
        }
        await $fetch('/api/me/settings', { method: 'PUT', body: settings, headers })
      } catch {
        // Non-fatal: the localStorage mirror still has it; next successful
        // save (or another device) reconciles.
      }
    }, 600)
  }

  function set<K extends keyof AppearancePrefs>(key: K, value: AppearancePrefs[K]) {
    prefs.value = { ...prefs.value, [key]: value }
    apply()
    mirror()
    scheduleSave()
  }

  /** Pick a preset accent — clears any custom override. */
  function setAccentPreset(name: AccentName) {
    prefs.value = { ...prefs.value, accent: name, accentCustom: null, accentCustomDerived: null }
    apply(); mirror(); scheduleSave()
  }

  /** Apply a custom accent hex, deriving + caching the family. No-op on a
   *  malformed hex. Returns the derived family (or null). */
  function setAccentCustom(hex: string): AccentDerived | null {
    const derived = deriveAccent(hex)
    if (!derived) return null
    prefs.value = { ...prefs.value, accentCustom: hex, accentCustomDerived: derived }
    apply(); mirror(); scheduleSave()
    return derived
  }

  /** Overlay server-stored appearance (post-auth). Server wins; missing
   *  fields keep local/default values. Does not echo back to the server. */
  function hydrateFromServer(server?: Record<string, unknown>) {
    if (!server) return
    const next = { ...prefs.value }
    const s = server as {
      theme?: string; accent?: string
      accent_custom?: string | null; accent_custom_derived?: AccentDerived | null
      density?: string; typeset?: string; font_scale?: string
      tone_follow?: boolean; lighting?: string; glass?: string
      radius?: string; hero?: string; motion?: string; scrollbar?: string
      ambient_mode?: string; ambient_intensity?: number; show_unavailable_recs?: boolean
    }
    if (s.theme) next.theme = s.theme as ThemeMode
    if (s.accent) next.accent = s.accent as AccentName
    // accent_custom present (even null) is authoritative for the override.
    if ('accent_custom' in s) {
      next.accentCustom = s.accent_custom ?? null
      next.accentCustomDerived = s.accent_custom
        ? (s.accent_custom_derived ?? deriveAccent(s.accent_custom)) : null
    }
    if (s.density) next.density = s.density as Density
    if (s.typeset) next.typeset = s.typeset as TypeSet
    if (s.font_scale) next.fontScale = s.font_scale as FontScale
    if (typeof s.tone_follow === 'boolean') next.toneFollow = s.tone_follow
    if (s.lighting) next.lighting = s.lighting as Lighting
    if (s.glass) next.glass = s.glass as Glass
    if (s.radius) next.radius = s.radius as RadiusMode
    if (s.hero) next.hero = s.hero as HeroMode
    if (s.motion) next.motion = s.motion as MotionMode
    if (s.scrollbar === 'overlay' || s.scrollbar === 'classic') next.scrollbar = s.scrollbar
    if (s.ambient_mode === 'on' || s.ambient_mode === 'off') next.ambientMode = s.ambient_mode
    if (s.ambient_intensity) next.ambientIntensity = s.ambient_intensity
    if (typeof s.show_unavailable_recs === 'boolean') next.showUnavailableRecs = s.show_unavailable_recs
    prefs.value = next
    apply()
    mirror()
  }

  const ambientEnabled = computed(() => prefs.value.ambientMode !== 'off')
  /** Whether pages should tint their chrome from artwork tone (knob #6). */
  const toneFollowEnabled = computed(() => prefs.value.toneFollow !== false)

  return {
    prefs, resolvedTheme, ambientEnabled, toneFollowEnabled,
    set, setAccentPreset, setAccentCustom, apply, hydrateFromServer,
  }
}
