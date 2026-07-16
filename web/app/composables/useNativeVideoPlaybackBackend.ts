import type {
  HeyaNativePlaybackBridge,
  NativePlaybackCapabilities,
  NativePlaybackCommand,
  NativePlaybackDiagnosticsEvent,
  NativePlaybackLoadRequest,
  NativePlaybackState,
  NativePlaybackStateEvent,
  NativePlaybackTrack,
} from '~/types/native-playback'
import type {
  VideoPlaybackBackend,
  VideoPlaybackCapabilities,
  VideoPlaybackDiagnostics,
  VideoPlaybackState,
} from '~/types/video-playback'

type NativePlaybackCommandPayload = NativePlaybackCommand extends infer Command
  ? Command extends NativePlaybackCommand
    ? Omit<Command, 'rendererSessionId' | 'commandId'>
    : never
  : never

export interface NativeVideoPlaybackBackend extends VideoPlaybackBackend<NativePlaybackLoadRequest> {
  readonly nativeState: NativePlaybackState
  readonly rendererSessionId: Readonly<Ref<string | null>>
  readonly activeVideoSurface: Readonly<Ref<'native-surface' | 'native-window'>>
  readonly audioTracks: ComputedRef<NativePlaybackTrack[]>
  readonly subtitleTracks: ComputedRef<NativePlaybackTrack[]>
  selectAudioTrack: (trackId: string) => Promise<void>
  selectSubtitleTrack: (trackId: string | null) => Promise<void>
}

function initialNativeState(): NativePlaybackState {
  return {
    playing: false,
    paused: true,
    ended: false,
    loading: true,
    buffering: false,
    videoSurfaceReady: false,
    currentTime: 0,
    duration: 0,
    buffered: 0,
    volume: 1,
    muted: false,
    fullscreen: false,
    seekRevision: 0,
    audioTracks: [],
    subtitleTracks: [],
  }
}

