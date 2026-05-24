import { BUILTIN_PRESETS } from '~/engine/dsp/equalizerPresets'

// Audio settings persist to localStorage and are applied to the engine on
// load. Single source of truth: read with useAudioSettings, write through
// the returned setters, the engine stays in sync via a watch.

const BANDS = 10

interface EQState {
  enabled: boolean
  preamp: number    // dB, -12..+12
  postgain: number  // dB
  bands: number[]   // length BANDS, each dB -12..+12
  presetName: string | null
}

interface CrossfadeState {
  mode: 'gapless' | 'crossfade'
  durationSeconds: number
}

interface ReplayGainState {
  mode: 'off' | 'track' | 'album' | 'auto'
}

const STORAGE_KEY = 'heya_audio_settings_v1'

interface StoredSettings {
  eq: EQState
  crossfade: CrossfadeState
  replayGain: ReplayGainState
}

const DEFAULTS: StoredSettings = {
  eq: {
    enabled: false,
    preamp: 0,
    postgain: 0,
    bands: Array(BANDS).fill(0),
    presetName: 'Flat',
  },
  crossfade: {
    mode: 'gapless',
    durationSeconds: 3,
  },
  replayGain: {
    mode: 'auto',
  },
}

function loadInitial(): StoredSettings {
  if (import.meta.server) return { ...DEFAULTS, eq: { ...DEFAULTS.eq, bands: [...DEFAULTS.eq.bands] } }
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { ...DEFAULTS, eq: { ...DEFAULTS.eq, bands: [...DEFAULTS.eq.bands] } }
    const parsed = JSON.parse(raw) as Partial<StoredSettings>
    return {
      eq: { ...DEFAULTS.eq, ...parsed.eq, bands: (parsed.eq?.bands ?? DEFAULTS.eq.bands).slice(0, BANDS) },
      crossfade: { ...DEFAULTS.crossfade, ...parsed.crossfade },
      replayGain: { ...DEFAULTS.replayGain, ...parsed.replayGain },
    }
  } catch {
    return { ...DEFAULTS, eq: { ...DEFAULTS.eq, bands: [...DEFAULTS.eq.bands] } }
  }
}

const state = ref<StoredSettings>(loadInitial())
let applyToEngineFn: (() => void) | null = null

function persist() {
  if (import.meta.server) return
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state.value)) } catch {}
}

export function useAudioSettings() {
  const eq = computed(() => state.value.eq)
  const crossfade = computed(() => state.value.crossfade)
  const replayGain = computed(() => state.value.replayGain)
  const presets = BUILTIN_PRESETS

  // -- EQ --------------------------------------------------------------
  function setEQEnabled(enabled: boolean) {
    state.value = { ...state.value, eq: { ...state.value.eq, enabled } }
    persist()
    applyToEngine()
  }
  function setEQBand(index: number, valueDb: number) {
    if (index < 0 || index >= BANDS) return
    const bands = [...state.value.eq.bands]
    bands[index] = clamp(valueDb, -12, 12)
    // Adjusting a band moves us off any active preset.
    state.value = { ...state.value, eq: { ...state.value.eq, bands, presetName: null } }
    persist()
    applyToEngine()
  }
  function setPreamp(valueDb: number) {
    state.value = { ...state.value, eq: { ...state.value.eq, preamp: clamp(valueDb, -12, 12), presetName: null } }
    persist()
    applyToEngine()
  }
  function setPostgain(valueDb: number) {
    state.value = { ...state.value, eq: { ...state.value.eq, postgain: clamp(valueDb, -12, 12), presetName: null } }
    persist()
    applyToEngine()
  }
  function applyPreset(name: string) {
    const p = presets.find((x) => x.name === name)
    if (!p) return
    state.value = {
      ...state.value,
      eq: {
        ...state.value.eq,
        bands: p.bands.slice(0, BANDS),
        preamp: p.preamp,
        postgain: p.postgain,
        presetName: name,
      },
    }
    persist()
    applyToEngine()
  }

  // -- Crossfade -------------------------------------------------------
  function setCrossfadeMode(mode: CrossfadeState['mode']) {
    state.value = { ...state.value, crossfade: { ...state.value.crossfade, mode } }
    persist()
    applyToEngine()
  }
  function setCrossfadeDuration(seconds: number) {
    state.value = { ...state.value, crossfade: { ...state.value.crossfade, durationSeconds: clamp(seconds, 1, 12) } }
    persist()
    applyToEngine()
  }

  // -- Replay gain -----------------------------------------------------
  function setReplayGainMode(mode: ReplayGainState['mode']) {
    state.value = { ...state.value, replayGain: { mode } }
    persist()
  }

  // -- Engine bridge ---------------------------------------------------
  // Lazy bridge — engine doesn't exist until first user gesture, so we
  // register the apply fn once and let it be called from any setter or
  // from outside (e.g. when usePlayer first creates the engine).
  function registerEngineBridge(fn: () => void) {
    applyToEngineFn = fn
    fn()
  }
  function applyToEngine() { applyToEngineFn?.() }

  return {
    eq, crossfade, replayGain, presets,
    setEQEnabled, setEQBand, setPreamp, setPostgain, applyPreset,
    setCrossfadeMode, setCrossfadeDuration,
    setReplayGainMode,
    registerEngineBridge,
  }
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}
