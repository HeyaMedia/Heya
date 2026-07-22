import type {
  HeyaNativeAudioBridge,
  NativeAudioCapabilities,
  NativeAudioCommand,
  NativeAudioLoadRequest,
  NativeAudioOutputDevice,
  NativeAudioProcessingSettings,
  NativeAudioState,
  NativeAudioStateEvent,
  NativeAudioTrackAnalysisUpdate,
  NativeAudioTrackRequest,
  NativeAudioVisualizerEvent,
} from '~/types/native-audio'
import type { AudioPlaybackClockSample, AudioPlaybackClockSource } from '~/types/audio-playback'
import { projectAudioPlaybackClock } from '~/utils/audioPlaybackClock'

type CommandPayload = NativeAudioCommand extends infer Command
  ? Command extends NativeAudioCommand
    ? Omit<Command, 'rendererSessionId' | 'commandId'>
    : never
  : never

function initialState(): NativeAudioState {
  return {
    playing: false,
    paused: true,
    loading: false,
    buffering: false,
    ended: false,
    positionSeconds: 0,
    durationSeconds: 0,
    volume: 1,
    muted: false,
    currentTrackId: null,
    startedTrackId: null,
    endedTrackId: null,
    sourceSampleRateHz: null,
    sourceChannels: null,
    outputSampleRateHz: null,
    outputChannels: null,
    outputDeviceId: null,
    outputDeviceName: null,
    resamplerActive: false,
    dspActive: false,
  }
}

