import type { UpNextItem } from '~/types/home'

// A recently-watched row, reduced to what Up Next needs. Both the deduped
// (/api/me/watch/recent) and episode-level (/api/me/watch/recent-episodes)
// shapes map onto this.
interface UpNextSourceRow {
  media_item_id: number
  title: string
  slug: string
  media_type: string
}

// Builds the "Up Next" rail: for each unique recently-watched TV series, resolve
// its next unwatched episode via /up-next (one round-trip per series, in
// parallel). Shared by the Home page and the TV Recommended landing.
//
// `source` is a getter over recently-watched rows — pass a query's `.data.value`
// getter so the rail rebuilds whenever that data refreshes. Movie rows and
// repeated series are ignored (deduped by media_item_id), so an episode-level
// feed with several episodes of one show still yields one Up Next tile.
export function useUpNext(source: () => UpNextSourceRow[] | undefined | null) {
  const { $heya } = useNuxtApp()
  const upNextItems = ref<UpNextItem[]>([])

  async function rebuild() {
    const recent = source()
    if (!recent?.length) { upNextItems.value = []; return }

    const series = new Map<number, UpNextSourceRow>()
    for (const row of recent) {
      if (row.media_type !== 'tv' && row.media_type !== 'anime') continue
      if (!series.has(row.media_item_id)) series.set(row.media_item_id, row)
    }

    const resolved = await Promise.allSettled(
      Array.from(series.values()).map(async row => {
        const up = await $heya('/api/media/{id}/up-next', { path: { id: row.media_item_id as never } }) as {
          has_next: boolean; file_id?: number; file_public_id?: string; episode_id?: number
          season_number?: number; episode_number?: number; episode_title?: string; runtime?: number
        }
        return { row, up }
      }),
    )

    const entries: UpNextItem[] = []
    for (const r of resolved) {
      if (r.status !== 'fulfilled') continue
      const { row, up } = r.value
      if (!up?.has_next || (!up.file_public_id && !up.file_id)) continue
      const sNum = up.season_number ?? 0
      const eNum = up.episode_number ?? 0
      const s = String(sNum).padStart(2, '0')
      const e = String(eNum).padStart(2, '0')
      const label = up.episode_title ? `S${s}E${e} · ${up.episode_title}` : `S${s}E${e}`
      entries.push({
        id: row.media_item_id, title: row.title, slug: row.slug,
        season_number: sNum, episode_number: eNum, episode_label: label,
        play_file_id: up.file_id || 0, play_file_public_id: up.file_public_id, episode_id: up.episode_id, runtime_minutes: up.runtime,
      })
    }
    upNextItems.value = entries.slice(0, 24)
  }

  watch(source, rebuild, { immediate: true })
  return { upNextItems, rebuild }
}
