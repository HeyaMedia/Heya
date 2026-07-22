import { acceptHMRUpdate, defineStore } from 'pinia'
import type { CrossfeedPreset } from '~~/shared/types/audio'
import { BUILTIN_PRESETS } from '~/engine/dsp/equalizerPresets'
import { clampTargetLufs, DEFAULT_TARGET_LUFS } from '~/engine/dsp/normalization'

export const AUDIO_BANDS = 10
const STORAGE_KEY = 'heya_audio_settings_v1'

export interface EQState {
  enabled: boolean
  preamp: number
  postgain: number
  bands: number[]
  presetName: string | null
}

export interface CrossfadeState {
  mode: 'gapless' | 'crossfade' | 'smart'
  durationSeconds: number
}

export interface ReplayGainState {
  mode: 'off' | 'track' | 'album' | 'auto'
  // Loudness target in LUFS. Feeds computeNormalizationGain for the browser
  // engines and the precomputed gainDb shipped to HeyaClient's Rust engine.
  targetLufs: number
}

export interface CrossfeedState {
  enabled: boolean
  preset: CrossfeedPreset
}

export type DspBlockId = 'equalizer' | 'crossfeed'

export interface AudioProfile {
  eqEnabled: boolean
  bands: number[]
  preamp: number
  postgain: number
  presetName: string | null
  crossfeedEnabled: boolean
  crossfeedPreset: CrossfeedPreset
}

export interface DspChainState {
  order: DspBlockId[]
  limiterEnabled: boolean
}

export interface StoredAudioSettings {
  eq: EQState
  crossfade: CrossfadeState
  replayGain: ReplayGainState
  crossfeed: CrossfeedState
  dspChain: DspChainState
}

const DEFAULTS: StoredAudioSettings = {
  eq: { enabled: false, preamp: 0, postgain: 0, bands: Array(AUDIO_BANDS).fill(0), presetName: 'Flat' },
  crossfade: { mode: 'gapless', durationSeconds: 3 },
  replayGain: { mode: 'auto', targetLufs: DEFAULT_TARGET_LUFS },
  crossfeed: { enabled: false, preset: 'natural' },
  dspChain: { order: ['equalizer', 'crossfeed'], limiterEnabled: true },
}

const DSP_BLOCKS: DspBlockId[] = ['equalizer', 'crossfeed']
let applyToEngineFn: (() => void) | null = null
let applyToEngineOwner: object | null = null

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}

function defaults(): StoredAudioSettings {
  return {
    ...DEFAULTS,
    eq: { ...DEFAULTS.eq, bands: [...DEFAULTS.eq.bands] },
    crossfade: { ...DEFAULTS.crossfade },
    replayGain: { ...DEFAULTS.replayGain },
    crossfeed: { ...DEFAULTS.crossfeed },
    dspChain: { ...DEFAULTS.dspChain, order: [...DEFAULTS.dspChain.order] },
  }
}

function sanitizeChain(value?: Partial<DspChainState>): DspChainState {
  const seen = new Set<DspBlockId>()
  const order: DspBlockId[] = []
  for (const id of value?.order ?? []) {
    if (DSP_BLOCKS.includes(id) && !seen.has(id)) { seen.add(id); order.push(id) }
  }
  for (const id of DSP_BLOCKS) if (!seen.has(id)) order.push(id)
  return { order, limiterEnabled: value?.limiterEnabled ?? DEFAULTS.dspChain.limiterEnabled }
}

function loadInitial(): StoredAudioSettings {
  const fallback = defaults()
  if (import.meta.server) return fallback
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<StoredAudioSettings>
    return {
      eq: { ...fallback.eq, ...parsed.eq, bands: (parsed.eq?.bands ?? fallback.eq.bands).slice(0, AUDIO_BANDS) },
      crossfade: { ...fallback.crossfade, ...parsed.crossfade },
      replayGain: {
        mode: parsed.replayGain?.mode ?? fallback.replayGain.mode,
        targetLufs: clampTargetLufs(parsed.replayGain?.targetLufs ?? fallback.replayGain.targetLufs),
      },
      crossfeed: { ...fallback.crossfeed, ...parsed.crossfeed },
      dspChain: sanitizeChain(parsed.dspChain),
    }
  } catch {
    return fallback
  }
}

