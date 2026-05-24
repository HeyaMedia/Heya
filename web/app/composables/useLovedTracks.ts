// Reactive sets of the user's loved tracks / artists / albums. Hydrated
// once per session, kept consistent across components via useState. Each
// kind has its own slot but shares the same shape and toggle logic.

type EntityKind = 'track' | 'artist' | 'album'

interface LovedSet {
  ids: Ref<Set<number>>
  loaded: Ref<boolean>
  inflight: Promise<void> | null
}

function makeSlot(kind: EntityKind): LovedSet {
  return {
    ids: useState(`loved_${kind}_ids`, () => new Set<number>()),
    loaded: useState(`loved_${kind}_loaded`, () => false),
    inflight: null,
  }
}

// Module-level so concurrent component mounts share state. Vue's useState
// already de-dupes within a request but we want the inflight promise too.
const slots: Record<EntityKind, LovedSet> = {
  track: makeSlot('track'),
  artist: makeSlot('artist'),
  album: makeSlot('album'),
}

const pluralPath: Record<EntityKind, string> = {
  track: 'tracks',
  artist: 'artists',
  album: 'albums',
}

async function loadKind(kind: EntityKind) {
  const slot = slots[kind]
  if (slot.loaded.value || import.meta.server) return
  if (!slot.inflight) {
    slot.inflight = (async () => {
      try {
        const resp = await apiFetch<{ ids: number[] }>(`/api/me/loved/${pluralPath[kind]}/ids`)
        slot.ids.value = new Set(resp.ids ?? [])
      } catch {
        // Empty set is the safe default — heart stays unfilled, toggles still work.
      } finally {
        slot.loaded.value = true
        slot.inflight = null
      }
    })()
  }
  await slot.inflight
}

async function toggleKind(kind: EntityKind, id: number) {
  const slot = slots[kind]
  const next = !slot.ids.value.has(id)
  // Optimistic flip so the heart responds immediately.
  const replaced = new Set(slot.ids.value)
  if (next) replaced.add(id)
  else replaced.delete(id)
  slot.ids.value = replaced
  try {
    const method = next ? 'POST' : 'DELETE'
    const resp = await apiFetch<{ loved: boolean }>(`/api/me/loved/${pluralPath[kind]}/${id}`, { method })
    if (resp.loved !== next) {
      const final = new Set(slot.ids.value)
      if (resp.loved) final.add(id)
      else final.delete(id)
      slot.ids.value = final
    }
  } catch {
    const revert = new Set(slot.ids.value)
    if (next) revert.delete(id)
    else revert.add(id)
    slot.ids.value = revert
  }
}

// Track-specific composable (legacy callers).
export function useLovedTracks() {
  return {
    lovedIds: readonly(slots.track.ids),
    isLoved: (id: number) => slots.track.ids.value.has(id),
    toggle: (id: number) => toggleKind('track', id),
    ensureLoaded: () => loadKind('track'),
  }
}

// Generic entity-loved composable.
export function useLovedEntity(kind: EntityKind) {
  return {
    lovedIds: readonly(slots[kind].ids),
    isLoved: (id: number) => slots[kind].ids.value.has(id),
    toggle: (id: number) => toggleKind(kind, id),
    ensureLoaded: () => loadKind(kind),
  }
}
