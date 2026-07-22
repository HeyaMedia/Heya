import { describe, expect, test } from 'bun:test'
import { projectAudioPlaybackClock } from '../app/utils/audioPlaybackClock.ts'
import { shouldSuppressCrossfade } from '../app/engine/crossfade/albumAware.ts'

function sample(overrides = {}) {
  return {
    positionSeconds: 10,
    durationSeconds: 60,
    playing: true,
    paused: false,
    loading: false,
    buffering: false,
    ended: false,
    sampledAtMilliseconds: 1_000,
    ...overrides,
  }
}

describe('audio playback clock projection', () => {
  test('keeps the UI clock advancing when a progress event is dropped', () => {
    expect(projectAudioPlaybackClock(sample(), 2_250).positionSeconds).toBe(11.25)
  })

  test('does not invent progress while paused, buffering, or loading', () => {
    expect(projectAudioPlaybackClock(sample({ playing: false, paused: true }), 5_000).positionSeconds).toBe(10)
    expect(projectAudioPlaybackClock(sample({ buffering: true }), 5_000).positionSeconds).toBe(10)
    expect(projectAudioPlaybackClock(sample({ loading: true }), 5_000).positionSeconds).toBe(10)
  })

  test('clamps an advancing clock at the known duration', () => {
    expect(projectAudioPlaybackClock(sample({ positionSeconds: 59.5 }), 5_000).positionSeconds).toBe(60)
  })
})

describe('album transition policy', () => {
  test('keeps adjacent album tracks and repeat-one loops gapless', () => {
    const first = { trackId: 10, albumId: 3, discNumber: 1, trackNumber: 4 }
    expect(shouldSuppressCrossfade(first, { trackId: 11, albumId: 3, discNumber: 1, trackNumber: 5 })).toBeTrue()
    expect(shouldSuppressCrossfade(first, first)).toBeTrue()
  })

  test('allows crossfade for non-adjacent or different-album queue items', () => {
    const first = { trackId: 10, albumId: 3, discNumber: 1, trackNumber: 4 }
    expect(shouldSuppressCrossfade(first, { trackId: 12, albumId: 3, discNumber: 1, trackNumber: 7 })).toBeFalse()
    expect(shouldSuppressCrossfade(first, { trackId: 13, albumId: 4, discNumber: 1, trackNumber: 5 })).toBeFalse()
  })

  test('falls back to release identity when sequence metadata is absent', () => {
    expect(shouldSuppressCrossfade(
      { albumName: 'Onderwaterwereld' },
      { albumName: 'onderwaterwereld' },
    )).toBeTrue()
  })
})
