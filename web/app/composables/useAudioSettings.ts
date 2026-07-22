import { storeToRefs } from 'pinia'
import { useAudioSettingsStore } from '~/stores/audio-settings'

/** Template-friendly bindings for the Pinia DSP domain. Direct integrations
 * can use useAudioSettingsStore(); destructuring consumers use this helper so
 * state remains reactive refs while actions stay bound Pinia actions. */
export function useAudioSettingsBindings() {
  const store = useAudioSettingsStore()
  return {
    ...storeToRefs(store),
    presets: store.presets,
    setEQEnabled: store.setEQEnabled,
    setEQBand: store.setEQBand,
    setPreamp: store.setPreamp,
    setPostgain: store.setPostgain,
    applyPreset: store.applyPreset,
    setCrossfadeMode: store.setCrossfadeMode,
    setCrossfadeDuration: store.setCrossfadeDuration,
    setReplayGainMode: store.setReplayGainMode,
    setReplayGainTarget: store.setReplayGainTarget,
    setCrossfeedEnabled: store.setCrossfeedEnabled,
    setCrossfeedPreset: store.setCrossfeedPreset,
    setLimiterEnabled: store.setLimiterEnabled,
    moveDspBlock: store.moveDspBlock,
    applyAudioProfile: store.applyAudioProfile,
    currentAudioProfile: store.currentAudioProfile,
    registerEngineBridge: store.registerEngineBridge,
  }
}
