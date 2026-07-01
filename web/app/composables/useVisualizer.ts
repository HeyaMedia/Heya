// Visualizer settings + ephemeral view state.
//
// Persisted knobs live in localStorage (client-only feature, no server round
// trip). Follows the same module-level-ref + localStorage shape as
// useAudioSettings / useAudioDevices so the whole audio surface stays uniform.

export type VisMode = 'milkdrop' | 'bars' | 'scope' | 'vu'
export type AutoCycleMode = 'random' | 'sequential'

interface VisualizerState {
  mode: VisMode
  currentPresetName: string
  autoCycleEnabled: boolean
  autoCycleIntervalSec: number
  autoCycleMode: AutoCycleMode
  // When true, preset navigation + auto-cycle draw only from favorites (falls
  // back to the full set if you haven't favorited anything yet).
  likedOnly: boolean
  favoritePresets: string[]
  presetHistory: string[]
  renderScale: number // 0.25..1.0 — GPU cost dial for the Milkdrop canvas
}

const STORAGE_KEY = 'heya_visualizer_v1'

const DEFAULTS: VisualizerState = {
  mode: 'milkdrop',
  currentPresetName: '',
  autoCycleEnabled: true,
  autoCycleIntervalSec: 30,
  autoCycleMode: 'random',
  likedOnly: false,
  favoritePresets: [],
  presetHistory: [],
  renderScale: 1.0,
}

function loadInitial(): VisualizerState {
  if (import.meta.server) return { ...DEFAULTS, favoritePresets: [], presetHistory: [] }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { ...DEFAULTS, favoritePresets: [], presetHistory: [] }
    const parsed = JSON.parse(raw) as Partial<VisualizerState>
    return {
      ...DEFAULTS,
      ...parsed,
      favoritePresets: parsed.favoritePresets ?? [],
      presetHistory: parsed.presetHistory ?? [],
      renderScale: clamp(parsed.renderScale ?? DEFAULTS.renderScale, 0.25, 1),
    }
  } catch {
    return { ...DEFAULTS, favoritePresets: [], presetHistory: [] }
  }
}

const state = ref<VisualizerState>(loadInitial())

// Ephemeral — never persisted.
const fullscreenOpen = ref(false)
const presetBrowserOpen = ref(false)

function persist() {
  if (import.meta.server) return
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state.value)) } catch {}
}

export function useVisualizer() {
  const mode = computed(() => state.value.mode)
  const currentPresetName = computed(() => state.value.currentPresetName)
  const autoCycleEnabled = computed(() => state.value.autoCycleEnabled)
  const autoCycleIntervalSec = computed(() => state.value.autoCycleIntervalSec)
  const autoCycleMode = computed(() => state.value.autoCycleMode)
  const likedOnly = computed(() => state.value.likedOnly)
  const favoritePresets = computed(() => state.value.favoritePresets)
  const presetHistory = computed(() => state.value.presetHistory)
  const renderScale = computed(() => state.value.renderScale)

  function setMode(m: VisMode) {
    state.value = { ...state.value, mode: m }
    persist()
  }
  // Preset name changes fire on every auto-cycle tick, so keep the write cheap.
  function setCurrentPreset(name: string) {
    if (state.value.currentPresetName === name) return
    state.value = { ...state.value, currentPresetName: name }
    pushHistory(name)
    persist()
  }
  function setAutoCycleEnabled(v: boolean) {
    state.value = { ...state.value, autoCycleEnabled: v }
    persist()
  }
  function setAutoCycleIntervalSec(v: number) {
    state.value = { ...state.value, autoCycleIntervalSec: clamp(Math.round(v), 5, 300) }
    persist()
  }
  function setAutoCycleMode(m: AutoCycleMode) {
    state.value = { ...state.value, autoCycleMode: m }
    persist()
  }
  function setLikedOnly(v: boolean) {
    state.value = { ...state.value, likedOnly: v }
    persist()
  }
  function setRenderScale(v: number) {
    state.value = { ...state.value, renderScale: clamp(v, 0.25, 1) }
    persist()
  }

  function isFavorite(name: string) {
    return state.value.favoritePresets.includes(name)
  }
  function toggleFavorite(name: string) {
    const favs = isFavorite(name)
      ? state.value.favoritePresets.filter((n) => n !== name)
      : [...state.value.favoritePresets, name]
    state.value = { ...state.value, favoritePresets: favs }
    persist()
  }
  // Most-recent-first, deduped, capped. Feeds the "recently seen" strip.
  function pushHistory(name: string) {
    if (!name) return
    const hist = [name, ...state.value.presetHistory.filter((n) => n !== name)].slice(0, 50)
    state.value = { ...state.value, presetHistory: hist }
  }

  return {
    mode, currentPresetName,
    autoCycleEnabled, autoCycleIntervalSec, autoCycleMode, likedOnly,
    favoritePresets, presetHistory, renderScale,
    fullscreenOpen, presetBrowserOpen,
    setMode, setCurrentPreset,
    setAutoCycleEnabled, setAutoCycleIntervalSec, setAutoCycleMode, setLikedOnly, setRenderScale,
    isFavorite, toggleFavorite,
  }
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}
