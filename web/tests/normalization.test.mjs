import { describe, expect, test } from 'bun:test'
import { selectReplayGainLoudness } from '../app/engine/dsp/normalization.ts'

const track = { lufs: -9, peak: 1 }
const album = { lufs: -12, peak: 0 }

describe('replay gain measurement selection', () => {
  test('disables normalization in off mode', () => {
    expect(selectReplayGainLoudness('off', track, album, true)).toBeNull()
  })

  test('uses track loudness in track mode', () => {
    expect(selectReplayGainLoudness('track', track, album, true)).toEqual(track)
  })

  test('uses album loudness in explicit album mode', () => {
    expect(selectReplayGainLoudness('album', track, album, false)).toEqual(album)
  })

  test('uses album loudness in auto mode only for album playback', () => {
    expect(selectReplayGainLoudness('auto', track, album, true)).toEqual(album)
    expect(selectReplayGainLoudness('auto', track, album, false)).toEqual(track)
  })

  test('falls back to track loudness when album analysis is incomplete', () => {
    expect(selectReplayGainLoudness(
      'auto',
      track,
      { lufs: -12, peak: null },
      true,
    )).toEqual(track)
  })
})
