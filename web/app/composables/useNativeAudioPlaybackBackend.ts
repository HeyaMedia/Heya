import type {
  HeyaNativeAudioBridge,
  NativeAudioCapabilities,
  NativeAudioCommand,
  NativeAudioLoadRequest,
  NativeAudioOutputDevice,
  NativeAudioProcessingSettings,
  NativeAudioState,
  NativeAudioStateEvent,
  NativeAudioTrackRequest,
  NativeAudioVisualizerEvent,
} from '~/types/native-audio'

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
    outputMode: 'processed',
    bitPerfectActive: false,
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

export interface NativeAudioPlaybackBackend {
  readonly capabilities: Readonly<NativeAudioCapabilities>
  readonly state: NativeAudioState
  readonly visualizer: Readonly<Ref<NativeAudioVisualizerEvent | null>>
  readonly rendererSessionId: Readonly<Ref<string | null>>
  readonly outputDevices: Readonly<Ref<readonly NativeAudioOutputDevice[]>>
  readonly activeOutputDeviceId: Readonly<Ref<string | null>>
  readonly followsSystemDefault: Readonly<Ref<boolean>>
  load(request: NativeAudioLoadRequest): Promise<void>
  setOutputMode(mode: 'processed' | 'bit_perfect'): Promise<void>
  refreshOutputDevices(): Promise<void>
  setOutputDevice(deviceId: string | null): Promise<void>
  preload(track: NativeAudioTrackRequest): Promise<void>
  play(): Promise<void>
  pause(): Promise<void>
  seek(positionSeconds: number): Promise<void>
  setVolume(volume: number): Promise<void>
  setMuted(muted: boolean): Promise<void>
  updateProcessing(settings: NativeAudioProcessingSettings): Promise<void>
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

  function applyState(event: NativeAudioStateEvent) {
    if (event.protocolVersion !== 1
      || event.rendererSessionId !== rendererSessionId.value
      || event.stateRevision <= stateRevision) return
    stateRevision = event.stateRevision
    Object.assign(state, event.payload)
    state.error = event.payload.error
    state.terminationReason = event.payload.terminationReason
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

  async function send(command: CommandPayload) {
    const sessionId = rendererSessionId.value
    if (!sessionId) throw new Error('Native audio has no active renderer session')
    const result = await bridge.sendAudioCommand({
      ...command,
      rendererSessionId: sessionId,
      commandId: commandId(),
    } as NativeAudioCommand)
    if (!result.accepted) throw new Error(result.error?.message ?? 'Native audio command was rejected')
  }

  return {
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
    },
    async setOutputMode(mode) {
      const updated = await bridge.setAudioOutputMode(mode)
      Object.assign(capabilities, updated)
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
    play: () => send({ type: 'play' }),
    pause: () => send({ type: 'pause' }),
    seek: positionSeconds => send({ type: 'seek', positionSeconds }),
    setVolume: volume => send({ type: 'setVolume', volume: Math.max(0, Math.min(1, volume)) }),
    setMuted: muted => send({ type: 'setMuted', muted }),
    updateProcessing: settings => send({ type: 'updateProcessing', settings }),
    stop: () => send({ type: 'stop' }),
    async dispose() {
      loadGeneration++
      const sessionId = rendererSessionId.value
      rendererSessionId.value = null
      if (sessionId) await bridge.disposeAudio({ rendererSessionId: sessionId }).catch(() => {})
      Object.assign(state, initialState())
    },
  }
}
