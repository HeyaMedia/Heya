// Home page composition — per-user section visibility + order, persisted in
// /api/me/settings (settings.home.sections). Shares the ['me','settings']
// vue-query key with pages/index.vue's pinned-hero query, so edits in
// Settings → Appearance reflect on Home through cache invalidation with no
// extra plumbing.
import { useQuery, useQueryClient } from '@tanstack/vue-query'

export interface HomeSectionDef {
  id: string
  label: string
  desc: string
}

// Default order = the home page's long-standing layout. The hero is a
// section like any other — some people just want rails.
export const HOME_SECTION_DEFS: HomeSectionDef[] = [
  { id: 'hero', label: 'Hero deck', desc: 'The featured spotlight at the top' },
  { id: 'continue-watching', label: 'Continue Watching', desc: 'Resume in-progress movies & episodes' },
  { id: 'up-next', label: 'Up Next', desc: 'Next episodes of shows you watch' },
  { id: 'for-you', label: 'For You', desc: 'Personalized recommendations' },
  { id: 'recent-movies', label: 'Recently Added Films', desc: 'Newest movies across libraries' },
  { id: 'recent-tv', label: 'Recently Added TV', desc: 'New shows, seasons & episodes' },
  { id: 'recent-albums', label: 'Recently Added Albums', desc: 'Latest music arrivals' },
  { id: 'recent-artists', label: 'Recently Added Artists', desc: 'New & updated artists' },
  { id: 'recent-books', label: 'Recently Added Books', desc: 'Newest books across libraries' },
]

export interface HomeSectionPref {
  id: string
  hidden?: boolean
}

interface MeSettingsBlob {
  home?: { sections?: HomeSectionPref[] }
  [key: string]: unknown
}

/** Server prefs → full resolved list: server order first (unknown IDs
 *  dropped), then any sections the stored list doesn't know about yet, in
 *  default order. Nothing stored = defaults, all visible. */
export function resolveSections(prefs?: HomeSectionPref[]) {
  const byId = new Map(HOME_SECTION_DEFS.map((d) => [d.id, d]))
  const out: (HomeSectionDef & { hidden: boolean })[] = []
  for (const p of prefs ?? []) {
    const def = byId.get(p.id)
    if (!def) continue
    out.push({ ...def, hidden: !!p.hidden })
    byId.delete(p.id)
  }
  for (const def of HOME_SECTION_DEFS) {
    if (byId.has(def.id)) out.push({ ...def, hidden: false })
  }
  return out
}

export function useHomeSections() {
  const { $heya } = useNuxtApp()
  const queryClient = useQueryClient()

  const settingsQuery = useQuery({
    queryKey: ['me', 'settings'],
    queryFn: async () => (await $heya('/api/me/settings')) as MeSettingsBlob,
    staleTime: 1000 * 60 * 5,
  })

  const sections = computed(() =>
    resolveSections(settingsQuery.data.value?.home?.sections),
  )

  const isVisible = (id: string) => !sections.value.find((s) => s.id === id)?.hidden
  const orderOf = (id: string) => {
    const i = sections.value.findIndex((s) => s.id === id)
    return i === -1 ? HOME_SECTION_DEFS.length : i
  }

  async function persist(next: HomeSectionPref[]) {
    const current = settingsQuery.data.value ?? {}
    const body: MeSettingsBlob = { ...current, home: { ...current.home, sections: next } }
    // Optimistic: rapid consecutive moves must each read the previous move's
    // result, not the stale pre-PUT cache (invalidate's refetch is async).
    queryClient.setQueryData(['me', 'settings'], body)
    try {
      await $heya('/api/me/settings', { method: 'PUT', body: body as never })
    } catch {
      // Roll back to server truth.
      queryClient.invalidateQueries({ queryKey: ['me', 'settings'] })
    }
  }

  function toPrefs() {
    return sections.value.map(({ id, hidden }) => (hidden ? { id, hidden } : { id }))
  }

  function toggle(id: string) {
    const next = toPrefs().map((p) =>
      p.id === id ? (p.hidden ? { id } : { id, hidden: true }) : p,
    )
    return persist(next)
  }

  function move(id: string, dir: -1 | 1) {
    const next = toPrefs()
    const i = next.findIndex((p) => p.id === id)
    const j = i + dir
    if (i === -1 || j < 0 || j >= next.length) return Promise.resolve()
    ;[next[i], next[j]] = [next[j]!, next[i]!]
    return persist(next)
  }

  function reset() {
    return persist([])
  }

  return { sections, isVisible, orderOf, toggle, move, reset, loading: settingsQuery.isLoading }
}
