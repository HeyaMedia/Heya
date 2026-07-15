import type {
  VideoPlaybackBackend,
  VideoPlaybackCapabilities,
  VideoPlaybackControls,
} from '~/types/video-playback'

export interface BrowserVideoPlaybackLoadRequest {
  url: string
  bearerToken?: string
}

const capabilities = Object.freeze({
  backend: 'browser',
  videoSurface: 'html-media-element',
  diagnostics: true,
  audioTrackSelection: true,
  subtitleTrackSelection: true,
  qualitySelection: true,
} satisfies VideoPlaybackCapabilities)

// Adapter around the existing HTMLMediaElement/HLS implementation. Keeping
// this wrapper behavior-free lets VideoPlayer depend on a renderer contract
// while browser/PWA playback remains the unchanged default.
export function useBrowserVideoPlaybackBackend(
  videoRef: Ref<HTMLVideoElement | undefined>,
): VideoPlaybackBackend<BrowserVideoPlaybackLoadRequest> {
  const player = useHeyaPlayer(videoRef)

  const controls: VideoPlaybackControls = {
    play: player.controls.play,
    pause: player.controls.pause,
    seek: player.controls.seek,
    setVolume: player.controls.setVolume,
    setMuted(muted) {
      if (player.state.muted !== muted) player.controls.toggleMute()
    },
    setFullscreen(fullscreen) {
      if (player.state.fullscreen !== fullscreen) player.controls.toggleFullscreen()
    },
  }

  return {
    kind: 'browser',
    capabilities,
    state: player.state,
    diagnostics: player.diagnostics,
    controls,
    load(request) {
      player.loadSource(request.url, request.bearerToken)
    },
    dispose: player.destroyHLS,
  }
}
