// Theme / accent / density / ambient-background preferences.
//
// Three layers keep this flash-free and cross-device:
//   1. The inline boot script in nuxt.config.ts stamps data-theme /
//      data-accent / data-density on <html> from the localStorage mirror
//      before first paint.
//   2. This composable owns the reactive state, re-stamps the attributes on
//      change, and keeps the localStorage mirror fresh.
//   3. /api/me/settings (users.settings JSONB) is the cross-device source of
//      truth — hydrated once after auth (plugins/appearance.client.ts),
//      server value wins over the local mirror; edits are debounced back up.
//
// Canvas-drawn UI (visualizers, waveforms) can't consume CSS vars reactively;
// they listen for the window 'heya:theme' CustomEvent dispatched after every
// applied change and re-resolve via getComputedStyle.

export type ThemeMode = 'system' | 'dark' | 'light' | 'oled'
export type AccentName =
  | 'gold' | 'ember' | 'crimson' | 'rose' | 'iris'
  | 'ocean' | 'teal' | 'moss' | 'silver'
export type Density = 'comfortable' | 'compact'

export interface AppearancePrefs {
  theme: ThemeMode
  accent: AccentName
  density: Density
  ambientMode: 'on' | 'off'
  ambientIntensity: number // scrim-relative backdrop visibility, 5–60 (%)
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

const STORAGE_KEY = 'heya-appearance'
export const AMBIENT_INTENSITY_DEFAULT = 30

const DEFAULTS: AppearancePrefs = {
  theme: 'dark',
  accent: 'gold',
  density: 'comfortable',
  ambientMode: 'on',
  ambientIntensity: AMBIENT_INTENSITY_DEFAULT,
}

// Boot-inline-style backgrounds must match --bg-1 per theme (heya.css).
const THEME_COLOR: Record<'dark' | 'light' | 'oled', string> = {
  dark: '#0c0c10',
  light: '#f1eee7',
  oled: '#000000',
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
    const d = document.documentElement.dataset
    if (t === 'dark') delete d.theme
    else d.theme = t
    if (prefs.value.accent === 'gold') delete d.accent
    else d.accent = prefs.value.accent
    if (prefs.value.density === 'comfortable') delete d.density
    else d.density = prefs.value.density

    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute('content', THEME_COLOR[t])

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
        const headers = { Authorization: `Bearer ${token}` }
        const settings = await $fetch<Record<string, unknown>>('/api/me/settings', { headers })
        settings.appearance = {
          theme: prefs.value.theme,
          accent: prefs.value.accent,
          density: prefs.value.density,
          ambient_mode: prefs.value.ambientMode,
          ambient_intensity: prefs.value.ambientIntensity,
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

  /** Overlay server-stored appearance (post-auth). Server wins; missing
   *  fields keep local/default values. Does not echo back to the server. */
  function hydrateFromServer(server?: {
    theme?: string
    accent?: string
    density?: string
    ambient_mode?: string
    ambient_intensity?: number
  }) {
    if (!server) return
    const next = { ...prefs.value }
    if (server.theme) next.theme = server.theme as ThemeMode
    if (server.accent) next.accent = server.accent as AccentName
    if (server.density) next.density = server.density as Density
    if (server.ambient_mode === 'on' || server.ambient_mode === 'off')
      next.ambientMode = server.ambient_mode
    if (server.ambient_intensity) next.ambientIntensity = server.ambient_intensity
    prefs.value = next
    apply()
    mirror()
  }

  const ambientEnabled = computed(() => prefs.value.ambientMode !== 'off')

  return { prefs, resolvedTheme, ambientEnabled, set, apply, hydrateFromServer }
}