function commandId(): string {
  return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

export function useNativeVideoPlaybackBackend(
  bridge: Readonly<HeyaNativePlaybackBridge>,
  nativeCapabilities: Readonly<NativePlaybackCapabilities>,
): NativeVideoPlaybackBackend {
  const capabilities = Object.freeze({
    backend: 'mpv',
    videoSurface: nativeCapabilities.videoSurface,
    diagnostics: nativeCapabilities.diagnostics,
    audioTrackSelection: nativeCapabilities.audioTrackSelection,
    subtitleTrackSelection: nativeCapabilities.subtitleTrackSelection,
    qualitySelection: nativeCapabilities.qualitySelection,
  } satisfies VideoPlaybackCapabilities)
  const nativeState = reactive<NativePlaybackState>(initialNativeState())
  const state = reactive<VideoPlaybackState>({
    playing: false,
    paused: true,
    ended: false,
    loading: true,
    buffering: false,
    currentTime: 0,
    duration: 0,
    buffered: 0,
    volume: 1,
    muted: false,
    fullscreen: false,
    error: null,
    seekRevision: 0,
  })
  const diagnostics = reactive<VideoPlaybackDiagnostics>({ backend: 'mpv' })
  const pendingStates = new Map<string, NativePlaybackStateEvent>()
  const pendingDiagnostics = new Map<string, NativePlaybackDiagnosticsEvent>()
  const rendererSessionId = ref<string | null>(null)
  // Stay visually opaque until loadPlayback confirms that an embedded
  // surface was actually attached. The macOS adapter may safely fall back to
  // a separate native window for this individual session.
  const activeVideoSurface = ref<'native-surface' | 'native-window'>('native-window')
  let stateRevision = 0
  let diagnosticsRevision = 0
  let loadGeneration = 0
  let disposed = false

  function applyState(event: NativePlaybackStateEvent) {
    if (event.protocolVersion !== 1 || event.rendererSessionId !== rendererSessionId.value || event.stateRevision <= stateRevision) return
    stateRevision = event.stateRevision
    Object.assign(nativeState, event.payload)
    nativeState.selectedAudioTrackId = event.payload.selectedAudioTrackId
    nativeState.selectedSubtitleTrackId = event.payload.selectedSubtitleTrackId
    nativeState.error = event.payload.error
    nativeState.terminationReason = event.payload.terminationReason
    const terminationError = event.payload.terminationReason === 'failed'
      ? 'Native playback failed'
      : event.payload.terminationReason === 'native_crashed'
        ? 'The native playback engine stopped unexpectedly'
        : null
    Object.assign(state, {
      playing: event.payload.playing,
      paused: event.payload.paused,
      ended: event.payload.ended,
      loading: event.payload.loading,
      buffering: event.payload.buffering,
      currentTime: event.payload.currentTime,
      duration: event.payload.duration,
      buffered: event.payload.buffered,
      volume: event.payload.volume,
      muted: event.payload.muted,
      fullscreen: event.payload.fullscreen,
      error: event.payload.error?.message ?? terminationError,
      seekRevision: event.payload.seekRevision,
    })
  }

  function applyDiagnostics(event: NativePlaybackDiagnosticsEvent) {
    if (event.protocolVersion !== 1 || event.rendererSessionId !== rendererSessionId.value || event.diagnosticsRevision <= diagnosticsRevision) return
    diagnosticsRevision = event.diagnosticsRevision
    for (const key of Object.keys(diagnostics) as (keyof VideoPlaybackDiagnostics)[]) {
      if (key !== 'backend') delete (diagnostics as any)[key]
    }
    if (event.payload) Object.assign(diagnostics, event.payload)
    diagnostics.backend = 'mpv'
  }

  function rememberPending<T extends NativePlaybackStateEvent | NativePlaybackDiagnosticsEvent>(map: Map<string, T>, event: T) {
    map.set(event.rendererSessionId, event)
    while (map.size > 4) map.delete(map.keys().next().value!)
  }

  const unsubscribeState = bridge.subscribePlaybackState((event) => {
    if (event.rendererSessionId === rendererSessionId.value) applyState(event)
    else rememberPending(pendingStates, event)
  })
  const unsubscribeDiagnostics = bridge.subscribePlaybackDiagnostics((event) => {
    if (event.rendererSessionId === rendererSessionId.value) applyDiagnostics(event)
    else rememberPending(pendingDiagnostics, event)
  })

  async function send(command: NativePlaybackCommandPayload) {
    const sessionId = rendererSessionId.value
    if (!sessionId || disposed) return
    try {
      const result = await bridge.sendPlaybackCommand({
        ...command,
        rendererSessionId: sessionId,
        commandId: commandId(),
      } as NativePlaybackCommand)
      if (!result.accepted) state.error = result.error?.message ?? 'Native playback command was rejected'
    } catch (error) {
      state.error = error instanceof Error ? error.message : 'Native playback command failed'
    }
  }

  function setDesiredTransportState(paused: boolean) {
    nativeState.paused = paused
    nativeState.playing = !paused
    nativeState.ended = false
    state.paused = paused
    state.playing = !paused
    state.ended = false
  }

  return {
    kind: 'mpv',
    capabilities,
    state,
    diagnostics,
    nativeState,
    rendererSessionId: readonly(rendererSessionId),
    activeVideoSurface: readonly(activeVideoSurface),
    audioTracks: computed(() => nativeState.audioTracks),
    subtitleTracks: computed(() => nativeState.subtitleTracks),
    controls: {
      play: () => {
        setDesiredTransportState(false)
        return send({ type: 'play' })
      },
      pause: () => {
        setDesiredTransportState(true)
        return send({ type: 'pause' })
      },
      seek: positionSeconds => send({ type: 'seek', positionSeconds }),
      setVolume: volume => send({ type: 'setVolume', volume: Math.max(0, Math.min(1, volume)) }),
      setMuted: muted => send({ type: 'setMuted', muted }),
      setFullscreen: fullscreen => send({ type: 'setFullscreen', fullscreen }),
    },
    selectAudioTrack: trackId => send({ type: 'selectAudioTrack', trackId }),
    selectSubtitleTrack: trackId => send({ type: 'selectSubtitleTrack', trackId }),
    async load(request) {
      const generation = ++loadGeneration
      disposed = false
      rendererSessionId.value = null
      activeVideoSurface.value = 'native-window'
      stateRevision = 0
      diagnosticsRevision = 0
      Object.assign(nativeState, initialNativeState())
      nativeState.selectedAudioTrackId = undefined
      nativeState.selectedSubtitleTrackId = undefined
      nativeState.error = undefined
      nativeState.terminationReason = undefined
      Object.assign(state, initialNativeState(), { error: null })
      const result = await bridge.loadPlayback(request)
      if (generation !== loadGeneration) {
        await bridge.disposePlayback(result).catch(() => {})
        return
      }
      rendererSessionId.value = result.rendererSessionId
      activeVideoSurface.value = result.videoSurface
      const pendingState = pendingStates.get(rendererSessionId.value)
      if (pendingState) applyState(pendingState)
      const pendingDiagnostic = pendingDiagnostics.get(rendererSessionId.value)
      if (pendingDiagnostic) applyDiagnostics(pendingDiagnostic)
      pendingStates.clear()
      pendingDiagnostics.clear()
    },
    async dispose() {
      if (disposed) return
      disposed = true
      loadGeneration++
      const sessionId = rendererSessionId.value
      rendererSessionId.value = null
      unsubscribeState()
      unsubscribeDiagnostics()
      if (sessionId) await bridge.disposePlayback({ rendererSessionId: sessionId }).catch(() => {})
    },
  }
}
