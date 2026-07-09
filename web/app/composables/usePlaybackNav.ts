import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'
import type { UpNextItem } from '~/components/home/UpNextRow.vue'

// Navigation into the video player, shared by the Home page and the Movies/TV
// Recommended landings so the /watch URL shape + entity tagging live in one
// place. `entity_type`/`entity_id` are forwarded so the activity panel and
// resume state key correctly (episode vs movie) — see the watch-progress
// entity-keying notes.
export function usePlaybackNav() {
  // Continue Watching tile → resume in the player. Falls back to the detail
  // page when no playable file resolved (deleted / never-matched file).
  function playContinue(item: ContinueWatchingItem) {
    const fileRef = item.file_public_id || item.file_id
    if (!fileRef) {
      navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, slug: item.slug, media_type: item.media_type }))
      return
    }
    const params = new URLSearchParams({ media_item_id: String(item.media_item_id), title: item.title })
    if (item.entity_type) params.set('entity_type', item.entity_type)
    if (item.entity_id) params.set('entity_id', String(item.entity_id))
    navigateTo(`/watch/${fileRef}?${params}`)
  }

  // Up Next tile → play the next unwatched episode.
  function playUpNext(entry: UpNextItem) {
    const s = String(entry.season_number).padStart(2, '0')
    const e = String(entry.episode_number).padStart(2, '0')
    const params = new URLSearchParams({ media_item_id: String(entry.id), title: `${entry.title} - S${s}E${e}` })
    if (entry.episode_id) {
      params.set('entity_type', 'episode')
      params.set('entity_id', String(entry.episode_id))
    }
    navigateTo(`/watch/${entry.play_file_public_id || entry.play_file_id}?${params}`)
  }

  return { playContinue, playUpNext }
}
