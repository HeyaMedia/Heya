import { useInfiniteQuery } from '@pinia/colada'
import { upNextRailInfinite } from '~/queries/activity'
import type { UpNextItem } from '~/types/home'

// The "Up Next" rail, server-owned: /api/me/up-next resolves the next
// unwatched episode WITH a playable file per recently-watched series in one
// round-trip. (The old client-side derivation — first 20 recent titles × one
// /up-next call each — went blind whenever a bulk mark-watched pass filled
// the recency window with finished shows.) Shared by the Home page and the
// TV Recommended landing; `enabled` gates the fetch on pages that only
// conditionally show the rail.
export function useUpNext(enabled: () => boolean = () => true) {
  const query = useInfiniteQuery(() => ({ ...upNextRailInfinite(), enabled: enabled() }))

  const upNextItems = computed<UpNextItem[]>(() => (query.data.value?.pages ?? []).flat().map((row) => {
    const s = String(row.season_number).padStart(2, '0')
    const e = String(row.episode_number).padStart(2, '0')
    return {
      id: row.media_item_id,
      title: row.title,
      slug: row.slug,
      season_number: row.season_number,
      episode_number: row.episode_number,
      episode_label: row.episode_title ? `S${s}E${e} · ${row.episode_title}` : `S${s}E${e}`,
      play_file_id: row.file_id,
      play_file_public_id: row.file_public_id,
      episode_id: row.episode_id,
      runtime_minutes: row.runtime,
      media_item_public_id: row.media_item_public_id,
    }
  }))

  return {
    upNextItems,
    isPending: query.isPending,
    hasMore: query.hasNextPage,
    loadingMore: computed(() => query.asyncStatus.value === 'loading'),
    loadMore: railLoadMore(query),
  }
}
