import type { Track } from '~/composables/usePlayer'

export type MusicUltrablurSource = 'poster' | 'artist' | 'backdrop' | 'generated'

export interface MusicUltrablurTarget {
  /** Changes for every song, even when two songs share the same album art. */
  key: string
  /** Stable identity used to keep late facet/image work off the next song. */
  trackKey: string
  source: MusicUltrablurSource
  imageUrl: string | null
  /** Deterministic color bed under an image, or the complete generated fallback. */
  background: string
}

interface MusicUltrablurCandidate {
  source: Exclude<MusicUltrablurSource, 'generated'>
  url: string
}

/** Small stable hash: deterministic across reloads, unlike Math.random(). */
export function musicUltrablurHash(value: string) {
  let hash = 0x811c9dc5
  for (let i = 0; i < value.length; i++) {
    hash ^= value.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

function seededRandom(seed: number) {
  let state = seed || 0x6d2b79f5
  return () => {
    state += 0x6d2b79f5
    let n = state
    n = Math.imul(n ^ (n >>> 15), n | 1)
    n ^= n + Math.imul(n ^ (n >>> 7), n | 61)
    return ((n ^ (n >>> 14)) >>> 0) / 4294967296
  }
}

/**
 * A broad, already-soft color field for tracks with no usable artwork.
 * Hues, focal points, and falloff all come from the track identity, so the
 * same song is recognizable between sessions without storing generated art.
 */
export function musicUltrablurGradient(seedText: string) {
  const random = seededRandom(musicUltrablurHash(seedText))
  const hueA = Math.round(random() * 359)
  const hueB = Math.round((hueA + 55 + random() * 100) % 360)
  const hueC = Math.round((hueA + 175 + random() * 100) % 360)
  const xA = Math.round(8 + random() * 64)
  const yA = Math.round(2 + random() * 48)
  const xB = Math.round(35 + random() * 60)
  const yB = Math.round(38 + random() * 58)
  const xC = Math.round(4 + random() * 80)
  const yC = Math.round(48 + random() * 48)

  return [
    `radial-gradient(ellipse 82% 70% at ${xA}% ${yA}%, hsl(${hueA} 78% 48% / 0.94) 0%, transparent 68%)`,
    `radial-gradient(ellipse 76% 72% at ${xB}% ${yB}%, hsl(${hueB} 72% 42% / 0.86) 0%, transparent 70%)`,
    `radial-gradient(ellipse 68% 64% at ${xC}% ${yC}%, hsl(${hueC} 66% 34% / 0.78) 0%, transparent 72%)`,
    `linear-gradient(145deg, hsl(${hueC} 52% 17%), hsl(${hueA} 58% 12%) 58%, hsl(${hueB} 52% 10%))`,
  ].join(', ')
}

function trackIdentity(track: Track) {
  return [
    track.id,
    track.artist,
    track.title,
    track.album,
  ].map(value => String(value ?? '').trim().toLocaleLowerCase()).join('|')
}

function fallbackSeed(track: Track, genres: string[]) {
  return [track.artist, track.title, ...genres]
    .map(value => value.trim().toLocaleLowerCase())
    .filter(Boolean)
    .join('|')
}

function artworkCandidates(track: Track): MusicUltrablurCandidate[] {
  const candidates: MusicUltrablurCandidate[] = []
  if (track.poster) candidates.push({ source: 'poster', url: track.poster })

  // Music artist pages address their media item by slug. The numeric artist
  // id remains a useful last-resort key for older/synthetic queue entries.
  const artistKey = track.artist_slug || track.artist_id
  if (artistKey) {
    candidates.push({ source: 'artist', url: `/api/media/${artistKey}/image/poster` })
    candidates.push({ source: 'backdrop', url: `/api/media/${artistKey}/image/backdrop` })
  }

  const seen = new Set<string>()
  return candidates.filter(({ url }) => {
    if (seen.has(url)) return false
    seen.add(url)
    return true
  })
}

/**
 * Resolves one mobile player color surface for both the collapsed bar and the
 * full now-playing sheet. Artwork is decoded before it replaces the previous
 * target, so a song change crossfades directly between two paintable layers.
 */
export function useMusicUltrablur(track: Ref<Track | null>, enabled: Ref<boolean>) {
  const imageTools = useBackgroundImageTools()
  const facetTrackId = computed<number | null>(() => enabled.value ? (track.value?.id ?? null) : null)
  const { facets } = useTrackFacets(facetTrackId)
  const target = shallowRef<MusicUltrablurTarget | null>(null)
  let resolveSequence = 0

  const genres = computed(() => (facets.value?.top_genres ?? [])
    .slice(0, 5)
    .map(genre => genre.name)
    .filter(Boolean))

  function generatedTarget(current: Track): MusicUltrablurTarget {
    const trackKey = trackIdentity(current)
    const seed = fallbackSeed(current, genres.value)
    const hash = musicUltrablurHash(seed)
    return {
      key: `${trackKey}:generated:${hash}`,
      trackKey,
      source: 'generated',
      imageUrl: null,
      background: musicUltrablurGradient(seed),
    }
  }

  watch([track, enabled], async ([current, isEnabled]) => {
    const sequence = ++resolveSequence
    if (!current || !isEnabled) {
      target.value = null
      return
    }

    const trackKey = trackIdentity(current)
    const generated = generatedTarget(current)

    // On the first mount, give the player a song-specific color immediately.
    // On later changes, retain the old song until the new layer is decoded.
    if (!target.value) target.value = generated
    if (!import.meta.client) return

    for (const candidate of artworkCandidates(current)) {
      const imageUrl = imageTools.ambientVariant(candidate.url)
      const ready = await imageTools.prepareResolved(imageUrl, 'low')
      if (sequence !== resolveSequence) return
      if (!ready) continue

      target.value = {
        key: `${trackKey}:${candidate.source}`,
        trackKey,
        source: candidate.source,
        imageUrl,
        background: generated.background,
      }
      return
    }

    if (sequence === resolveSequence) target.value = generatedTarget(current)
  }, { immediate: true })

  // Facets commonly arrive after artwork resolution. They only affect the
  // no-art fallback; never restart successful/failed image probes just to add
  // genres to the seed.
  watch(genres, () => {
    const current = track.value
    if (!enabled.value || !current || target.value?.source !== 'generated') return
    if (target.value.trackKey !== trackIdentity(current)) return
    target.value = generatedTarget(current)
  })

  return readonly(target)
}
