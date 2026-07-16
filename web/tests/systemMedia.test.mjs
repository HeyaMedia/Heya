import { describe, expect, test } from 'bun:test'
import {
  clampSystemMediaPosition,
  systemMediaArtworkKey,
  systemMediaItemKey,
  systemMediaNotificationBody,
} from '../app/utils/systemMedia.ts'

describe('system media normalization', () => {
  test('keeps library tracks stable while distinguishing radio metadata changes', () => {
    const first = { id: -7, title: 'First song', artist: 'Radio artist', album: '' }
    const second = { ...first, title: 'Second song' }
    expect(systemMediaItemKey(first)).toBe(systemMediaItemKey({ ...first }))
    expect(systemMediaItemKey(second)).not.toBe(systemMediaItemKey(first))
    expect(systemMediaItemKey(first).length).toBeLessThan(64)
    expect(systemMediaArtworkKey('/api/music/artists/a/albums/b/cover')).toMatch(/^art:[0-9a-f]{8}$/)
  })

  test('builds compact notification copy without empty separators', () => {
    expect(systemMediaNotificationBody({ id: 1, title: 'Song', artist: 'Artist', album: 'Album' }))
      .toBe('Artist · Album')
    expect(systemMediaNotificationBody({ id: 1, title: 'Song', artist: '', album: '' }))
      .toBe('Now playing in Heya')
  })

  test('clamps invalid and overshooting positions', () => {
    expect(clampSystemMediaPosition(12, 100)).toBe(12)
    expect(clampSystemMediaPosition(120, 100)).toBe(100)
    expect(clampSystemMediaPosition(Number.NaN, 100)).toBe(0)
    expect(clampSystemMediaPosition(5, 0)).toBe(0)
  })
})
