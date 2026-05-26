/**
 * Per-user ratings cache for tracks / albums / artists. One module-scoped
 * cache per kind so the rating widget renders consistently wherever the
 * same entity appears (Songs page + Player + Album page + Favorites …).
 *
 * Use `useRatings('track' | 'album' | 'artist')` to grab the cache for a
 * specific kind. The API surface is identical across kinds; the underlying
 * HTTP routes (/api/me/ratings/{tracks|albums|artists}/...) handle the
 * persistence.
 */

type Kind = 'track' | 'album' | 'artist'

// Module-scoped state, one cache per kind.
const stores: Record<Kind, { ratings: Ref<Map<number, number>>; inflight: Map<number, Promise<number>> }> = {
  track: { ratings: ref(new Map()), inflight: new Map() },
  album: { ratings: ref(new Map()), inflight: new Map() },
  artist: { ratings: ref(new Map()), inflight: new Map() },
}

export function useRatings(kind: Kind) {
  const store = stores[kind]
  const ratings = store.ratings

  function get(id: number): number {
    return ratings.value.get(id) ?? 0
  }

  async function getOne(id: number): Promise<{ rating: number }> {
    const { $heya } = useNuxtApp()
    if (kind === 'track') return await $heya('/api/me/ratings/tracks/{id}', { path: { id } }) as unknown as { rating: number }
    if (kind === 'album') return await $heya('/api/me/ratings/albums/{id}', { path: { id } }) as unknown as { rating: number }
    return await $heya('/api/me/ratings/artists/{id}', { path: { id } }) as unknown as { rating: number }
  }

  async function setOne(id: number, rating: number): Promise<void> {
    const { $heya } = useNuxtApp()
    const body = { rating }
    if (kind === 'track') { await $heya('/api/me/ratings/tracks/{id}', { method: 'PUT', path: { id }, body }); return }
    if (kind === 'album') { await $heya('/api/me/ratings/albums/{id}', { method: 'PUT', path: { id }, body }); return }
    await $heya('/api/me/ratings/artists/{id}', { method: 'PUT', path: { id }, body })
  }

  async function getBatch(ids: number[]): Promise<Record<string, number>> {
    const { $heya } = useNuxtApp()
    if (kind === 'track') {
      const r = await $heya('/api/me/ratings/tracks/batch', { method: 'POST', body: { track_ids: ids } }) as unknown as { ratings: Record<string, number> }
      return r.ratings ?? {}
    }
    if (kind === 'album') {
      const r = await $heya('/api/me/ratings/albums/batch', { method: 'POST', body: { album_ids: ids } }) as unknown as { ratings: Record<string, number> }
      return r.ratings ?? {}
    }
    const r = await $heya('/api/me/ratings/artists/batch', { method: 'POST', body: { artist_ids: ids } }) as unknown as { ratings: Record<string, number> }
    return r.ratings ?? {}
  }

  async function load(id: number): Promise<number> {
    if (ratings.value.has(id)) return ratings.value.get(id)!
    if (store.inflight.has(id)) return store.inflight.get(id)!
    const p = (async () => {
      try {
        const r = await getOne(id)
        const v = r.rating ?? 0
        ratings.value.set(id, v)
        return v
      } catch {
        ratings.value.set(id, 0)
        return 0
      } finally {
        store.inflight.delete(id)
      }
    })()
    store.inflight.set(id, p)
    return p
  }

  async function set(id: number, rating: number): Promise<void> {
    const prev = ratings.value.get(id) ?? 0
    ratings.value.set(id, rating)
    ratings.value = new Map(ratings.value)
    try {
      await setOne(id, rating)
    } catch {
      ratings.value.set(id, prev)
      ratings.value = new Map(ratings.value)
      throw new Error('Failed to save rating')
    }
  }

  function primeMany(entries: Record<number, number> | Array<[number, number]>) {
    const it = Array.isArray(entries) ? entries : Object.entries(entries).map(([k, v]) => [Number(k), v] as [number, number])
    for (const [id, r] of it) {
      ratings.value.set(id, r)
    }
    ratings.value = new Map(ratings.value)
  }

  async function primeBulk(ids: number[]) {
    const needed = ids.filter((id) => !ratings.value.has(id))
    if (!needed.length) return
    try {
      const map = await getBatch(needed)
      for (const id of needed) {
        ratings.value.set(id, map[String(id)] ?? 0)
      }
      ratings.value = new Map(ratings.value)
    } catch {
      // Best-effort prime — stars just stay empty until the user touches one.
    }
  }

  return { get, load, set, primeMany, primeBulk, ratings: readonly(ratings) }
}

// Back-compat: every existing call site uses useTrackRatings. Re-export
// so we don't have to touch them — under the hood it's just useRatings('track').
export function useTrackRatings() {
  return useRatings('track')
}