function commandId(): string {
  return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

export interface NativeAudioPlaybackBackend extends AudioPlaybackClockSource {
  readonly kind: 'native'
  readonly capabilities: Readonly<NativeAudioCapabilities>
  readonly state: NativeAudioState
  readonly visualizer: Readonly<Ref<NativeAudioVisualizerEvent | null>>
  readonly rendererSessionId: Readonly<Ref<string | null>>
  readonly outputDevices: Readonly<Ref<readonly NativeAudioOutputDevice[]>>
  readonly activeOutputDeviceId: Readonly<Ref<string | null>>
  readonly followsSystemDefault: Readonly<Ref<boolean>>
  load(request: NativeAudioLoadRequest): Promise<void>
  refreshOutputDevices(): Promise<void>
  setOutputDevice(deviceId: string | null): Promise<void>
  preload(track: NativeAudioTrackRequest): Promise<void>
  play(): Promise<void>
  pause(): Promise<void>
  seek(positionSeconds: number): Promise<void>
  setVolume(volume: number): Promise<void>
  setMuted(muted: boolean): Promise<void>
  updateProcessing(settings: NativeAudioProcessingSettings): Promise<void>
  updateTrackAnalysis(update: NativeAudioTrackAnalysisUpdate): Promise<void>
  stop(): Promise<void>
  dispose(): Promise<void>
}

export function useNativeAudioPlaybackBackend(
  bridge: Readonly<HeyaNativeAudioBridge>,
  capabilities: Readonly<NativeAudioCapabilities>,
): NativeAudioPlaybackBackend {
  const state = reactive<NativeAudioState>(initialState())
  const visualizer = shallowRef<NativeAudioVisualizerEvent | null>(null)
  const rendererSessionId = ref<string | null>(null)
  const outputDevices = ref<NativeAudioOutputDevice[]>([])
  const activeOutputDeviceId = ref<string | null>(null)
  const followsSystemDefault = ref(true)
  const pendingStates = new Map<string, NativeAudioStateEvent>()
  let stateRevision = 0
  let visualizerRevision = 0
  let loadGeneration = 0
  let authoritativeClock: AudioPlaybackClockSample = {
    positionSeconds: 0,
    durationSeconds: 0,
    playing: false,
    paused: true,
    loading: false,
    buffering: false,
    ended: false,
    sampledAtMilliseconds: performance.now(),
  }

  function rememberClock() {
    authoritativeClock = {
      positionSeconds: state.positionSeconds,
      durationSeconds: state.durationSeconds,
      playing: state.playing,
      paused: state.paused,
      loading: state.loading,
      buffering: state.buffering,
      ended: state.ended,
      sampledAtMilliseconds: performance.now(),
    }
  }

  function applyState(event: NativeAudioStateEvent) {
    if (event.protocolVersion !== 2
      || event.rendererSessionId !== rendererSessionId.value
      || event.stateRevision <= stateRevision) return
    stateRevision = event.stateRevision
    Object.assign(state, event.payload)
    state.error = event.payload.error
    state.terminationReason = event.payload.terminationReason
    rememberClock()
  }

  const unsubscribeState = bridge.subscribeAudioState((event) => {
    if (event.rendererSessionId === rendererSessionId.value) applyState(event)
    else {
      pendingStates.set(event.rendererSessionId, event)
      while (pendingStates.size > 4) pendingStates.delete(pendingStates.keys().next().value!)
    }
  })
  const unsubscribeVisualizer = bridge.subscribeAudioVisualizer((event) => {
    if (event.rendererSessionId !== rendererSessionId.value
      || event.visualizerRevision <= visualizerRevision) return
    visualizerRevision = event.visualizerRevision
    visualizer.value = event
  })

  async function send(command: CommandPayload, reconcile = false) {
    const sessionId = rendererSessionId.value
    if (!sessionId) throw new Error('Native audio has no active renderer session')
    const result = await bridge.sendAudioCommand({
      ...command,
      rendererSessionId: sessionId,
      commandId: commandId(),
    } as NativeAudioCommand)
    if (!result.accepted) throw new Error(result.error?.message ?? 'Native audio command was rejected')
    if (reconcile) {
      // Command acceptance means queued, not yet rendered. Give the callback a
      // deadline, then verify the resulting state through the v2 pull path.
      await new Promise(resolve => setTimeout(resolve, 60))
      await reconcileClock()
    }
  }

  async function reconcileClock() {
    const sessionId = rendererSessionId.value
    if (!sessionId) return
    applyState(await bridge.getAudioState({ rendererSessionId: sessionId }))
  }

  return {
    kind: 'native',
    capabilities,
    state,
    visualizer: readonly(visualizer),
    rendererSessionId: readonly(rendererSessionId),
    outputDevices: readonly(outputDevices),
    activeOutputDeviceId: readonly(activeOutputDeviceId),
    followsSystemDefault: readonly(followsSystemDefault),
    async load(request) {
      const generation = ++loadGeneration
      rendererSessionId.value = null
      stateRevision = 0
      visualizerRevision = 0
      visualizer.value = null
      Object.assign(state, initialState(), { loading: true })
      const result = await bridge.loadAudio(request)
      if (generation !== loadGeneration) {
        await bridge.disposeAudio({ rendererSessionId: result.rendererSessionId }).catch(() => {})
        return
      }
      rendererSessionId.value = result.rendererSessionId
      const pending = pendingStates.get(result.rendererSessionId)
      if (pending) applyState(pending)
      pendingStates.clear()
      // Protocol v2 never trusts event delivery as the only source of truth.
      // Read Rust's PCM-frame clock before handing ownership to the UI.
      await reconcileClock()
    },
    async refreshOutputDevices() {
      const snapshot = await bridge.getAudioOutputDevices()
      outputDevices.value = snapshot.devices
      activeOutputDeviceId.value = snapshot.activeDeviceId
      followsSystemDefault.value = snapshot.followsSystemDefault
    },
    async setOutputDevice(deviceId) {
      const snapshot = await bridge.setAudioOutputDevice(deviceId)
      outputDevices.value = snapshot.devices
      activeOutputDeviceId.value = snapshot.activeDeviceId
      followsSystemDefault.value = snapshot.followsSystemDefault
    },
    async preload(track) {
      const sessionId = rendererSessionId.value
      if (!sessionId) return
      const result = await bridge.preloadNextAudio({
        rendererSessionId: sessionId,
        commandId: commandId(),
        track,
      })
      if (!result.accepted) throw new Error(result.error?.message ?? 'Native audio preload was rejected')
    },
    play: () => send({ type: 'play' }, true),
    pause: () => send({ type: 'pause' }, true),
    seek: positionSeconds => send({ type: 'seek', positionSeconds }, true),
    setVolume: volume => send({ type: 'setVolume', volume: Math.max(0, Math.min(1, volume)) }),
    setMuted: muted => send({ type: 'setMuted', muted }),
    updateProcessing: settings => send({ type: 'updateProcessing', settings }),
    updateTrackAnalysis: update => send({ type: 'updateTrackAnalysis', ...update }),
    stop: () => send({ type: 'stop' }, true),
    readClock() {
      return projectAudioPlaybackClock(authoritativeClock, performance.now())
    },
    reconcileClock,
    async dispose() {
      loadGeneration++
      const sessionId = rendererSessionId.value
      rendererSessionId.value = null
      if (sessionId) await bridge.disposeAudio({ rendererSessionId: sessionId }).catch(() => {})
      Object.assign(state, initialState())
      rememberClock()
    },
  }
}
