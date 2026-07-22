import type { AudioPlaybackClockSample } from '~/types/audio-playback'

export function projectAudioPlaybackClock(
  sample: AudioPlaybackClockSample,
  nowMilliseconds: number,
): AudioPlaybackClockSample {
  const duration = Number.isFinite(sample.durationSeconds)
    ? Math.max(0, sample.durationSeconds)
    : 0
  const basePosition = Number.isFinite(sample.positionSeconds)
    ? Math.max(0, sample.positionSeconds)
    : 0
  const elapsed = sample.playing && !sample.loading && !sample.buffering && !sample.ended
    ? Math.max(0, nowMilliseconds - sample.sampledAtMilliseconds) / 1000
    : 0
  const position = duration > 0
    ? Math.min(duration, basePosition + elapsed)
    : basePosition + elapsed

  return {
    ...sample,
    positionSeconds: position,
    durationSeconds: duration,
    sampledAtMilliseconds: nowMilliseconds,
  }
}
