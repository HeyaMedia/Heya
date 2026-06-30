import type { CrossfeedPreset } from '~~/shared/types/audio'
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
  // 'gapless'   — hard cut on the outgoing track's natural end (no overlap)
  // 'crossfade' — fixed-duration equal-power overlap
  // 'smart'     — overlap aligned to the outgoing track's structure
  //               (fade/outro/silence) from server boundary analysis, falling
  //               back to timed when a track has no boundaries
  mode: 'gapless' | 'crossfade' | 'smart'
  durationSeconds: number
  // When true, crossfade is suppressed between two consecutive tracks from the
  // same album so intentional album segues (live records, concept albums, DJ
  // mixes) play back-to-back with a hard gapless cut instead of a fade.
  albumAware: boolean
}

interface ReplayGainState {
  mode: 'off' | 'track' | 'album' | 'auto'
}

interface CrossfeedState {
  enabled: boolean
  preset: CrossfeedPreset
}

// The user-reorderable effect blocks (between the pinned normalization head and
// limiter tail). The EQ's preamp/postgain travel with the 'equalizer' entry.
export type DspBlockId = 'equalizer' | 'crossfeed'

// A saveable EQ + crossfeed snapshot — the unit a per-output-device profile stores.
export interface AudioProfile {
  eqEnabled: boolean
  bands: number[]
  preamp: number
  postgain: number
  presetName: string | null
  crossfeedEnabled: boolean
  crossfeedPreset: CrossfeedPreset
}

interface DspChainState {
  order: DspBlockId[]    // chain order of the effect blocks
  limiterEnabled: boolean // the brick-wall safety limiter (default on)
}

const STORAGE_KEY = 'heya_audio_settings_v1'

interface StoredSettings {
  eq: EQState
  crossfade: CrossfadeState
  replayGain: ReplayGainState
  crossfeed: CrossfeedState
  dspChain: DspChainState
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
    albumAware: true,
  },
  replayGain: {
    mode: 'auto',
  },
  crossfeed: {
    enabled: false,
    preset: 'natural',
  },
  dspChain: {
    order: ['equalizer', 'crossfeed'],
    limiterEnabled: true,
  },
}

const DSP_BLOCKS: DspBlockId[] = ['equalizer', 'crossfeed']

// Coerce a persisted chain into a complete, dupe-free permutation of the known
// blocks — so a stale/corrupt order can't drop a block or smuggle in an unknown
// one, and a block added in a newer version lands in its default slot.
function sanitizeChain(c: Partial<DspChainState> | undefined): DspChainState {
  const seen = new Set<DspBlockId>()
  const order: DspBlockId[] = []
  for (const id of c?.order ?? []) {
    if (DSP_BLOCKS.includes(id) && !seen.has(id)) { seen.add(id); order.push(id) }
  }
  for (const id of DSP_BLOCKS) if (!seen.has(id)) order.push(id)
  return { order, limiterEnabled: c?.limiterEnabled ?? DEFAULTS.dspChain.limiterEnabled }
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
      crossfeed: { ...DEFAULTS.crossfeed, ...parsed.crossfeed },
      dspChain: sanitizeChain(parsed.dspChain),
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
  const crossfeed = computed(() => state.value.crossfeed)
  const dspChain = computed(() => state.value.dspChain)
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
  function setCrossfadeAlbumAware(albumAware: boolean) {
    state.value = { ...state.value, crossfade: { ...state.value.crossfade, albumAware } }
    persist()
    applyToEngine()
  }

  // -- Replay gain -----------------------------------------------------
  function setReplayGainMode(mode: ReplayGainState['mode']) {
    state.value = { ...state.value, replayGain: { mode } }
    persist()
    // Re-apply so the engine bridge re-levels the current track (and the
    // preloaded next deck) immediately instead of only on the next play.
    applyToEngine()
  }

  // -- Crossfeed -------------------------------------------------------
  function setCrossfeedEnabled(enabled: boolean) {
    state.value = { ...state.value, crossfeed: { ...state.value.crossfeed, enabled } }
    persist()
    applyToEngine()
  }
  function setCrossfeedPreset(preset: CrossfeedPreset) {
    state.value = { ...state.value, crossfeed: { ...state.value.crossfeed, preset } }
    persist()
    applyToEngine()
  }

  // -- DSP chain -------------------------------------------------------
  function setLimiterEnabled(enabled: boolean) {
    state.value = { ...state.value, dspChain: { ...state.value.dspChain, limiterEnabled: enabled } }
    persist()
    applyToEngine()
  }
  // Move an effect block one slot earlier (dir -1) or later (dir +1) in the chain.
  function moveDspBlock(id: DspBlockId, dir: -1 | 1) {
    const order = [...state.value.dspChain.order]
    const i = order.indexOf(id)
    const j = i + dir
    if (i < 0 || j < 0 || j >= order.length) return
    ;[order[i], order[j]] = [order[j]!, order[i]!]
    state.value = { ...state.value, dspChain: { ...state.value.dspChain, order } }
    persist()
    applyToEngine()
  }

  // -- Device profiles -------------------------------------------------
  // Apply a saved per-output-device EQ + crossfeed profile atomically. Used by
  // useAudioDevices when the active output changes.
  function applyAudioProfile(p: AudioProfile) {
    state.value = {
      ...state.value,
      eq: {
        ...state.value.eq,
        enabled: p.eqEnabled,
        bands: p.bands.slice(0, BANDS),
        preamp: clamp(p.preamp, -12, 12),
        postgain: clamp(p.postgain, -12, 12),
        presetName: p.presetName,
      },
      crossfeed: { enabled: p.crossfeedEnabled, preset: p.crossfeedPreset },
    }
    persist()
    applyToEngine()
  }
  // Snapshot the current EQ + crossfeed as a profile (for "save to this device").
  function currentAudioProfile(): AudioProfile {
    const e = state.value.eq
    return {
      eqEnabled: e.enabled,
      bands: [...e.bands],
      preamp: e.preamp,
      postgain: e.postgain,
      presetName: e.presetName,
      crossfeedEnabled: state.value.crossfeed.enabled,
      crossfeedPreset: state.value.crossfeed.preset,
    }
  }

  // -- Engine bridge ---------------------------------------------------
  // Lazy bridge — engine doesn't exist until first user gesture, so we
  // register the apply fn once and let it be called from any setter or
  // from outside (e.g. when usePlayer first creates the engine).
  function registerEngineBridge(fn: () => void) {
    // Idempotent: only wire (and do the initial apply) when no bridge is live.
    // ensureEngine calls this on every invocation so a hot-reload that nulls
    // applyToEngineFn gets re-bridged on the next player interaction, but normal
    // calls are a cheap no-op.
    if (applyToEngineFn) return
    applyToEngineFn = fn
    fn()
  }
  function applyToEngine() { applyToEngineFn?.() }

  return {
    eq, crossfade, replayGain, crossfeed, dspChain, presets,
    setEQEnabled, setEQBand, setPreamp, setPostgain, applyPreset,
    setCrossfadeMode, setCrossfadeDuration, setCrossfadeAlbumAware,
    setReplayGainMode,
    setCrossfeedEnabled, setCrossfeedPreset,
    setLimiterEnabled, moveDspBlock,
    applyAudioProfile, currentAudioProfile,
    registerEngineBridge,
  }
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}