export const useAudioSettingsStore = defineStore('audio-settings', () => {
  const settings = ref<StoredAudioSettings>(loadInitial())
  const eq = computed(() => settings.value.eq)
  const crossfade = computed(() => settings.value.crossfade)
  const replayGain = computed(() => settings.value.replayGain)
  const crossfeed = computed(() => settings.value.crossfeed)
  const dspChain = computed(() => settings.value.dspChain)

  function persist() {
    if (import.meta.server) return
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings.value)) } catch { /* non-fatal */ }
  }

  function applyToEngine() { applyToEngineFn?.() }
  function commit(next: StoredAudioSettings) {
    settings.value = next
    persist()
    applyToEngine()
  }

  function setEQEnabled(enabled: boolean) {
    commit({ ...settings.value, eq: { ...settings.value.eq, enabled } })
  }
  function setEQBand(index: number, valueDb: number) {
    if (index < 0 || index >= AUDIO_BANDS) return
    const bands = [...settings.value.eq.bands]
    bands[index] = clamp(valueDb, -12, 12)
    commit({ ...settings.value, eq: { ...settings.value.eq, bands, presetName: null } })
  }
  function setPreamp(valueDb: number) {
    commit({ ...settings.value, eq: { ...settings.value.eq, preamp: clamp(valueDb, -12, 12), presetName: null } })
  }
  function setPostgain(valueDb: number) {
    commit({ ...settings.value, eq: { ...settings.value.eq, postgain: clamp(valueDb, -12, 12), presetName: null } })
  }
  function applyPreset(name: string) {
    const preset = BUILTIN_PRESETS.find(item => item.name === name)
    if (!preset) return
    commit({
      ...settings.value,
      eq: {
        ...settings.value.eq,
        bands: preset.bands.slice(0, AUDIO_BANDS),
        preamp: preset.preamp,
        postgain: preset.postgain,
        presetName: name,
      },
    })
  }
  function setCrossfadeMode(mode: CrossfadeState['mode']) {
    commit({ ...settings.value, crossfade: { ...settings.value.crossfade, mode } })
  }
  function setCrossfadeDuration(seconds: number) {
    commit({ ...settings.value, crossfade: { ...settings.value.crossfade, durationSeconds: clamp(seconds, 1, 12) } })
  }
  function setReplayGainMode(mode: ReplayGainState['mode']) {
    commit({ ...settings.value, replayGain: { ...settings.value.replayGain, mode } })
  }
  function setReplayGainTarget(targetLufs: number) {
    commit({
      ...settings.value,
      replayGain: { ...settings.value.replayGain, targetLufs: clampTargetLufs(targetLufs) },
    })
  }
  function setCrossfeedEnabled(enabled: boolean) {
    commit({ ...settings.value, crossfeed: { ...settings.value.crossfeed, enabled } })
  }
  function setCrossfeedPreset(preset: CrossfeedPreset) {
    commit({ ...settings.value, crossfeed: { ...settings.value.crossfeed, preset } })
  }
  function setLimiterEnabled(enabled: boolean) {
    commit({ ...settings.value, dspChain: { ...settings.value.dspChain, limiterEnabled: enabled } })
  }
  function moveDspBlock(id: DspBlockId, direction: -1 | 1) {
    const order = [...settings.value.dspChain.order]
    const from = order.indexOf(id)
    const to = from + direction
    if (from < 0 || to < 0 || to >= order.length) return
    ;[order[from], order[to]] = [order[to]!, order[from]!]
    commit({ ...settings.value, dspChain: { ...settings.value.dspChain, order } })
  }
  function applyAudioProfile(profile: AudioProfile) {
    commit({
      ...settings.value,
      eq: {
        ...settings.value.eq,
        enabled: profile.eqEnabled,
        bands: profile.bands.slice(0, AUDIO_BANDS),
        preamp: clamp(profile.preamp, -12, 12),
        postgain: clamp(profile.postgain, -12, 12),
        presetName: profile.presetName,
      },
      crossfeed: { enabled: profile.crossfeedEnabled, preset: profile.crossfeedPreset },
    })
  }
  function currentAudioProfile(): AudioProfile {
    return {
      eqEnabled: settings.value.eq.enabled,
      bands: [...settings.value.eq.bands],
      preamp: settings.value.eq.preamp,
      postgain: settings.value.eq.postgain,
      presetName: settings.value.eq.presetName,
      crossfeedEnabled: settings.value.crossfeed.enabled,
      crossfeedPreset: settings.value.crossfeed.preset,
    }
  }
  function resetAudioProfile() {
    // Output-bound profiles own only transducer-specific processing. Playback
    // behavior (ReplayGain, crossfade, limiter/order) remains global.
    commit({
      ...settings.value,
      eq: { ...DEFAULTS.eq, bands: [...DEFAULTS.eq.bands] },
      crossfeed: { ...DEFAULTS.crossfeed },
    })
  }
  function registerEngineBridge(owner: object, fn: () => void) {
    // The active playback backend owns this single bridge. Switching between
    // browser WebAudio and HeyaClient's native Rust engine replaces it so
    // settings never update an inactive renderer. Re-registering the current
    // owner is intentionally a no-op: the initial application can prepare a
    // transition, which re-enters ensureEngine() and would otherwise recurse.
    if (applyToEngineOwner === owner && applyToEngineFn) return
    applyToEngineOwner = owner
    applyToEngineFn = fn
    fn()
  }

  return {
    settings, eq, crossfade, replayGain, crossfeed, dspChain,
    presets: BUILTIN_PRESETS,
    setEQEnabled, setEQBand, setPreamp, setPostgain, applyPreset,
    setCrossfadeMode, setCrossfadeDuration,
    setReplayGainMode, setReplayGainTarget, setCrossfeedEnabled, setCrossfeedPreset,
    setLimiterEnabled, moveDspBlock,
    applyAudioProfile, currentAudioProfile, resetAudioProfile, registerEngineBridge,
    applyToEngine,
  }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(useAudioSettingsStore, import.meta.hot))
